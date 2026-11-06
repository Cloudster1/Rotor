//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

// traceWrite bounds how long a run's spans take to reach Postgres once its runner sent them.
const traceWrite = 30 * time.Second

// tracedByHand is a run stored behind the policies' back, with a data key of its own and a trace id.
type tracedByHand struct {
	id, workflow, person uuid.UUID
	dataKey              string
	traceID              trace.TraceID
}

// runWithTrace stores a running run of person's, as the store finds a run whose runner sends spans.
func (h *harness) runWithTrace(t *testing.T, person uuid.UUID) tracedByHand {
	t.Helper()
	workflow := h.keptWorkflow(t, person, "")
	id, wrapped, inputs := sealedRun(t, []byte("{}"))
	traceparent := observability.FreshTraceparent()
	if _, err := h.super.Exec(context.Background(), `INSERT INTO run (id, workflow_id, version, user_id, status, data_key,
			sealed_inputs, traceparent) VALUES ($1, $2, 1, $3, 'running', $4, $5, $6)`,
		id, workflow, person, wrapped, inputs, traceparent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM run WHERE id = $1`, id) })
	return tracedByHand{id: id, workflow: workflow, person: person, dataKey: wrapped, traceID: observability.TraceIDOf(traceparent)}
}

// runnerSpans are n checked spans of a run's trace, each with an input, as a runner's batch reaches the store.
func runnerSpans(r tracedByHand, n int) []observability.RunnerSpan {
	out := make([]observability.RunnerSpan, n)
	now := time.Now()
	for i := range out {
		var id trace.SpanID
		_, _ = rand.Read(id[:])
		out[i] = observability.RunnerSpan{TraceID: r.traceID, SpanID: id, Name: "node ask", Start: now, End: now.Add(time.Millisecond),
			Attributes: map[string]string{"openinference.span.kind": "AGENT", "rotor.run": r.id.String(), "input.value": "the story"}}
	}
	return out
}

// traceStore is the store as the api holds it, opening runs' keys with OpenBao's root token.
func (h *harness) traceStore(t *testing.T) *traces.Service {
	t.Helper()
	s := traces.NewService(h.cluster, slog.New(slog.DiscardHandler))
	s.Keys = runKeys(t)
	return s
}

// keptTrace is what Postgres holds of a run's trace: its row's counts and the spans left.
type keptTrace struct {
	id                    uuid.UUID
	spans, dropped, rows  int
	cut, expired, content bool
}

func (h *harness) keptTraceOf(t *testing.T, runID uuid.UUID) (keptTrace, bool) {
	t.Helper()
	var k keptTrace
	err := h.super.QueryRow(context.Background(), `SELECT t.id, t.spans, t.dropped, t.cut_at IS NOT NULL, t.expired_at IS NOT NULL,
			t.content, (SELECT count(*) FROM trace_span s WHERE s.trace = t.id) FROM trace t WHERE t.run_id = $1`, runID).
		Scan(&k.id, &k.spans, &k.dropped, &k.cut, &k.expired, &k.content, &k.rows)
	if err == pgx.ErrNoRows {
		return keptTrace{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return k, true
}

// spansReadBy counts a run's spans and traces person reads, straight through SQL as rotor_app.
func (h *harness) spansReadBy(t *testing.T, person, runID uuid.UUID) (traces, spans int) {
	t.Helper()
	err := asApp(context.Background(), h.super, person, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM trace WHERE run_id = $1),
			(SELECT count(*) FROM trace_span s JOIN trace t ON t.id = s.trace WHERE t.run_id = $1)`, runID).Scan(&traces, &spans)
	})
	if err != nil {
		t.Fatal(err)
	}
	return traces, spans
}

// randomTraceID is a trace id no run has, as a sender's own trace takes.
func randomTraceID() string {
	var id trace.TraceID
	_, _ = rand.Read(id[:])
	return id.String()
}

// keepContent sets whether Rotor keeps spans' text, behind the policies' back, and puts it back after the test.
func (h *harness) keepContent(t *testing.T, keep bool) {
	t.Helper()
	ctx := context.Background()
	var before bool
	if err := h.super.QueryRow(ctx, `SELECT keep_content FROM trace_setting`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE trace_setting SET keep_content = $1`, keep); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `UPDATE trace_setting SET keep_content = $1`, before)
	})
}
