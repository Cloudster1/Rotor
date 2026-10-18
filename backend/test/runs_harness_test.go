//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// envSandboxStack is the stack's compose project, whose containers a test stops and starts.
const envSandboxStack = "ROTOR_TEST_SANDBOX_STACK"

// apiWith calls the api as the browser with extra headers, such as an Idempotency-Key.
func (b *browser) apiWith(method, path string, body any, headers map[string]string) (int, map[string]any) {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, b.app+"/api/v1"+path, reader)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, text := b.do(req)
	var out map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return resp.StatusCode, out
}

// startRun starts a run of a workflow as b and answers the run, failing unless the api answers want.
func startRun(t *testing.T, b *browser, workflowID string, inputs map[string]any, want int) map[string]any {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows/"+workflowID+"/runs", map[string]any{"inputs": inputs})
	if status != want {
		t.Fatalf("starting a run = %d %v, want %d", status, answer, want)
	}
	return answer
}

// waitRun polls the run as b until it is in one of the states given.
func waitRun(t *testing.T, b *browser, runID string, states ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(runWait)
	var run map[string]any
	for time.Now().Before(deadline) {
		var status int
		status, run = b.api(http.MethodGet, "/runs/"+runID, nil)
		if status != http.StatusOK {
			t.Fatalf("reading run %s = %d %v", runID, status, run)
		}
		for _, want := range states {
			if run["status"] == want {
				return run
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("run %s is %v after %s, want one of %v", runID, run["status"], runWait, states)
	return nil
}

// sandboxesOf lists the stack's sandboxes of one run.
func sandboxesOf(t *testing.T, b sandbox.Backend, runID string) []sandbox.Sandbox {
	t.Helper()
	found, err := b.List(context.Background(), map[string]string{sandbox.LabelRun: runID})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// waitNoSandbox waits until the run has no sandbox left.
func waitNoSandbox(t *testing.T, b sandbox.Backend, runID string) {
	t.Helper()
	deadline := time.Now().Add(sandboxWait)
	for time.Now().Before(deadline) {
		if len(sandboxesOf(t, b, runID)) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("run %s still has sandboxes after %s: %+v", runID, sandboxWait, sandboxesOf(t, b, runID))
}

// dockerCall sends one request to the host's Docker daemon, for what the sandbox backend does not offer.
func dockerCall(t *testing.T, method, path string, want ...int) {
	t.Helper()
	client := &http.Client{Timeout: config.SandboxCallTimeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", config.DefaultDockerSocket)
		}}}
	req, _ := http.NewRequest(method, "http://docker"+path, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, w := range want {
		if resp.StatusCode == w {
			return
		}
	}
	t.Fatalf("docker %s %s = %d %s", method, path, resp.StatusCode, body)
}

// stopWorker stops the stack's worker until the test ends, so the test decides when runs are taken up.
func stopWorker(t *testing.T) (start func()) {
	t.Helper()
	stack := os.Getenv(envSandboxStack)
	if stack == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envSandboxStack)
	}
	name := fmt.Sprintf("/containers/%s-worker-1", stack)
	dockerCall(t, http.MethodPost, name+"/stop?t=5", http.StatusNoContent, http.StatusNotModified)
	var once sync.Once
	start = func() {
		once.Do(func() { dockerCall(t, http.MethodPost, name+"/start", http.StatusNoContent, http.StatusNotModified) })
	}
	t.Cleanup(start)
	return start
}

// fakeSandboxes is a sandbox backend whose sandboxes never start a runner, for what a test drives by hand.
type fakeSandboxes struct {
	mu      sync.Mutex
	spawned []sandbox.Spec
	alive   map[sandbox.ID]sandbox.Spec
}

func newFakeSandboxes() *fakeSandboxes { return &fakeSandboxes{alive: map[sandbox.ID]sandbox.Spec{}} }

func (f *fakeSandboxes) Spawn(_ context.Context, spec sandbox.Spec) (sandbox.ID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawned = append(f.spawned, spec)
	id := sandbox.ID("fake-" + uuid.NewString())
	f.alive[id] = spec
	return id, nil
}

func (f *fakeSandboxes) Destroy(_ context.Context, id sandbox.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.alive[id]; !ok {
		return sandbox.ErrNotFound
	}
	delete(f.alive, id)
	return nil
}

func (f *fakeSandboxes) Status(_ context.Context, id sandbox.ID) (sandbox.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.alive[id]; !ok {
		return sandbox.Status{}, sandbox.ErrNotFound
	}
	return sandbox.Status{State: sandbox.StateRunning}, nil
}

func (f *fakeSandboxes) List(_ context.Context, labels map[string]string) ([]sandbox.Sandbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sandbox.Sandbox
	for id, spec := range f.alive {
		l := map[string]string{sandbox.LabelRun: spec.RunID.String()}
		for k, v := range spec.Labels {
			l[k] = v
		}
		out = append(out, sandbox.Sandbox{ID: id, Labels: l, Status: sandbox.Status{State: sandbox.StateRunning}})
	}
	return out, nil
}

func (f *fakeSandboxes) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.spawned)
}

// greeter calls the tool as the run's person and greets them with the run's input.
func greeter(name, tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + name + `}
inputs:
  type: object
  required: [greeting]
  properties:
    greeting: {type: string, minLength: 1}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - id: greet
    type: code
    output: {schema: {type: object, required: [text]}}
    source: |
      def run(state, ctx):
          return {"text": state["inputs"]["greeting"] + ", " + state["nodes"]["ask"]["user"]["name"]}
edges:
  - {from: ask, to: greet}
`
}
