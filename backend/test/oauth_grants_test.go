//go:build integration

package test

// Adapted from Exciter at 3b340ca and ee72629, backend/test/oauth_grants_test.go and oauth_apps_test.go.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// toolNames are the tools an MCP answer lists.
func toolNames(result map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, each := range result["tools"].([]any) {
		out[each.(map[string]any)["name"].(string)] = true
	}
	return out
}

// grantOf is the grant a person holds for an assistant, read behind the policies.
func grantOf(t *testing.T, h *harness, person uuid.UUID, c *handClient) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM oauth_grant WHERE user_id = $1 AND client_id = $2`, person, c.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func personOf(t *testing.T, b *browser) uuid.UUID {
	t.Helper()
	_, me := b.me()
	return uuid.MustParse(me["user"].(map[string]any)["id"].(string))
}

// A grant made to read lists and reads, and writes nothing, whichever way it is asked.
func TestAGrantMadeToReadWritesNothing(t *testing.T) {
	h := newHarness(t)
	bob := signedIn(t, "bob")
	_, access := connectHand(t, h, bob, map[string]any{"allow": true, "readOnly": true})
	_, _, listed := mcpAs(t, access, "tools/list", nil)
	if names := toolNames(listed); names["start_run"] || names["cancel_run"] || !names["get_run"] || !names["validate_workflow"] {
		t.Errorf("a reading grant is offered %v", names)
	}
	called := callTool(t, access, "start_run", map[string]any{"workflowID": uuid.NewString(), "inputs": map[string]any{}})
	if called["isError"] != true || !strings.Contains(called["content"].([]any)[0].(map[string]any)["text"].(string), "read only") {
		t.Errorf("a reading grant starts a run: %v", called)
	}
	checked := callTool(t, access, "validate_workflow", map[string]any{"specYAML": "apiVersion: rotor.dev/v1"})
	if checked["isError"] == true {
		t.Errorf("a reading grant cannot check a workflow: %v", checked)
	}

	c, _, _ := register(t, h, newAddress(t), nil)
	page := sentTo(t, c.from, c.authorizeURL(t, map[string]string{"scope": "mcp:read"}))
	path := "/oauth/requests/" + page.Query().Get("request")
	if status, got := bob.api(http.MethodGet, path, nil); status != http.StatusOK || got["request"].(map[string]any)["readOnly"] != true {
		t.Errorf("the request to read reads %d %v", status, got)
	}
	if status, got := bob.api(http.MethodPost, path, map[string]any{"allow": true}); status != http.StatusUnprocessableEntity {
		t.Errorf("allowing everything to an assistant that asked to read answers %d %v", status, got)
	}
}

// A person lists and ends their own assistants, an administrator anybody's, and ending one stops its token at once;
// straight through SQL as rotor_app the grants hold, and an assistant makes or ends none.
func TestAPersonAndAnAdministratorEndAGrant(t *testing.T) {
	h := newHarness(t)
	bob, alice := signedIn(t, "bob"), signedIn(t, "alice")
	c, access := connectHand(t, h, bob, map[string]any{"allow": true})
	bobID := personOf(t, bob)
	grant := grantOf(t, h, bobID, c)

	status, mine := bob.api(http.MethodGet, "/oauth/grants", nil)
	if status != http.StatusOK || !strings.Contains(stringify(mine), grant.String()) || !strings.Contains(stringify(mine), "Hand-made assistant") {
		t.Errorf("bob lists %d %v", status, mine)
	}
	if status, _ := bob.api(http.MethodGet, "/oauth/grants/everyone", nil); status != http.StatusForbidden {
		t.Errorf("a member lists everybody's assistants with %d", status)
	}
	if status, _ := alice.api(http.MethodDelete, "/oauth/grants/"+grant.String(), nil); status != http.StatusNotFound {
		t.Errorf("alice ends bob's grant as her own with %d", status)
	}

	t.Run("straight through SQL as rotor_app", func(t *testing.T) {
		ctx := context.Background()
		member := h.addPerson(t, "member")
		var seen int
		_ = h.cluster.Read(db.WithUser(ctx, member), func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM oauth_grant WHERE id = $1`, grant).Scan(&seen)
		})
		if seen != 0 {
			t.Error("another member sees bob's grant")
		}
		for name, query := range map[string]string{
			"reading tokens": `SELECT count(*) FROM oauth_token`, "reading codes": `SELECT count(*) FROM oauth_code`,
			"reading requests": `SELECT count(*) FROM oauth_request`,
		} {
			err := h.cluster.Read(db.WithUser(ctx, bobID), func(ctx context.Context, tx db.DBTX) error {
				var n int
				return tx.QueryRow(ctx, query).Scan(&n)
			})
			permissionDenied(t, name, err)
		}
		err := h.cluster.Write(db.WithUser(ctx, member), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only) VALUES ($1, $2, $3, false)`, bobID, c.id, callback)
			return err
		})
		if err == nil {
			t.Error("a grant is made in somebody else's name")
		}
		// An assistant acting for bob ends nothing of his, and makes nothing in his name.
		through := db.WithVia(db.WithUser(ctx, bobID), grant)
		_ = h.cluster.Write(through, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `DELETE FROM oauth_grant WHERE id = $1`, grant)
			if err == nil && tag.RowsAffected() != 0 {
				t.Error("an assistant ends its own grant through SQL")
			}
			return errors.New("roll back")
		})
	})

	if status, _ := bob.api(http.MethodDelete, "/oauth/grants/"+grant.String(), nil); status != http.StatusNoContent {
		t.Fatalf("bob disconnects with %d", status)
	}
	if status, _, _ := mcpAs(t, access, "tools/list", nil); status != http.StatusUnauthorized {
		t.Errorf("a disconnected assistant still works: %d", status)
	}

	c2, access2 := connectHand(t, h, bob, map[string]any{"allow": true})
	grant2 := grantOf(t, h, bobID, c2)
	if status, everyone := alice.api(http.MethodGet, "/oauth/grants/everyone", nil); status != http.StatusOK || !strings.Contains(stringify(everyone), grant2.String()) {
		t.Errorf("alice lists everybody's assistants with %d %v", status, everyone)
	}
	if status, _ := alice.api(http.MethodDelete, "/oauth/grants/everyone/"+grant2.String(), nil); status != http.StatusNoContent {
		t.Errorf("an administrator ends bob's grant with %d", status)
	}
	if status, _, _ := mcpAs(t, access2, "tools/list", nil); status != http.StatusUnauthorized {
		t.Errorf("an assistant an administrator disconnected still works: %d", status)
	}
	if n := countRows(t, h, `SELECT count(*) FROM audit_log WHERE action = 'oauth.revoked' AND target_id = ANY ($1)
		AND data->>'by' IN ('person', 'administrator')`, []string{grant.String(), grant2.String()}); n != 2 {
		t.Errorf("%d audit entries for the two disconnections, want 2", n)
	}
}

// Switching somebody off ends their grants, as it ends their sessions.
func TestSwitchingSomebodyOffEndsTheirGrants(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	member := h.addPerson(t, "member")
	c, status, _ := register(t, h, newAddress(t), nil)
	if status != http.StatusCreated {
		t.Fatalf("register: %d", status)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only) VALUES ($1, $2, $3, false)`,
		member, c.id, callback); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, member); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `SELECT count(*) FROM oauth_grant WHERE user_id = $1`, member); n != 0 {
		t.Errorf("%d grants outlive their person being switched off", n)
	}
}

