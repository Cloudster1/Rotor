//go:build integration

package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// The service's checks are the database's too: straight through SQL as rotor_app, only the run's people and the
// approval's group see it, nobody opens or decides one, and a decided approval never moves again.
func TestTheDatabaseShowsAnApprovalToItsPeopleAndLetsNobodyWriteIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	starter, approver, outsider := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	group := "reviewers-" + uuid.NewString()[:8]
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET groups = ARRAY[$2] WHERE id = $1`, approver, group); err != nil {
		t.Fatal(err)
	}
	var workflowID, runID, approvalID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ('approved', $1) RETURNING id`, starter).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, workflowID) })
	if _, err := h.super.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by)
		VALUES ($1, 1, 'x', '{}', 'h', $2)`, workflowID, starter); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO run (workflow_id, version, user_id, data_key, sealed_inputs)
		VALUES ($1, 1, $2, $3, $4) RETURNING id`, workflowID, starter, placeholderKey, placeholderSealed).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM run WHERE id = $1`, runID) })
	if _, err := h.super.Exec(ctx, `INSERT INTO run_event (run_id, seq, type, sealed_payload) VALUES ($1, 1, 'interrupt', $2)`,
		runID, placeholderSealed); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO approval (run_id, starter_id, workflow_id, workflow_name, started_by_name, node,
			interrupt_id, sealed_message, sealed_preview, sealed_form_values, approver_group, expires_at)
		VALUES ($1, $2, $3, 'approved', 'Starter', 'review', 'i-1', $6, $6, $6, $4, $5) RETURNING id`,
		runID, starter, workflowID, group, time.Now().Add(time.Hour), placeholderSealed).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}

	sees := func(person uuid.UUID, query string) int {
		t.Helper()
		var n int
		if err := h.cluster.Read(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, query, runID).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, c := range []struct {
		name   string
		person uuid.UUID
		want   int
	}{{"the starter", starter, 1}, {"a member of the group", approver, 1}, {"an outsider", outsider, 0}} {
		for _, table := range []string{"approval", "run", "run_event"} {
			column := "run_id"
			if table == "run" {
				column = "id"
			}
			if n := sees(c.person, `SELECT count(*) FROM `+table+` WHERE `+column+` = $1`); n != c.want {
				t.Errorf("%s sees %d rows of %s, want %d", c.name, n, table, c.want)
			}
		}
	}

	as := func(person uuid.UUID, statement string, args ...any) error {
		return h.cluster.Write(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, statement, args...)
			return err
		})
	}
	permissionDenied(t, "an approver deciding as rotor_app", as(approver, `UPDATE approval SET status = 'approved', decided_at = now() WHERE id = $1`, approvalID))
	permissionDenied(t, "a starter opening an approval as rotor_app", as(starter, `INSERT INTO approval (run_id, starter_id, workflow_id,
		workflow_name, started_by_name, node, interrupt_id, sealed_message, sealed_preview, sealed_form_values, expires_at)
		VALUES ($1, $2, $3, 'x', 'x', 'n', 'i-2', $4, $4, $4, now())`,
		runID, starter, workflowID, placeholderSealed))

	if _, err := h.super.Exec(ctx, `UPDATE approval SET status = 'rejected', decided_at = now() WHERE id = $1`, approvalID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE approval SET status = 'approved' WHERE id = $1`, approvalID); err == nil {
		t.Error("a rejected approval became approved")
	}
}
