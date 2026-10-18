//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// insertTool is the statement the registry runs, reduced to the columns the table requires.
const insertTool = `INSERT INTO tool (name, base_url, resource, issuer, authorization_endpoint, token_endpoint, client_id,
	client_auth_method, registration) VALUES ('exciter', $1, $1 || '/api/v1/mcp', $1, $1 || '/a', $1 || '/t', 'c',
	'client_secret_basic', 'dynamic') RETURNING id`

// The registry's guard is the database's too: a member writing straight through SQL as rotor_app is refused.
func TestOnlyAnAdministratorWritesATool(t *testing.T) {
	h := newHarness(t)
	admin, member := h.addPerson(t, "admin"), h.addPerson(t, "member")
	base := "https://exciter-" + uuid.NewString()[:8] + ".test"
	ctx := context.Background()

	err := h.cluster.Write(db.WithUser(ctx, member), func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		return tx.QueryRow(ctx, insertTool, base).Scan(&id)
	})
	permissionDenied(t, "a member adding a tool as rotor_app", err)

	var id uuid.UUID
	if err := h.cluster.Write(db.WithUser(ctx, admin), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, insertTool, base).Scan(&id)
	}); err != nil {
		t.Fatalf("an administrator adding a tool as rotor_app: %v", err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM tool WHERE id = $1`, id) })

	for what, statement := range map[string]string{
		"changing": `UPDATE tool SET client_id = 'mine' WHERE id = $1`,
		"removing": `DELETE FROM tool WHERE id = $1`,
	} {
		var touched int64
		if err := h.cluster.Write(db.WithUser(ctx, member), func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, statement, id)
			touched = tag.RowsAffected()
			return err
		}); err != nil || touched != 0 {
			t.Errorf("a member %s a tool as rotor_app touched %d rows, %v; want none", what, touched, err)
		}
	}

	seen := func(person uuid.UUID) int {
		var n int
		ctx := ctx
		if person != uuid.Nil {
			ctx = db.WithUser(ctx, person)
		}
		if err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM tool WHERE id = $1 AND client_id = 'c'`, id).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if seen(member) != 1 {
		t.Error("a member does not see the tool, unchanged, to connect to it")
	}
	if seen(uuid.Nil) != 0 {
		t.Error("nobody signed in sees the tool")
	}
}
