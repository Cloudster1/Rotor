//go:build integration

package test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	sealing "github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// A run is traced without asking (#242): its runner's spans land in Postgres beside their export, read through SQL
// by whoever reads the run and by no other member.
func TestARunsSpansAreKeptForWhoeverReadsTheRun(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	key, product := "llm-key-"+uuid.NewString(), "kept-"+uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": []map[string]any{
		{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}})
	bob, bobID, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	run := startRun(t, bob, createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string))), map[string]any{}, 201)
	runID := uuid.MustParse(run["id"].(string))
	if done := waitRun(t, bob, runID.String(), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["reason"])
	}

	// The runner's last spans come right before its Done, and are stored beside the run ending.
	want := []string{"run", "node ask", "mcp whoami"}
	var names, kinds []string
	deadline := time.Now().Add(traceWrite)
	for {
		names, kinds = nil, nil
		rows, err := h.super.Query(ctx, `SELECT s.name, s.kind FROM trace_span s JOIN trace t ON t.id = s.trace
			JOIN run r ON r.id = t.run_id AND r.trace_id = t.trace_id
			WHERE t.run_id = $1 AND s.service = 'rotor-runner' AND s.source = 'runner' AND s.attributes->>'rotor.run' = $2`,
			runID, runID.String())
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name, kind string
			if err := rows.Scan(&name, &kind); err != nil {
				t.Fatal(err)
			}
			names, kinds = append(names, name), append(kinds, kind)
		}
		rows.Close()
		if !slices.ContainsFunc(want, func(n string) bool { return !slices.Contains(names, n) }) && slices.Contains(kinds, "LLM") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run's kept spans are %v of kinds %v; want %v and a model call", names, kinds, want)
		}
		time.Sleep(time.Second)
	}

	// Text is kept only sealed, with the run's own key.
	box := h.runBox(t, runID)
	rows, err := h.super.Query(ctx, `SELECT t.id, s.span_id, s.sealed_content FROM trace_span s JOIN trace t ON t.id = s.trace
		WHERE t.run_id = $1 AND s.sealed_content IS NOT NULL`, runID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var trace uuid.UUID
		var span string
		var sealed []byte
		if err := rows.Scan(&trace, &span, &sealed); err != nil {
			t.Fatal(err)
		}
		plain, err := box.Open(sealing.ColumnTraceContent, sealing.TraceSpanRow(trace, span), sealed)
		if err != nil || !json.Valid(plain) {
			t.Errorf("span %s's text does not open with the run's key: %v", span, err)
		}
	}
	rows.Close()

	if traces, spans := h.spansReadBy(t, bobID, runID); traces != 1 || spans < len(want) {
		t.Errorf("bob, who started the run, reads %d traces and %d spans of it", traces, spans)
	}
	other := h.addPerson(t, "member")
	if traces, spans := h.spansReadBy(t, other, runID); traces != 0 || spans != 0 {
		t.Errorf("a member who may not see the run reads %d traces and %d spans of it", traces, spans)
	}
}

// Text is kept only while an administrator lets Rotor keep it, and then sealed with the run's key for its span alone.
func TestASpansTextIsKeptSealedOnlyWhileAllowed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	r := h.runWithTrace(t, h.addPerson(t, "member"))
	h.keepContent(t, true)
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, runnerSpans(r, 1)); err != nil {
		t.Fatal(err)
	}
	h.keepContent(t, false)
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, runnerSpans(r, 1)); err != nil {
		t.Fatal(err)
	}

	kept, _ := h.keptTraceOf(t, r.id)
	if kept.spans != 2 || !kept.content {
		t.Fatalf("the trace holds %+v, want two spans and text", kept)
	}
	rows, err := h.super.Query(ctx, `SELECT span_id, sealed_content, attributes::text FROM trace_span WHERE trace = $1
		ORDER BY received_at`, kept.id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	box := h.runBox(t, r.id)
	for i := 0; rows.Next(); i++ {
		var span, attributes string
		var sealed []byte
		if err := rows.Scan(&span, &sealed, &attributes); err != nil {
			t.Fatal(err)
		}
		if containsText(attributes) {
			t.Errorf("span %s keeps its text in the clear: %s", span, attributes)
		}
		if i == 1 {
			if sealed != nil {
				t.Error("a span stored while text is not kept holds text")
			}
			continue
		}
		plain, err := box.Open(sealing.ColumnTraceContent, sealing.TraceSpanRow(kept.id, span), sealed)
		var text map[string]string
		if err != nil || json.Unmarshal(plain, &text) != nil || text["input.value"] != "the story" {
			t.Errorf("span %s's text opens as %q, %v", span, plain, err)
		}
		if _, err := box.Open(sealing.ColumnTraceContent, sealing.TraceSpanRow(uuid.New(), span), sealed); err == nil {
			t.Error("a span's text opens as another trace's")
		}
	}
}

