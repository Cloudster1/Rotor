//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// userIn is the one person a listing of /users answered with.
func userIn(t *testing.T, answer map[string]any) map[string]any {
	t.Helper()
	users, _ := answer["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("want one person in %v", answer)
	}
	u, _ := users[0].(map[string]any)
	return u
}

// Admin's Users page lists every person with their groups and roles, and switches them off and on again.
func TestAnAdministratorSwitchesAPersonOffAndOn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := newBrowser(t)
	alice.signIn("alice")
	person, member := h.addPerson(t, "member"), h.addPerson(t, "member")
	group := h.localGroup(t, person)
	workflow := h.ownedWorkflow(t, member)
	if _, err := h.super.Exec(ctx, `INSERT INTO role_assignment (role, workflow_id, group_id) VALUES ('runner', $1, $2)`, workflow, group); err != nil {
		t.Fatal(err)
	}
	theirs := h.browserFor(t, person)

	_, listing := alice.api(http.MethodGet, "/users?q="+h.emailOf(t, person), nil)
	u := userIn(t, listing)
	if u["isActive"] != true || !slices.Contains(listed(u, "groups", "builtin"), perm.Everyone) || !slices.Contains(listed(u, "groups", "id"), group.String()) {
		t.Errorf("the person is listed as %v, want active, in Everyone and the group", u)
	}
	if roles := listed(u, "grants", "role"); !slices.Contains(roles, "author") || !slices.Contains(roles, "runner") ||
		!slices.Contains(listed(u, "grants", "workflowId"), workflow.String()) {
		t.Errorf("the person holds %v, want Author through Everyone and Runner on the workflow through the group", u["grants"])
	}

	status, switched := alice.api(http.MethodPatch, "/users/"+person.String(), map[string]any{"isActive": false})
	if got, _ := switched["user"].(map[string]any); status != http.StatusOK || got["isActive"] != false || slices.Contains(listed(got, "groups", "builtin"), perm.Everyone) {
		t.Errorf("switching off = %d %v, want switched off and out of Everyone", status, switched)
	}
	if status, _ := theirs.me(); status != http.StatusUnauthorized {
		t.Errorf("the switched-off person's session answers %d, want 401", status)
	}
	if h.sees(t, person, workflow) {
		t.Error("a switched-off person still sees what their group was granted")
	}
	status, switched = alice.api(http.MethodPatch, "/users/"+person.String(), map[string]any{"isActive": true})
	if got, _ := switched["user"].(map[string]any); status != http.StatusOK || got["isActive"] != true || !slices.Contains(listed(got, "groups", "builtin"), perm.Everyone) {
		t.Errorf("switching on = %d %v, want let in and back in Everyone", status, switched)
	}

	if status, answer := alice.api(http.MethodPatch, "/users/"+signedInID(t, alice).String(), map[string]any{"isActive": false}); status != http.StatusConflict {
		t.Errorf("switching oneself off = %d %v, want 409", status, answer)
	}
	if status, _ := alice.api(http.MethodPatch, "/users/"+uuid.NewString(), map[string]any{"isActive": false}); status != http.StatusNotFound {
		t.Errorf("switching off nobody = %d, want 404", status)
	}
	plain := h.browserFor(t, member)
	if status, _ := plain.api(http.MethodGet, "/users", nil); status != http.StatusForbidden {
		t.Errorf("a member listing people = %d, want 403", status)
	}
	if status, _ := plain.api(http.MethodPatch, "/users/"+person.String(), map[string]any{"isActive": false}); status != http.StatusForbidden {
		t.Errorf("a member switching somebody off = %d, want 403", status)
	}
	err := asApp(ctx, h.super, signedInID(t, alice), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, person)
		return err
	})
	permissionDenied(t, "an administrator switching somebody off as rotor_app", err)
}

// The last person who may administer Rotor stays switched on, refused by the service and by the database; it needs an
// installation of its own, since the shared one always has others.
func TestTheLastActiveAdministratorStaysOn(t *testing.T) {
	h := newHarness(t)
	s := h.newScratch(t, 0)
	ctx := context.Background()
	var first, second, team uuid.UUID
	if err := s.super.QueryRow(ctx, `INSERT INTO app_user (email, name, standing) VALUES ('first@suite.test', 'First', 'member') RETURNING id`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := s.super.QueryRow(ctx, `INSERT INTO app_user (email, name, standing) VALUES ('second@suite.test', 'Second', 'member') RETURNING id`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.super.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1)`, first); err != nil {
		t.Fatal(err)
	}
	sessions := auth.NewService(s.cluster, config.DefaultSessionTTL)
	if err := sessions.SwitchOff(ctx, uuid.Nil, first); !errors.Is(err, auth.ErrLastActiveAdministrator) {
		t.Errorf("switching off the only administrator = %v, want ErrLastActiveAdministrator", err)
	}
	err := s.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, first)
		return err
	})
	refusedBy(t, "switching off the only administrator straight through SQL", err, "23514", "app_user_keeps_an_administrator")

	// A second administrator, through a group, lets the first go; then the second is the one who stays.
	if err := s.super.QueryRow(ctx, `INSERT INTO user_group (name) VALUES ('Keepers') RETURNING id`).Scan(&team); err != nil {
		t.Fatal(err)
	}
	if _, err := s.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, team, second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.super.Exec(ctx, `INSERT INTO role_assignment (role, group_id) VALUES ('administrator', $1)`, team); err != nil {
		t.Fatal(err)
	}
	if err := sessions.SwitchOff(ctx, uuid.Nil, first); err != nil {
		t.Errorf("switching off one of two administrators: %v", err)
	}
	if err := sessions.SwitchOff(ctx, uuid.Nil, second); !errors.Is(err, auth.ErrLastActiveAdministrator) {
		t.Errorf("switching off the last active administrator = %v, want ErrLastActiveAdministrator", err)
	}
	if err := sessions.SwitchOn(ctx, uuid.Nil, first); err != nil {
		t.Errorf("switching the first back on: %v", err)
	}
	if err := sessions.SwitchOff(ctx, uuid.Nil, second); err != nil {
		t.Errorf("switching off the second once the first is back: %v", err)
	}
}
