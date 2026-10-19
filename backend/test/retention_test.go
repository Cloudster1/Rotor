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
	"github.com/cloudster1/rotor/backend/internal/retention"
)

// sweepWait is how long a test waits for a sweep the stack's worker may be holding the lock for.
const sweepWait = 15 * time.Second

// keptWorkflow makes a workflow whose current version keeps its runs for retention ("" for the installation's).
func (h *harness) keptWorkflow(t *testing.T, owner uuid.UUID, retention string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ($1, $2) RETURNING id`, uniqueName("kept"), owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM workflow WHERE id = $1`, id) })
	spec := `{}`
	if retention != "" {
		spec = `{"retention": "` + retention + `"}`
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash) VALUES ($1, 1, 'x', $2, 'h')`,
		id, spec); err != nil {
		t.Fatal(err)
	}
	return id
}

// endedRun stores a run of the workflow that ended agoDays ago and was torn down, with an event and an approval;
// a run with agoDays below zero is still running.
func (h *harness) endedRun(t *testing.T, workflow, person uuid.UUID, agoDays int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	status := "running"
	if agoDays >= 0 {
		status = "succeeded"
	}
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO run (workflow_id, version, user_id, status, data_key, sealed_inputs,
			created_at, finished_at, torn_down_at)
		VALUES ($1, 1, $2, $3, $4, $5, now() - interval '400 days',
			CASE WHEN $6 >= 0 THEN now() - make_interval(days => $6) END, CASE WHEN $6 >= 0 THEN now() END) RETURNING id`,
		workflow, person, status, placeholderKey, placeholderSealed, agoDays).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM run WHERE id = $1`, id) })
	if _, err := h.super.Exec(ctx, `INSERT INTO run_event (run_id, seq, type, sealed_payload) VALUES ($1, 1, 'run.finished', $2)`,
		id, placeholderSealed); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO approval (run_id, starter_id, workflow_id, workflow_name, started_by_name, node,
			interrupt_id, message, status, decided_at, expires_at)
		VALUES ($1, $2, $3, 'kept', 'Starter', 'review', 'i-1', 'Go on?', 'approved', now(), now())`, id, person, workflow); err != nil {
		t.Fatal(err)
	}
	return id
}

func (h *harness) kept(t *testing.T, table, column string, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE `+column+` = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestRunsPastTheirRetentionAreDeletedWithAllTheyHeld(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	plain, short, long := h.keptWorkflow(t, person, ""), h.keptWorkflow(t, person, "7d"), h.keptWorkflow(t, person, "180d")
	old, recent := h.endedRun(t, plain, person, 100), h.endedRun(t, plain, person, 10)
	shortGone, longCapped := h.endedRun(t, short, person, 10), h.endedRun(t, long, person, 100)
	running := h.endedRun(t, plain, person, -1)
	var audited int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&audited); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		id   uuid.UUID
		kept bool
	}{
		"a run past the installation's 90 days": {old, false}, "a run within them": {recent, true},
		"a run past its workflow's 7 days": {shortGone, false}, "a run whose workflow asks for 180 days": {longCapped, false},
		"a run still going": {running, true},
	}
	wrong := func() []string {
		var out []string
		for name, c := range cases {
			for _, table := range []string{"run", "run_event", "approval"} {
				column := "run_id"
				if table == "run" {
					column = "id"
				}
				if got := h.kept(t, table, column, c.id); got != c.kept {
					out = append(out, fmt.Sprintf("%s: its %s rows kept = %v, want %v", name, table, got, c.kept))
				}
			}
		}
		return out
	}
	// The stack's worker sweeps too and holds the lock while it does; a sweep that finds it held is tried again.
	deadline := time.Now().Add(sweepWait)
	for {
		if err := retention.NewService(h.cluster, slog.New(slog.DiscardHandler)).Sweep(ctx); err != nil {
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
	for _, workflow := range []uuid.UUID{plain, short, long} {
		if !h.audited(t, "run.data_deleted", workflow.String()) {
			t.Errorf("no run.data_deleted entry for workflow %s", workflow)
		}
	}
	var after int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&after); err != nil || after < audited {
		t.Errorf("the audit log went from %d to %d entries (%v); nothing deletes it", audited, after, err)
	}
	permissionDenied(t, "rotor_admin deleting a run's events alone", h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM run_event WHERE run_id = $1`, recent)
		return err
	}))
}

func TestAnAdministratorSetsTheRetentionAndNobodyElse(t *testing.T) {
	h := newHarness(t)
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	_, before := alice.api(http.MethodGet, "/settings/retention", nil)
	t.Cleanup(func() {
		alice.api(http.MethodPut, "/settings/retention", map[string]any{"runDataDays": before["runDataDays"]})
	})

	if status, _ := bob.api(http.MethodGet, "/settings/retention", nil); status != http.StatusForbidden {
		t.Errorf("a member reads the retention with %d", status)
	}
	if status, _ := bob.api(http.MethodPut, "/settings/retention", map[string]any{"runDataDays": 1}); status != http.StatusForbidden {
		t.Errorf("a member sets the retention with %d", status)
	}
	for _, days := range []int{0, 3651} {
		if status, answer := alice.api(http.MethodPut, "/settings/retention", map[string]any{"runDataDays": days}); status != http.StatusUnprocessableEntity {
			t.Errorf("%d days = %d %v, want 422", days, status, answer)
		}
	}
	status, set := alice.api(http.MethodPut, "/settings/retention", map[string]any{"runDataDays": 30})
	if status != http.StatusOK || set["runDataDays"] != float64(30) || set["maxDays"] != float64(3650) || set["changedBy"] == nil {
		t.Fatalf("setting 30 days = %d %v", status, set)
	}
	var data string
	if err := h.super.QueryRow(context.Background(), `SELECT data::text FROM audit_log WHERE action = 'retention.changed'
		ORDER BY created_at DESC LIMIT 1`).Scan(&data); err != nil || data == "" {
		t.Errorf("the change is not audited: %v", err)
	}

	member := h.addPerson(t, "member")
	err := h.cluster.Write(db.WithUser(context.Background(), member), func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE retention_setting SET run_data_days = 1`)
		if err == nil && tag.RowsAffected() != 0 {
			t.Error("a member changed the retention straight through SQL")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