func containsText(attributes string) bool {
	var plain map[string]string
	_ = json.Unmarshal([]byte(attributes), &plain)
	_, ok := plain["input.value"]
	return ok
}

// The cap holds whoever inserts (#242): a trace stops at its spans and bytes, and a sender's own traces at theirs,
// counting what it skipped and saying when it was cut.
func TestATracePastItsCapIsCutAndMarked(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	r := h.runWithTrace(t, person)
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, runnerSpans(r, 1)); err != nil {
		t.Fatal(err)
	}
	kept, _ := h.keptTraceOf(t, r.id)
	tag, err := h.super.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, service, source, started_at, ended_at)
		SELECT $1, lpad(to_hex(g), 16, '0'), 'step', 'rotor-runner', 'runner', now(), now() FROM generate_series(1, $2) g`,
		kept.id, config.MaxTraceSpans)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := h.keptTraceOf(t, r.id)
	if tag.RowsAffected() != config.MaxTraceSpans-1 || after.spans != config.MaxTraceSpans || after.rows != config.MaxTraceSpans ||
		after.dropped != 1 || !after.cut {
		t.Errorf("inserting %d spans into a trace of one took %d and left %+v, want it cut at %d with one dropped",
			config.MaxTraceSpans, tag.RowsAffected(), after, config.MaxTraceSpans)
	}

	// A span id taken already is skipped, and the cap holds rotor_app too: a sender's span into a trace at its bytes.
	other := h.runWithTrace(t, person)
	if err := h.traceStore(t).Keep(ctx, other.id, other.dataKey, observability.AllTracing, runnerSpans(other, 1)); err != nil {
		t.Fatal(err)
	}
	full, _ := h.keptTraceOf(t, other.id)
	var span string
	if err := h.super.QueryRow(ctx, `SELECT span_id FROM trace_span WHERE trace = $1`, full.id).Scan(&span); err != nil {
		t.Fatal(err)
	}
	if tag, err := h.super.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, service, source, started_at, ended_at)
		VALUES ($1, $2, 'again', 'rotor-runner', 'runner', now(), now())`, full.id, span); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("a span id taken already = %v, %v; want it skipped", tag, err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE trace SET bytes = $2 - 1 WHERE id = $1`, full.id, config.MaxTraceBytes); err != nil {
		t.Fatal(err)
	}
	err = asApp(ctx, h.super, person, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, service, source, sender_id, started_at, ended_at)
			VALUES ($1, 'aaaaaaaaaaaaaaaa', 'app', 'my-app', 'otlp', $2, now(), now())`, full.id, person)
		if err != nil {
			return err
		}
		var dropped int
		var cut bool
		if err := tx.QueryRow(ctx, `SELECT dropped, cut_at IS NOT NULL FROM trace WHERE id = $1`, full.id).Scan(&dropped, &cut); err != nil {
			return err
		}
		if tag.RowsAffected() != 0 || dropped != 1 || !cut {
			t.Errorf("a sender's span past the trace's bytes took %d rows, leaving %d dropped and cut %v", tag.RowsAffected(), dropped, cut)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A sender's own traces stop at their spans together.
	var owned uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO trace (trace_id, owner_id, spans) VALUES ($1, $2, $3) RETURNING id`,
		randomTraceID(), person, config.MaxSenderSpans).Scan(&owned); err != nil {
		t.Fatal(err)
	}
	err = asApp(ctx, h.super, person, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, service, source, sender_id, started_at, ended_at)
			VALUES ($1, 'bbbbbbbbbbbbbbbb', 'app', 'my-app', 'otlp', $2, now(), now())`, owned, person)
		if err == nil && tag.RowsAffected() != 0 {
			t.Error("a sender past its spans across its own traces kept one more")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
