//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// The service's checks are the database's too: straight through SQL as rotor_app, a person sees and changes only
// their own triggers, writes no firing, and starts no run under someone else's trigger.
func TestTheDatabaseKeepsTriggersToTheirOwners(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice, bob := h.addPerson(t, "member"), h.addPerson(t, "member")
	var workflowID, triggerID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id, sharing) VALUES ('shared', $1, 'everyone') RETURNING id`, alice).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(ctx, `DELETE FROM run WHERE workflow_id = $1`, workflowID)
		_, _ = h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, workflowID)
	})
	if _, err := h.super.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by)
		VALUES ($1, 1, 'x', '{}', 'h', $2)`, workflowID, alice); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO trigger (workflow_id, owner_id, name, kind, cron, time_zone)
		VALUES ($1, $2, 'alices', 'schedule', '0 8 * * 1', 'UTC') RETURNING id`, workflowID, alice).Scan(&triggerID); err != nil {
		t.Fatal(err)
	}
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
	if err := h.cluster.Read(db.WithUser(ctx, bob), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM trigger WHERE id = $1`, triggerID).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Errorf("another person sees %d of alice's triggers (%v)", seen, err)
	}
	if touched, err := as(bob, `UPDATE trigger SET cron = '* * * * *' WHERE id = $1`, triggerID); err != nil || touched != 0 {
		t.Errorf("another person changed alice's trigger: %d rows, %v", touched, err)
	}
	if _, err := as(bob, `INSERT INTO trigger (workflow_id, owner_id, name, kind, cron, time_zone) VALUES ($1, $2, 'forged', 'schedule', '0 8 * * 1', 'UTC')`,
		workflowID, alice); err == nil {
		t.Error("a person made a trigger owned by someone else")
	}
	_, err := as(alice, `INSERT INTO trigger_fire (trigger_id, fire_key, due_at) VALUES ($1, 'forged', now())`, triggerID)
	permissionDenied(t, "an owner queueing a firing as rotor_app", err)
	if _, err := as(bob, `INSERT INTO run (workflow_id, version, user_id, trigger_id, data_key, sealed_inputs) VALUES ($1, 1, $2, $3, $4, $5)`,
		workflowID, bob, triggerID, placeholderKey, placeholderSealed); err == nil {
		t.Error("a person started a run under someone else's trigger")
	}
	if touched, err := as(alice, `INSERT INTO run (workflow_id, version, user_id, trigger_id, data_key, sealed_inputs) VALUES ($1, 1, $2, $3, $4, $5)`,
		workflowID, alice, triggerID, placeholderKey, placeholderSealed); err != nil || touched != 1 {
		t.Errorf("the owner starting a run under their trigger = %d rows, %v", touched, err)
	}
}
