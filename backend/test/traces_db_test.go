//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/observability"
)

// A guard is not proven by the service refusing: rotor_app writes no runner span, no span into another's trace and
// no trace of a run it may not read, and changes or deletes none it reads.
func TestRotorAppWritesOnlyTheSpansAndTracesTheContractAllows(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	starter, stranger := h.addPerson(t, "member"), h.addPerson(t, "member")
	r := h.runWithTrace(t, starter)
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, runnerSpans(r, 1)); err != nil {
		t.Fatal(err)
	}
	kept, _ := h.keptTraceOf(t, r.id)
	var owned string
	if err := h.super.QueryRow(ctx, `INSERT INTO trace (trace_id, owner_id) VALUES ($1, $2) RETURNING id`, randomTraceID(), starter).
		Scan(&owned); err != nil {
		t.Fatal(err)
	}

	refused := map[string]struct {
		person uuid.UUID
		sql    string
		args   []any
	}{
		"a runner's span as rotor_app": {starter, `INSERT INTO trace_span (trace, span_id, name, service, source, started_at, ended_at)
			VALUES ($1, 'cccccccccccccccc', 'forged', 'rotor-runner', 'runner', now(), now())`, []any{kept.id}},
		"a span under another sender's name": {starter, `INSERT INTO trace_span (trace, span_id, name, service, source, sender_id,
			started_at, ended_at) VALUES ($1, 'cccccccccccccccc', 'forged', 'app', 'otlp', $2, now(), now())`, []any{kept.id, stranger}},
		"a span into a run's trace the sender may not read": {stranger, `INSERT INTO trace_span (trace, span_id, name, service, source,
			sender_id, started_at, ended_at) VALUES ($1, 'cccccccccccccccc', 'app', 'app', 'otlp', $2, now(), now())`, []any{kept.id, stranger}},
		"a span into another sender's own trace": {stranger, `INSERT INTO trace_span (trace, span_id, name, service, source, sender_id,
			started_at, ended_at) VALUES ($1, 'cccccccccccccccc', 'app', 'app', 'otlp', $2, now(), now())`, []any{owned, stranger}},
		"a trace of a run it may not read": {stranger, `INSERT INTO trace (trace_id, run_id) VALUES ($1, $2)`,
			[]any{r.traceID.String(), r.id}},
		"a trace of its run under another trace id": {starter, `INSERT INTO trace (trace_id, run_id) VALUES ($1, $2)`,
			[]any{randomTraceID(), r.id}},
		"a trace already counted": {starter, `INSERT INTO trace (trace_id, owner_id, spans) VALUES ($1, $2, 5)`,
			[]any{randomTraceID(), starter}},
		"a trace of someone else's": {starter, `INSERT INTO trace (trace_id, owner_id) VALUES ($1, $2)`,
			[]any{randomTraceID(), stranger}},
		"changing a span":           {starter, `UPDATE trace_span SET name = 'changed' WHERE trace = $1`, []any{kept.id}},
		"deleting a span":           {starter, `DELETE FROM trace_span WHERE trace = $1`, []any{kept.id}},
		"changing a trace's counts": {starter, `UPDATE trace SET spans = 0 WHERE id = $1`, []any{kept.id}},
		"deleting a trace":          {starter, `DELETE FROM trace WHERE id = $1`, []any{kept.id}},
	}
	for what, c := range refused {
		err := asApp(ctx, h.super, c.person, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, c.sql, c.args...)
			return err
		})
		permissionDenied(t, what, err)
	}

	// What the endpoint will write (#243) the database takes: a sender's span into a run's trace it reads, and into
	// a trace of its own.
	for what, trace := range map[string]any{"the run's trace": kept.id, "its own trace": owned} {
		err := asApp(ctx, h.super, starter, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, service, source, sender_id, sender_name,
				started_at, ended_at) VALUES ($1, 'dddddddddddddddd', 'app', 'my-app', 'otlp', $2, 'Starter', now(), now())`, trace, starter)
			return err
		})
		if err != nil {
			t.Errorf("a sender's span into %s = %v", what, err)
		}
	}
}
