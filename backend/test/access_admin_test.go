//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/perm"
)

// The matrix makes a role, ticks what it grants with the whole list each time, and deletes it; the built-ins stay.
func TestRolesAreMadeTickedAndDeletedButTheBuiltinsStay(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	name := uniqueName("Reviewer")
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM role WHERE name = $1`, name) })

	status, made := alice.api(http.MethodPost, "/roles", map[string]any{"name": name, "permissions": []string{"workflow.read"}})
	if status != http.StatusCreated {
		t.Fatalf("making a role = %d %v", status, made)
	}
	role, _ := made["role"].(map[string]any)
	key, _ := role["key"].(string)
	if key != string(perm.KeyFromName(name)) || role["builtin"] != false {
		t.Errorf("the new role is %v, want the key %s made from its name and not built in", role, perm.KeyFromName(name))
	}
	if status, _ := alice.api(http.MethodPost, "/roles", map[string]any{"name": name}); status != http.StatusUnprocessableEntity {
		t.Errorf("a second role of the same name = %d, want 422", status)
	}

	// A tick sends the whole list; what it names, in the matrix's order, is what the role grants.
	status, saved := alice.api(http.MethodPatch, "/roles/"+key, map[string]any{"permissions": []string{"workflow.run", "workflow.read"}})
	role, _ = saved["role"].(map[string]any)
	if got := role["permissions"]; status != http.StatusOK || !slices.Equal(stringsOf(got), []string{"workflow.read", "workflow.run"}) {
		t.Errorf("ticking run = %d, grants %v", status, got)
	}
	if status, _ := alice.api(http.MethodPatch, "/roles/"+key, map[string]any{"permissions": []string{"workflow.fly"}}); status != http.StatusUnprocessableEntity {
		t.Errorf("a permission nobody checks for = %d, want 422", status)
	}
	_, roles := alice.api(http.MethodGet, "/roles", nil)
	if !slices.Contains(listed(roles, "roles", "key"), key) || !slices.Contains(listed(roles, "roles", "name"), "Workflow administrator") {
		t.Errorf("the roles listed are %v", listed(roles, "roles", "name"))
	}
	_, words := alice.api(http.MethodGet, "/permissions", nil)
	if got := listed(words, "permissions", "permission"); len(got) != len(perm.AllPermissions) || got[0] != "workflow.read" {
		t.Errorf("the permissions are %v, want the catalogue in its order", got)
	}

	// The built-ins are edited but never deleted, and the Administrator keeps administering Rotor.
	if status, answer := alice.api(http.MethodDelete, "/roles/runner", nil); status != http.StatusConflict {
		t.Errorf("deleting Runner = %d %v, want 409", status, answer)
	}
	if status, _ := alice.api(http.MethodPatch, "/roles/administrator", map[string]any{"permissions": []string{"workflow.read"}}); status != http.StatusUnprocessableEntity {
		t.Errorf("taking administration from the Administrator = %d, want 422", status)
	}
	if status, _ := alice.api(http.MethodDelete, "/roles/"+key, nil); status != http.StatusNoContent {
		t.Errorf("deleting the role = %d, want 204", status)
	}
	if status, _ := alice.api(http.MethodDelete, "/roles/"+key, nil); status != http.StatusNotFound {
		t.Errorf("deleting it again = %d, want 404", status)
	}

	// Somebody who does not administer Rotor reads the roles and changes none, here and straight through SQL.
	member := h.addPerson(t, "member")
	plain := h.browserFor(t, member)
	if status, _ := plain.api(http.MethodGet, "/roles", nil); status != http.StatusOK {
		t.Errorf("a member reading the roles = %d, want 200", status)
	}
	if status, _ := plain.api(http.MethodPost, "/roles", map[string]any{"name": name}); status != http.StatusForbidden {
		t.Errorf("a member making a role = %d, want 403", status)
	}
	err := asApp(context.Background(), h.super, member, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO role (key, name) VALUES ('sneaky', 'Sneaky')`)
		return err
	})
	permissionDenied(t, "a member making a role as rotor_app", err)
	err = asApp(context.Background(), h.super, member, func(tx pgx.Tx) error {
		tag, err := tx.Exec(context.Background(), `UPDATE role SET permissions = '{rotor.administer}' WHERE key = 'runner'`)
		if err == nil && tag.RowsAffected() != 0 {
			t.Error("a member changed what Runner grants as rotor_app")
		}
		return err
	})
	if err != nil {
		t.Errorf("a member changing Runner as rotor_app = %v, want no row touched", err)
	}
}

