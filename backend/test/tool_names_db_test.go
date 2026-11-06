//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// toolNamesMigrated is the version once 01530_tool_names.sql has run; destinationsMigrated is the one before it.
const toolNamesMigrated = 1530

// nameRefused fails the test unless err is the database refusing a second tool of a name.
func nameRefused(t *testing.T, what string, err error) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" || pg.ConstraintName != "tool_name_unique" {
		t.Errorf("%s: %v, want tool_name_unique refusing it", what, err)
	}
}

// twinOf copies a tool under its own name at another address straight through SQL as the superuser, with the
// triggers off as before #267, which is the only way left to hold two tools of one name.
func twinOf(t *testing.T, h *harness, id string) string {
	t.Helper()
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	var twin string
	if err := tx.QueryRow(ctx, `INSERT INTO tool (name, product, base_url, resource, mcp_url, issuer, authorization_endpoint,
		token_endpoint, client_id, client_auth_method, registration, status)
		SELECT name, product, base_url || '/twin', resource, mcp_url, issuer, authorization_endpoint, token_endpoint, client_id,
			client_auth_method, registration, status FROM tool WHERE id = $1 RETURNING id`, id).Scan(&twin); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM tool WHERE id = $1`, twin) })
	return twin
}

// A second tool of a name is refused by the database whoever writes it, case aside, as rotor_app and as the superuser.
func TestTheDatabaseRefusesASecondToolOfAName(t *testing.T) {
	h := newHarness(t)
	admin := h.addPerson(t, "admin")
	ctx := db.WithUser(context.Background(), admin)
	name := "guarded-" + uuid.NewString()[:8]
	add := func(base, name string) (uuid.UUID, error) {
		var id uuid.UUID
		err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, insertTool, base, name).Scan(&id)
		})
		if err == nil {
			t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM tool WHERE id = $1`, id) })
		}
		return id, err
	}
	if _, err := add("https://"+name+".test", name); err != nil {
		t.Fatalf("an administrator adding %s: %v", name, err)
	}
	_, err := add("https://"+name+"-two.test", strings.ToUpper(name))
	nameRefused(t, "adding the name in upper case as rotor_app", err)

	other, err := add("https://"+name+"-other.test", name+"-other")
	if err != nil {
		t.Fatal(err)
	}
	err = h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE tool SET name = $2 WHERE id = $1`, other, name)
		return err
	})
	nameRefused(t, "renaming a tool to a name another has as rotor_app", err)
	_, err = h.super.Exec(context.Background(), `INSERT INTO tool (name, product, base_url, resource, mcp_url, issuer,
		authorization_endpoint, token_endpoint, client_id, client_auth_method, registration)
		SELECT name, product, base_url || '/again', resource, mcp_url, issuer, authorization_endpoint, token_endpoint, client_id,
			client_auth_method, registration FROM tool WHERE id = $1`, other)
	nameRefused(t, "the superuser copying a tool under its name with the triggers on", err)
}

// A name two tools share from before is its own problem wherever a binding is resolved: validating, saving and
// starting a run each say so with tool_ambiguous, and renaming one of them settles it (#267).
func TestANameSharedFromBeforeIsItsOwnProblem(t *testing.T) {
	h := newHarness(t)
	product := "shared-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, toolID := connected(t, stub)
	alice := signedIn(t, "alice")
	workflow := createWorkflow(t, bob, bindsTool(product))
	twin := twinOf(t, h, toolID.String())

	if codes := problemsAt(t, bob, bindsTool(product)); codes["/tools/t/tool"] != "tool_ambiguous" {
		t.Errorf("binding a shared name validates as %v, want tool_ambiguous at /tools/t/tool", codes)
	}
	status, answer := bob.api(http.MethodPost, "/workflows", map[string]any{"specYAML": bindsTool(product)})
	if e := errorOf(answer); status != http.StatusUnprocessableEntity || e["code"] != "workflow_invalid" ||
		!strings.Contains(stringOf(e["fields"]), "rename all but one") {
		t.Errorf("saving a binding of a shared name = %d %v", status, answer)
	}
	status, answer = bob.api(http.MethodPost, "/workflows/"+workflow+"/runs", map[string]any{"inputs": map[string]any{}})
	if e := errorOf(answer); status != http.StatusConflict || e["code"] != "tool_ambiguous" ||
		!strings.Contains(stringOf(e["message"]), "Several tools are called "+product) || !strings.Contains(stringOf(e["fields"]), product) {
		t.Errorf("starting a run binding a shared name = %d %v", status, answer)
	}

	if status, answer := alice.api(http.MethodPut, "/tools/"+twin+"/name", map[string]any{"name": product + "-twin"}); status != http.StatusOK {
		t.Fatalf("renaming the twin = %d %v", status, answer)
	}
	if codes := problemsAt(t, bob, bindsTool(product)); len(codes) != 0 {
		t.Errorf("binding the name once the twin is renamed validates as %v, want no problems", codes)
	}
}

// The migration keeps the names installations already share, makes each tool's product its name, refuses a new
// duplicate, and goes back down and up again.
func TestTheToolNamesMigrationKeepsSharedNamesAndRefusesNewOnes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s := h.newScratch(t, destinationsMigrated)
	old := `INSERT INTO tool (name, base_url, resource, mcp_url, issuer, authorization_endpoint, token_endpoint, client_id,
		client_auth_method, registration) VALUES ('exciter', $1, $1, $1, $1, $1, $1, 'c', 'client_secret_basic', 'dynamic')`
	for _, base := range []string{"https://one.test", "https://two.test"} {
		if _, err := s.super.Exec(ctx, old, base); err != nil {
			t.Fatal(err)
		}
	}

	s.up(toolNamesMigrated)
	var shared int
	if err := s.super.QueryRow(ctx, `SELECT count(*) FROM tool WHERE name = 'exciter' AND product = 'exciter'`).Scan(&shared); err != nil || shared != 2 {
		t.Errorf("after the migration %d tools are exciter by name and product (%v), want 2", shared, err)
	}
	_, err := s.super.Exec(ctx, `INSERT INTO tool (name, product, base_url, resource, mcp_url, issuer, authorization_endpoint,
		token_endpoint, client_id, client_auth_method, registration)
		VALUES ('Exciter', 'exciter', 'https://three.test', 'r', 'm', 'i', 'a', 't', 'c', 'client_secret_basic', 'dynamic')`)
	nameRefused(t, "a third Exciter after the migration", err)

	s.down(destinationsMigrated)
	var product int
	if err := s.super.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'tool' AND column_name = 'product'`).
		Scan(&product); err != nil || product != 0 {
		t.Errorf("going back down left the product column (%d, %v)", product, err)
	}
	if _, err := s.super.Exec(ctx, old, "https://three.test"); err != nil {
		t.Errorf("before the migration a third exciter is refused: %v", err)
	}
	s.up(toolNamesMigrated)
}
