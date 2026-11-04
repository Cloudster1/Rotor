//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
)

// Signing in is not being let in (#204): a stranger at the provider waits on Admin's Users page.
func TestAStrangerWaitsAndIsLetInFromTheUsersPage(t *testing.T) {
	h := newHarness(t)
	s := newStranger(t, h, "stranger")
	b := newBrowser(t)
	if landed := b.signIn(s.username); landed != waitingLanding {
		t.Fatalf("a stranger landed on %q, want %q", landed, waitingLanding)
	}
	if status, _ := b.me(); status != http.StatusUnauthorized {
		t.Errorf("a stranger is signed in with %d", status)
	}
	// Asking twice is one request.
	newBrowser(t).signIn(s.username)
	id, letIn, waiting := h.personByEmail(t, s.email)
	if letIn || !waiting || h.inEveryone(t, id) {
		t.Fatalf("the stranger is let in %v, waiting %v, in Everyone %v", letIn, waiting, h.inEveryone(t, id))
	}

	admin := h.browserFor(t, h.addPerson(t, auth.RoleAdmin))
	held := 0
	for _, email := range requested(t, admin) {
		if email == s.email {
			held++
		}
	}
	if held != 1 {
		t.Errorf("the requests hold %s %d times, want once", s.email, held)
	}
	if _, users := admin.api(http.MethodGet, "/users?q="+s.username, nil); users["total"] != float64(0) {
		t.Errorf("somebody waiting is listed as a user: %v", users)
	}
	member := h.browserFor(t, h.addPerson(t, auth.RoleMember))
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/users/requests"}, {http.MethodPost, "/users/requests/" + id.String() + "/admit"},
		{http.MethodDelete, "/users/requests/" + id.String()},
	} {
		if status, _ := member.api(call.method, call.path, nil); status != http.StatusForbidden {
			t.Errorf("a member's %s %s = %d, want 403", call.method, call.path, status)
		}
	}

	status, answer := admin.api(http.MethodPost, "/users/requests/"+id.String()+"/admit", map[string]any{"role": "member"})
	if user, _ := answer["user"].(map[string]any); status != http.StatusOK || user["email"] != s.email {
		t.Fatalf("letting the stranger in = %d %v", status, answer)
	}
	if status, _ := admin.api(http.MethodPost, "/users/requests/"+id.String()+"/admit", nil); status != http.StatusNotFound {
		t.Errorf("letting them in twice = %d, want 404", status)
	}
	if _, letIn, waiting = h.personByEmail(t, s.email); !letIn || waiting || !h.inEveryone(t, id) {
		t.Errorf("after letting in: let in %v, waiting %v", letIn, waiting)
	}
	b = newBrowser(t)
	if landed := b.signIn(s.username); landed != "/runs" {
		t.Fatalf("the person let in landed on %q", landed)
	}
	if _, me := b.me(); me["role"] != auth.RoleMember {
		t.Errorf("the person let in is %v", me)
	}
	var admitted int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2
		AND actor_user_id IS NOT NULL AND data->>'via' = 'users_page'`, audit.ActionMemberAdmitted, id.String()).Scan(&admitted); err != nil || admitted != 1 {
		t.Errorf("%d %s entries (%v)", admitted, audit.ActionMemberAdmitted, err)
	}
}

func TestAStrangerTurnedAwayMayAskAgain(t *testing.T) {
	h := newHarness(t)
	s := newStranger(t, h, "turned")
	if landed := newBrowser(t).signIn(s.username); landed != waitingLanding {
		t.Fatalf("a stranger landed on %q", landed)
	}
	id, _, _ := h.personByEmail(t, s.email)
	admin := h.browserFor(t, h.addPerson(t, auth.RoleAdmin))
	if status, answer := admin.api(http.MethodPost, "/users/requests/"+id.String()+"/admit", map[string]any{"role": "owner"}); status != http.StatusUnprocessableEntity {
		t.Errorf("letting in as owner = %d %v, want 422", status, answer)
	}
	if status, _ := admin.api(http.MethodDelete, "/users/requests/"+id.String(), nil); status != http.StatusNoContent {
		t.Fatalf("turning away = %d", status)
	}
	if status, answer := admin.api(http.MethodDelete, "/users/requests/"+id.String(), nil); status != http.StatusNotFound {
		t.Errorf("turning away twice = %d %v, want 404", status, answer)
	}
	if slices.Contains(requested(t, admin), s.email) {
		t.Error("somebody turned away is still waiting")
	}
	if _, letIn, _ := h.personByEmail(t, s.email); letIn {
		t.Error("somebody turned away is let in")
	}
	// They ask again by signing in again, and wait as before.
	if landed := newBrowser(t).signIn(s.username); landed != waitingLanding {
		t.Errorf("signing in again landed on %q", landed)
	}
	if !slices.Contains(requested(t, admin), s.email) {
		t.Error("asking again is not a request")
	}
	var declined int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`,
		audit.ActionMemberDeclined, id.String()).Scan(&declined); err != nil || declined != 1 {
		t.Errorf("%d %s entries (%v)", declined, audit.ActionMemberDeclined, err)
	}
}

