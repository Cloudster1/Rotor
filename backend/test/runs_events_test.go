//go:build integration

package test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/cache"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/runs"
)

// signalWait bounds how long a published event signal may take to reach a subscriber in another process.
const signalWait = 30 * time.Second

// chatty prints in two code nodes, so a run has several events to stream and replay.
func chatty() string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("chatty") + `}
limits: {maxSteps: 10, timeout: 2m}
entry: first
nodes:
  - id: first
    type: code
    output: {schema: {type: object}}
    source: |
      import time
      def run(state, ctx):
          for i in range(3):
              print("first", i)
              time.sleep(1)
          return {"n": 3}
  - id: second
    type: code
    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          print("second")
          return {"done": True}
edges:
  - {from: first, to: second}
`
}

// sseEvent is one server-sent event as a client reads it.
type sseEvent struct {
	ID    int64
	Event string
	Data  runs.Event
}

// readEvents reads a stream to its end and parses its events.
func readEvents(t *testing.T, body io.Reader) []sseEvent {
	t.Helper()
	var out []sseEvent
	var current sseEvent
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if current.Event != "" {
				out = append(out, current)
			}
			current = sseEvent{}
		case strings.HasPrefix(line, "id: "):
			current.ID, _ = strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
		case strings.HasPrefix(line, "event: "):
			current.Event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &current.Data); err != nil {
				t.Fatalf("an event's data is not JSON: %q", line)
			}
		}
	}
	return out
}

// stream follows a run's events through base as the browser, from after lastID, until the stream ends.
func (b *browser) stream(t *testing.T, base, runID string, lastID int64) (int, []sseEvent) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/runs/"+runID+"/events", nil)
	if lastID > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(lastID, 10))
	}
	client := b.client
	if base != b.app {
		// Another api than the stack's: the session cookie goes along by hand.
		app, _ := url.Parse(b.app + "/api/v1")
		for _, c := range b.client.Jar.Cookies(app) {
			req.AddCookie(c)
		}
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("the stream answers %s", resp.Header.Get("Content-Type"))
	}
	return resp.StatusCode, readEvents(t, resp.Body)
}

// numbered fails unless the events are numbered from first on, one by one, and say what their rows say.
func numbered(t *testing.T, events []sseEvent, first int64) {
	t.Helper()
	for i, e := range events {
		if e.ID != first+int64(i) || e.Data.Seq != e.ID || e.Data.Type != e.Event {
			t.Fatalf("event %d is %+v, want number %d", i, e, first+int64(i))
		}
	}
}

func TestARunsEventsStreamLiveAndReplayAfterTheLastOneSeen(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	workflowID := createWorkflow(t, alice, chatty())
	run := startRun(t, alice, workflowID, map[string]any{}, http.StatusCreated)
	runID, _ := run["id"].(string)

	// Followed from the start, while the run is still queued: everything arrives, and the stream ends with the run.
	status, events := alice.stream(t, alice.app, runID, 0)
	if status != http.StatusOK || len(events) == 0 {
		t.Fatalf("the stream = %d with %d events", status, len(events))
	}
	numbered(t, events, 1)
	last := events[len(events)-1]
	if last.Event != "run.finished" {
		t.Errorf("the stream ended on %+v, want run.finished", last)
	}
	if stored := h.events(t, uuid.MustParse(runID)); len(stored) != len(events) {
		t.Errorf("the stream sent %d events of the %d stored", len(events), len(stored))
	}
	logs := 0
	for _, e := range events {
		if e.Event == "log" {
			logs++
		}
	}
	if logs < 2 {
		t.Errorf("the stream carried %d log events: %+v", logs, events)
	}

	// A reconnect after the third event gets the rest, without gaps or repeats.
	_, rest := alice.stream(t, alice.app, runID, 3)
	if len(rest) != len(events)-3 {
		t.Fatalf("after event 3 the stream sent %d events, want %d", len(rest), len(events)-3)
	}
	numbered(t, rest, 4)

	// Someone who may not see the run is refused before any stream starts.
	dave := newBrowser(t)
	dave.signIn("dave")
	if status, _ := dave.stream(t, dave.app, runID, 0); status != http.StatusNotFound {
		t.Errorf("dave following alice's run = %d, want 404", status)
	}
}

func TestAnotherApiReplicaStreamsARunWhoseRunnerTalksToTheFirst(t *testing.T) {
	h := newHarness(t)
	signals, err := cache.Open(h.cfg.Valkey.URL, slog.New(slog.DiscardHandler))
	if err != nil || signals == nil {
		t.Skipf("ROTOR_VALKEY_URL is not set; run the suite with make test-integration (%v)", err)
	}
	t.Cleanup(func() { _ = signals.Close() })
	// A second api, in this process, on the same database and Valkey, serving only what following a run needs.
	replica := &httpapi.Server{DB: h.cluster, Log: slog.New(slog.DiscardHandler), Sessions: auth.NewService(h.cluster, h.cfg.Auth.SessionTTL),
		CookieName: h.cfg.Auth.SessionCookie, Runs: runs.NewService(h.cluster, nil, nil, nil, signals)}
	server := httptest.NewServer(replica.Routes(nil))
	t.Cleanup(server.Close)

	alice := newBrowser(t)
	alice.signIn("alice")
	workflowID := createWorkflow(t, alice, chatty())
	run := startRun(t, alice, workflowID, map[string]any{}, http.StatusCreated)
	runID, _ := run["id"].(string)

	// The stack's api announces each event it stores to every replica.
	ctx, cancel := context.WithTimeout(context.Background(), signalWait)
	defer cancel()
	heard := signals.Subscribe(ctx, runnerctl.EventsChannel(uuid.MustParse(runID)))
	status, events := alice.stream(t, server.URL, runID, 0)
	if status != http.StatusOK || len(events) == 0 || events[len(events)-1].Event != "run.finished" {
		t.Fatalf("the replica's stream = %d with %+v", status, events)
	}
	numbered(t, events, 1)
	select {
	case <-heard:
	case <-ctx.Done():
		t.Error("no replica heard that the run had a new event")
	}
}
