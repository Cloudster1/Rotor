//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// actionUnderTest marks the entries this suite writes, so each test reads back only its own.
const actionUnderTest = "test.recorded"

func TestTheAuditLogIsOnlyEverAddedTo(t *testing.T) {
	h := newHarness(t)
	admin := h.addPerson(t, "admin")
	ctx := db.WithUser(context.Background(), admin)
	target := uuid.NewString()

	err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return audit.Write(ctx, tx, audit.Entry{Action: actionUnderTest, TargetType: "test", TargetID: target, Actor: admin,
			Data: map[string]any{"clientSecret": "s3cr3t"}, IP: "192.0.2.1"})
	})
	if err != nil {
		t.Fatalf("an administrator could not write their own entry: %v", err)
	}
	var data string
	if err := h.super.QueryRow(context.Background(),
		`SELECT data::text FROM audit_log WHERE target_id = $1`, target).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if data != `{"clientSecret": "[redacted]"}` {
		t.Errorf("the entry holds %s", data)
	}

	for _, update := range []struct {
		role string
		run  func(context.Context, func(context.Context, db.DBTX) error) error
	}{{"rotor_app", h.cluster.Write}, {"rotor_admin", h.cluster.WriteAdmin}} {
		err := update.run(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE audit_log SET action = 'test.rewritten' WHERE target_id = $1`, target)
			return err
		})
		permissionDenied(t, "UPDATE as "+update.role, err)
		err = update.run(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE target_id = $1`, target)
			return err
		})
		permissionDenied(t, "DELETE as "+update.role, err)
	}
}

func TestOnlyAdministratorsReadTheAuditLogAndNobodyWritesAsAnother(t *testing.T) {
	h := newHarness(t)
	admin, member := h.addPerson(t, "admin"), h.addPerson(t, "member")
	target := uuid.NewString()
	if _, err := h.super.Exec(context.Background(),
		`INSERT INTO audit_log (actor_user_id, action, target_type, target_id) VALUES ($1, $2, 'test', $3)`,
		admin, actionUnderTest, target); err != nil {
		t.Fatal(err)
	}

	seen := func(person uuid.UUID) int {
		var n int
		err := h.cluster.Read(db.WithUser(context.Background(), person), func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE target_id = $1`, target).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := seen(admin); n != 1 {
		t.Errorf("an administrator sees %d entries, want 1", n)
	}
	if n := seen(member); n != 0 {
		t.Errorf("a member sees %d entries, want none", n)
	}

	// Straight through SQL as rotor_app, the member cannot write an act down to the administrator.
	err := h.cluster.Write(db.WithUser(context.Background(), member), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit_log (actor_user_id, action, target_type) VALUES ($1, $2, 'test')`, admin, actionUnderTest)
		return err
	})
	if err == nil {
		t.Error("a member wrote an entry in an administrator's name")
	}
}

func TestPeopleSeeThemselvesAndNobodyElse(t *testing.T) {
	h := newHarness(t)
	me, other := h.addPerson(t, "member"), h.addPerson(t, "admin")
	var ids []uuid.UUID
	err := h.cluster.Read(db.WithUser(context.Background(), me), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id FROM app_user WHERE id = ANY($1)`, []uuid.UUID{me, other})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != me {
		t.Errorf("a member sees %v, want only themselves", ids)
	}

	err = h.cluster.Write(db.WithUser(context.Background(), me), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET role = 'admin' WHERE id = $1`, me)
		return err
	})
	permissionDenied(t, "a member promoting themselves as rotor_app", err)
}
