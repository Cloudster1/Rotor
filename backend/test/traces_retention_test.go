//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

// keepTracesMigrated is the version once 01500_keep_traces.sql has run; lastBeforeTraces the one before it.
const (
	keepTracesMigrated = 1500
	lastBeforeTraces   = 1420
)

// backdate makes a trace look as if it took its last span days ago.
func (h *harness) backdate(t *testing.T, trace uuid.UUID, days int) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `UPDATE trace SET last_span_at = now() - make_interval(days => $2) WHERE id = $1`,
		trace, days); err != nil {
		t.Fatal(err)
	}
}

// Spans of a trace that took none for the retention go: a run's trace keeps its row and counts and says it expired,
// a sender's own trace goes whole, and a shorter retention applies to traces kept already.
func TestSpansPastTheRetentionGo(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	store := h.traceStore(t)
	keep := func(spans int) (tracedByHand, keptTrace) {
		r := h.runWithTrace(t, person)
		if err := store.Keep(ctx, r.id, r.dataKey, runnerSpans(r, spans)); err != nil {
			t.Fatal(err)
		}
		kept, _ := h.keptTraceOf(t, r.id)
		return r, kept
	}
	old, oldTrace := keep(3)
	recent, _ := keep(1)
	shortened, shortenedTrace := keep(1)
	h.backdate(t, oldTrace.id, 20)
	h.backdate(t, shortenedTrace.id, 2)
	var owned uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO trace (trace_id, owner_id) VALUES ($1, $2) RETURNING id`, randomTraceID(), person).
		Scan(&owned); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO trace_span (trace, span_id, name, service, source, sender_id, started_at, ended_at)
		VALUES ($1, 'eeeeeeeeeeeeeeee', 'app', 'my-app', 'otlp', $2, now(), now())`, owned, person); err != nil {
		t.Fatal(err)
	}
	h.backdate(t, owned, 20)

	// rotor_admin deletes no span the retention keeps, whatever it asks.
	err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM trace_span s USING trace t WHERE t.id = s.trace AND t.run_id = $1`, recent.id)
		if err == nil && tag.RowsAffected() != 0 {
			t.Error("rotor_admin deleted a span the retention keeps")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	permissionDenied(t, "rotor_admin changing a span", h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE trace_span SET name = 'changed' WHERE trace = $1`, oldTrace.id)
		return err
	}))

	// A day's retention reaches the trace kept two days, and the default's 14 the one of 20.
	var days int
	if err := h.super.QueryRow(ctx, `SELECT retention_days FROM trace_setting`).Scan(&days); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `UPDATE trace_setting SET retention_days = $1`, days)
	})
	if _, err := h.super.Exec(ctx, `UPDATE trace_setting SET retention_days = 1`); err != nil {
		t.Fatal(err)
	}

	wrong := func() []string {
		var out []string
		for name, c := range map[string]struct {
			run         uuid.UUID
			spans, rows int
			expired     bool
		}{"a run's trace past the retention": {old.id, 3, 0, true}, "a run's trace within it": {recent.id, 1, 1, false},
			"a run's trace past a shortened retention": {shortened.id, 1, 0, true}} {
			got, ok := h.keptTraceOf(t, c.run)
			if !ok || got.spans != c.spans || got.rows != c.rows || got.expired != c.expired {
				out = append(out, fmt.Sprintf("%s holds %+v (kept %v), want %d counted, %d left, expired %v", name, got, ok, c.spans, c.rows, c.expired))
			}
		}
		if h.kept(t, "trace", "id", owned) || h.kept(t, "trace_span", "trace", owned) {
			out = append(out, "a sender's own trace past the retention is kept")
		}
		return out
	}
	// The stack's worker sweeps too and holds the lock while it does; a sweep that finds it held is tried again.
	deadline := time.Now().Add(sweepWait)
	for {
		if err := traces.NewService(h.cluster, slog.New(slog.DiscardHandler)).Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		problems := wrong()
		if len(problems) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after sweeping: %v", problems)
		}
		time.Sleep(time.Second)
	}

	// A run deleted sooner takes its trace with it.
	if _, err := h.super.Exec(ctx, `DELETE FROM run WHERE id = $1`, recent.id); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.keptTraceOf(t, recent.id); ok {
		t.Error("a deleted run's trace is kept")
	}
}

// Everyone signed in reads how long traces are kept; an administrator alone changes it, audited.
func TestAnAdministratorSetsHowLongTracesAreKept(t *testing.T) {
	h := newHarness(t)
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	status, before := bob.api(http.MethodGet, "/settings/traces", nil)
	if status != http.StatusOK || before["retentionDays"] == nil || before["keepContent"] == nil {
		t.Fatalf("a member reads the trace setting with %d %v", status, before)
	}
	t.Cleanup(func() {
		alice.api(http.MethodPut, "/settings/traces", map[string]any{"retentionDays": before["retentionDays"], "keepContent": before["keepContent"]})
	})

	if status, _ := bob.api(http.MethodPut, "/settings/traces", map[string]any{"retentionDays": 1, "keepContent": true}); status != http.StatusForbidden {
		t.Errorf("a member changes the trace setting with %d", status)
	}
	for _, days := range []int{0, 91} {
		status, answer := alice.api(http.MethodPut, "/settings/traces", map[string]any{"retentionDays": days, "keepContent": false})
		fields, _ := answer["error"].(map[string]any)["fields"].(map[string]any)
		if status != http.StatusUnprocessableEntity || fields["retentionDays"] == nil {
			t.Errorf("%d days = %d %v, want 422 naming retentionDays", days, status, answer)
		}
	}
	status, set := alice.api(http.MethodPut, "/settings/traces", map[string]any{"retentionDays": 30, "keepContent": false})
	if status != http.StatusOK || set["retentionDays"] != float64(30) || set["keepContent"] != false || set["changedBy"] == nil {
		t.Fatalf("keeping traces 30 days = %d %v", status, set)
	}
	var data string
	if err := h.super.QueryRow(context.Background(), `SELECT data::text FROM audit_log WHERE action = 'traces.changed'
		ORDER BY created_at DESC LIMIT 1`).Scan(&data); err != nil || data == "" {
		t.Errorf("the change is not audited: %v", err)
	}

	member := h.addPerson(t, "member")
	err := h.cluster.Write(db.WithUser(context.Background(), member), func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE trace_setting SET retention_days = 1, keep_content = true`)
		if err == nil && tag.RowsAffected() != 0 {
			t.Error("a member changed the trace setting straight through SQL")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The migration goes down and up again on a database of its own, taking the run's trace id with it.
func TestTheTraceMigrationGoesDownAndUp(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.newScratch(t, keepTracesMigrated)
	present := func() (int, bool) {
		var tables int
		var column bool
		if err := s.super.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE tablename IN ('trace', 'trace_span', 'trace_setting')`).
			Scan(&tables); err != nil {
			t.Fatal(err)
		}
		if err := s.super.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'run'
			AND column_name = 'trace_id')`).Scan(&column); err != nil {
			t.Fatal(err)
		}
		return tables, column
	}
	if tables, column := present(); tables != 3 || !column {
		t.Fatalf("the migration made %d trace tables and run.trace_id %v", tables, column)
	}
	s.down(lastBeforeTraces)
	if tables, column := present(); tables != 0 || column {
		t.Errorf("after migrating down %d trace tables and run.trace_id %v stay", tables, column)
	}
	s.up(0)
	if tables, column := present(); tables != 3 || !column {
		t.Errorf("migrating up again made %d trace tables and run.trace_id %v", tables, column)
	}
}
