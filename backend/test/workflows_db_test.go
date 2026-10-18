//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// The api's checks are the database's too: a member writing straight through SQL as rotor_app is refused.
func TestTheDatabaseKeepsWorkflowsToTheirOwnersAndVersionsUnchanged(t *testing.T) {
	h := newHarness(t)
	owner, other := h.addPerson(t, "member"), h.addPerson(t, "member")
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ('private', $1) RETURNING id`, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by)
		VALUES ($1, 1, 'x', '{}', 'h', $2)`, id, owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, id) })
	as := func(person uuid.UUID, statement string, args ...any) (int64, error) {
		var touched int64
		err := h.cluster.Write(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, statement, args...)
			touched = tag.RowsAffected()
			return err
		})
		return touched, err
	}

	var seen int
	if err := h.cluster.Read(db.WithUser(ctx, other), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM workflow_version WHERE workflow_id = $1`, id).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Errorf("another member sees %d versions of a private workflow (%v)", seen, err)
	}
	if touched, err := as(other, `UPDATE workflow SET sharing = 'everyone' WHERE id = $1`, id); err != nil || touched != 0 {
		t.Errorf("another member shared a workflow: %d rows, %v", touched, err)
	}
	// The numbering trigger, which cannot see the workflow either, refuses it before the policy does.
	if _, err := as(other, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by) VALUES ($1, 2, 'y', '{}', 'h', $2)`, id, other); err == nil {
		t.Error("another member added a version as rotor_app")
	}
	_, err := as(owner, `UPDATE workflow_version SET spec_yaml = 'changed' WHERE workflow_id = $1`, id)
	permissionDenied(t, "the owner rewriting a version as rotor_app", err)
	_, err = as(owner, `DELETE FROM workflow WHERE id = $1`, id)
	permissionDenied(t, "the owner deleting a workflow as rotor_app", err)
	if _, err := as(owner, `UPDATE workflow SET owner_id = $2 WHERE id = $1`, id, other); err == nil {
		t.Error("the owner handed the workflow to another")
	}
	if _, err := as(owner, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by) VALUES ($1, 3, 'y', '{}', 'h', $2)`, id, owner); err == nil {
		t.Error("a version skipped a number")
	}
	if touched, err := as(owner, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by) VALUES ($1, 2, 'y', '{}', 'h', $2)`, id, owner); err != nil || touched != 1 {
		t.Errorf("the owner adding the next version: %d, %v", touched, err)
	}
}