func stringsOf(v any) []string {
	out := []string{}
	items, _ := v.([]any)
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// Groups made by hand take members here; Everyone and the realm's take them at sign-in alone.
func TestGroupsAreKeptByHandAndEveryoneAndTheRealmsAreNot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := newBrowser(t)
	alice.signIn("alice")
	person := h.addPerson(t, "member")
	name, claim := uniqueName("Reviewers"), uniqueName("realm-reviewers")
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM user_group WHERE name IN ($1, $2)`, name, claim) })

	status, made := alice.api(http.MethodPost, "/groups", map[string]any{"name": name, "description": "Read every change"})
	if status != http.StatusCreated {
		t.Fatalf("making a group = %d %v", status, made)
	}
	group := idOf(t, made, "group")
	status, changed := alice.api(http.MethodPost, "/groups/"+group+"/members", map[string]any{"userId": person.String()})
	if got, _ := changed["group"].(map[string]any); status != http.StatusOK || !slices.Equal(listed(got, "members", "userId"), []string{person.String()}) {
		t.Errorf("adding a member = %d %v", status, changed)
	}
	if status, _ := alice.api(http.MethodPost, "/groups/"+group+"/members", map[string]any{"userId": uuid.NewString()}); status != http.StatusNotFound {
		t.Errorf("adding somebody Rotor does not know = %d, want 404", status)
	}
	_, read := alice.api(http.MethodGet, "/groups/"+group, nil)
	if got, _ := read["group"].(map[string]any); got["memberCount"] != float64(1) {
		t.Errorf("the group reads %v", got)
	}
	status, changed = alice.api(http.MethodDelete, "/groups/"+group+"/members/"+person.String(), nil)
	if got, _ := changed["group"].(map[string]any); status != http.StatusOK || len(listed(got, "members", "userId")) != 0 {
		t.Errorf("taking the member out = %d %v", status, changed)
	}

	// A group matching a value of the realm's claim belongs to the realm.
	status, made = alice.api(http.MethodPost, "/groups", map[string]any{"name": claim, "externalRef": claim})
	realm := idOf(t, made, "group")
	if got, _ := made["group"].(map[string]any); status != http.StatusCreated || got["source"] != "oidc" {
		t.Errorf("a group matching a claim = %d %v, want source oidc", status, made)
	}
	if status, _ := alice.api(http.MethodPost, "/groups/"+realm+"/members", map[string]any{"userId": person.String()}); status != http.StatusConflict {
		t.Errorf("adding to a realm group by hand = %d, want 409", status)
	}
	var everyone string
	if err := h.super.QueryRow(ctx, `SELECT id::text FROM user_group WHERE builtin = 'everyone'`).Scan(&everyone); err != nil {
		t.Fatal(err)
	}
	if status, _ := alice.api(http.MethodPost, "/groups/"+everyone+"/members", map[string]any{"userId": person.String()}); status != http.StatusConflict {
		t.Errorf("adding to Everyone by hand = %d, want 409", status)
	}
	if status, _ := alice.api(http.MethodDelete, "/groups/"+everyone+"/members/"+person.String(), nil); status != http.StatusConflict {
		t.Errorf("taking somebody out of Everyone = %d, want 409", status)
	}
	if status, _ := alice.api(http.MethodDelete, "/groups/"+everyone, nil); status != http.StatusConflict {
		t.Errorf("deleting Everyone = %d, want 409", status)
	}

	// Everyone first, then the realm's, then those kept by hand.
	_, all := alice.api(http.MethodGet, "/groups", nil)
	sources, builtins := listed(all, "groups", "source"), listed(all, "groups", "builtin")
	byHand := slices.Index(sources[1:], "local")
	if len(builtins) == 0 || builtins[0] != perm.Everyone || byHand < 0 || slices.Contains(sources[1+byHand:], "oidc") {
		t.Errorf("the groups are listed as %v %v, want Everyone, the realm's, then by hand", builtins, sources)
	}

	// Somebody who grants nowhere looks no groups up; nobody writes a membership as rotor_app.
	plain := h.browserFor(t, person)
	if status, _ := plain.api(http.MethodGet, "/groups", nil); status != http.StatusForbidden {
		t.Errorf("a member listing the groups = %d, want 403", status)
	}
	if status, _ := plain.api(http.MethodPost, "/groups", map[string]any{"name": uniqueName("mine")}); status != http.StatusForbidden {
		t.Errorf("a member making a group = %d, want 403", status)
	}
	err := asApp(ctx, h.super, signedInID(t, alice), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, group, person)
		return err
	})
	permissionDenied(t, "an administrator writing a membership as rotor_app", err)
	if status, _ := alice.api(http.MethodDelete, "/groups/"+group, nil); status != http.StatusNoContent {
		t.Errorf("deleting the group = %d, want 204", status)
	}
}