// Administrators switch assistants off for everyone: discovery goes, tokens are refused without pointing anywhere,
// and the grants work again when switched on.
func TestAssistantsAreSwitchedOffAndOn(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	bob, alice := signedIn(t, "bob"), signedIn(t, "alice")
	c, access := connectHand(t, h, bob, map[string]any{"allow": true})

	if status, _ := bob.api(http.MethodPut, "/settings/assistants", map[string]any{"connectionsOn": false}); status != http.StatusForbidden {
		t.Errorf("a member switches assistants off with %d", status)
	}
	if status, got := alice.api(http.MethodPut, "/settings/assistants", map[string]any{"connectionsOn": false}); status != http.StatusOK || got["connectionsOn"] != false {
		t.Fatalf("switching off answers %d %v", status, got)
	}
	anyone := newAddress(t).client(false)
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server"} {
		resp, err := anyone.Get(rotorApp(t) + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s while off answers %d", path, resp.StatusCode)
		}
	}
	if status, challenge, _ := mcpAs(t, access, "tools/list", nil); status != http.StatusUnauthorized || strings.Contains(challenge, "resource_metadata") {
		t.Errorf("a token while off answers %d %q", status, challenge)
	}
	if to := sentTo(t, c.from, c.authorizeURL(t, nil)); to.Query().Get("error") != "switched_off" {
		t.Errorf("an authorization while off goes to %s", to)
	}
	if _, status, got := register(t, h, newAddress(t), nil); status != http.StatusBadRequest || got["error"] != "temporarily_unavailable" {
		t.Errorf("a registration while off answers %d %v", status, got)
	}
	if status, _ := c.form(t, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rotor_ort_x"}}); status != http.StatusBadRequest {
		t.Errorf("a refresh while off answers %d", status)
	}

	if status, _ := alice.api(http.MethodPut, "/settings/assistants", map[string]any{"connectionsOn": true}); status != http.StatusOK {
		t.Fatalf("switching on answers %d", status)
	}
	callTool(t, access, "whoami", map[string]any{})
	if n := countRows(t, h, `SELECT count(*) FROM audit_log WHERE action = 'oauth.switched' AND actor_user_id = $1
		AND created_at > now() - interval '5 minutes'`, personOf(t, alice)); n < 2 {
		t.Errorf("%d audit entries for switching, want 2", n)
	}
}

// insufficientPrivilege fails unless err is the database refusing what only a person may do.
func insufficientPrivilege(t *testing.T, what string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("%s = %v, want insufficient privilege (42501)", what, err)
	}
}