// With Let people in by their groups on, a realm group holding a grant lets its members in with its roles; off, or a
// group holding nothing, is no way in.
func TestAGroupWithAGrantLetsSomebodyInWhenTheSettingIsOn(t *testing.T) {
	h := newHarness(t)
	h.providerSnapshot(t)
	admin := h.browserFor(t, h.addPerson(t, auth.RoleAdmin))
	granted, bare := newRealmGroup(t, h, "letin"), newRealmGroup(t, h, "bare")
	status, answer := admin.api(http.MethodPost, "/groups", map[string]any{"name": granted, "externalRef": granted})
	if status != http.StatusCreated {
		t.Fatalf("the realm group in Rotor = %d %v", status, answer)
	}
	if status, answer := admin.api(http.MethodPost, "/role-assignments", map[string]any{"role": "editor", "groupId": idOf(t, answer, "group")}); status != http.StatusCreated {
		t.Fatalf("granting the group Editor = %d %v", status, answer)
	}

	off := newStranger(t, h, "grouped", granted)
	if landed := newBrowser(t).signIn(off.username); landed != waitingLanding {
		t.Errorf("with the setting off, a granted group let somebody in to %q", landed)
	}

	if status, answer := admin.api(http.MethodPut, "/oidc-provider", providerBody(t, admin, map[string]any{"admitByGroup": true})); status != http.StatusOK {
		t.Fatalf("turning Let people in by their groups on = %d %v", status, answer)
	}
	on := newStranger(t, h, "grouped", granted)
	b := newBrowser(t)
	if landed := b.signIn(on.username); landed != "/runs" {
		t.Fatalf("with the setting on, the granted group's member landed on %q", landed)
	}
	if _, access := b.api(http.MethodGet, "/access/me", nil); !slices.Contains(listed(access, "grants", "role"), "editor") {
		t.Errorf("the group's role did not come with it: %v", access)
	}
	id, _, waiting := h.personByEmail(t, on.email)
	if waiting || !h.inEveryone(t, id) {
		t.Errorf("let in by the group but waiting %v or outside Everyone", waiting)
	}
	var via string
	if err := h.super.QueryRow(context.Background(), `SELECT data->>'group' FROM audit_log WHERE action = $1 AND target_id = $2
		AND data->>'via' = 'sign_in'`, audit.ActionMemberAdmitted, id.String()).Scan(&via); err != nil || via != granted {
		t.Errorf("the audit names group %q (%v), want %q", via, err, granted)
	}
	// Somebody already waiting is let in by their next sign-in, and stops waiting.
	if landed := newBrowser(t).signIn(off.username); landed != "/runs" {
		t.Errorf("the one who waited landed on %q", landed)
	}
	if _, _, waiting := h.personByEmail(t, off.email); waiting {
		t.Error("let in by the group but still waiting")
	}
	holdsNothing := newStranger(t, h, "bare", bare)
	if landed := newBrowser(t).signIn(holdsNothing.username); landed != waitingLanding {
		t.Errorf("a group holding no grant let somebody in to %q", landed)
	}
}

// Whoever is in the realm group holding Administrator is let in whatever the setting, so there is always somebody to
// let the rest in, and the realm's service accounts keep working.
func TestTheAdministratorsGroupLetsItsMembersInWithTheSettingOff(t *testing.T) {
	h := newHarness(t)
	s := newStranger(t, h, "admin", "rotor-administrators")
	b := newBrowser(t)
	if landed := b.signIn(s.username); landed != "/runs" {
		t.Fatalf("a member of rotor-administrators landed on %q", landed)
	}
	if _, me := b.me(); me["role"] != auth.RoleAdmin {
		t.Errorf("a member of rotor-administrators is %v", me["role"])
	}
}

// Everybody who had an account before the change is let in, in Everyone, and waits for nothing.
func TestExistingPeopleAreLetInByTheMigration(t *testing.T) {
	h := newHarness(t)
	s := h.newScratch(t, 1320)
	ctx := context.Background()
	if _, err := s.super.Exec(ctx, `INSERT INTO app_user (email, name) VALUES ('on@suite.test', 'On')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.super.Exec(ctx, `INSERT INTO app_user (email, name, is_active) VALUES ('off@suite.test', 'Off', false)`); err != nil {
		t.Fatal(err)
	}
	s.up(0)
	rows, err := s.super.Query(ctx, `
		SELECT u.email::text, u.standing, EXISTS (SELECT 1 FROM group_member m JOIN user_group g ON g.id = m.group_id
		                                         WHERE g.builtin = 'everyone' AND m.user_id = u.id)
		FROM app_user u ORDER BY u.email`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			email, standing string
			everyone        bool
		)
		if err := rows.Scan(&email, &standing, &everyone); err != nil {
			t.Fatal(err)
		}
		if standing != "member" || everyone != (email == "on@suite.test") {
			t.Errorf("%s after the migration: standing %q, in Everyone %v", email, standing, everyone)
		}
	}
	var waiting int
	if err := s.super.QueryRow(ctx, `SELECT count(*) FROM join_request`).Scan(&waiting); err != nil || waiting != 0 {
		t.Errorf("%d waiting after the migration (%v)", waiting, err)
	}
	// A person made after it starts outside.
	var standing *string
	if err := s.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('new@suite.test', 'New') RETURNING standing`).Scan(&standing); err != nil || standing != nil {
		t.Errorf("a new person's standing = %v (%v), want none", standing, err)
	}
}
