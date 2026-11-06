//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// emailOf is the address addPerson gave a person, which is what a search finds them by.
func (h *harness) emailOf(t *testing.T, person any) string {
	t.Helper()
	var email string
	if err := h.super.QueryRow(context.Background(), `SELECT email::text FROM app_user WHERE id = $1`, person).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// Admin's Access page grants over the whole installation and over one workflow, lists, filters and revokes.
func TestAnAdministratorGrantsListsAndRevokes(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	person, owner := h.addPerson(t, "member"), h.addPerson(t, "member")
	group := h.localGroup(t, person)
	workflow := h.ownedWorkflow(t, owner)

	status, granted := alice.api(http.MethodPost, "/role-assignments", map[string]any{"role": "editor", "userId": person.String()})
	editor := idOf(t, granted, "assignment")
	if got, _ := granted["assignment"].(map[string]any); status != http.StatusCreated || got["roleName"] != "Editor" || got["workflowId"] != nil {
		t.Errorf("granting Editor over the installation = %d %v", status, granted)
	}
	if _, again := alice.api(http.MethodPost, "/role-assignments", map[string]any{"role": "editor", "userId": person.String()}); idOf(t, again, "assignment") != editor {
		t.Errorf("granting the same twice made %v, want the one grant %s", again, editor)
	}
	status, granted = alice.api(http.MethodPost, "/role-assignments",
		map[string]any{"role": "runner", "workflowId": workflow.String(), "groupId": group.String()})
	if got, _ := granted["assignment"].(map[string]any); status != http.StatusCreated || got["workflowId"] != workflow.String() || got["groupName"] == "" {
		t.Errorf("granting Runner on a workflow to a group = %d %v", status, granted)
	}
	if status, _ := alice.api(http.MethodPost, "/role-assignments",
		map[string]any{"role": "administrator", "workflowId": workflow.String(), "userId": person.String()}); status != http.StatusUnprocessableEntity {
		t.Errorf("granting Administrator over one workflow = %d, want 422", status)
	}
	if status, _ := alice.api(http.MethodPost, "/role-assignments",
		map[string]any{"role": "runner", "userId": person.String(), "groupId": group.String()}); status != http.StatusUnprocessableEntity {
		t.Errorf("granting to a person and a group at once = %d, want 422", status)
	}

	// A search finds the person's grants; a workflow keeps what answers there, the installation-wide ones first.
	_, found := alice.api(http.MethodGet, "/role-assignments?q="+h.emailOf(t, person), nil)
	if got := listed(found, "assignments", "id"); !slices.Equal(got, []string{editor}) {
		t.Errorf("searching for the person found %v, want %s", got, editor)
	}
	_, there := alice.api(http.MethodGet, "/role-assignments?workflow="+workflow.String()+"&limit=200", nil)
	items, _ := there["assignments"].([]any)
	first, _ := items[0].(map[string]any)
	if first["workflowId"] != nil {
		t.Errorf("the grants answering on the workflow begin with %v, want the installation-wide ones first", first)
	}
	_, runners := alice.api(http.MethodGet, "/role-assignments?role=runner&workflow="+workflow.String(), nil)
	if got := listed(runners, "assignments", "groupId"); !slices.Contains(got, group.String()) {
		t.Errorf("Runner on the workflow lists groups %v, want %s", got, group)
	}
	_, one := alice.api(http.MethodGet, "/role-assignments?limit=1", nil)
	if total, _ := one["total"].(float64); len(listed(one, "assignments", "id")) != 1 || total < 2 {
		t.Errorf("a page of one = %v", one)
	}

	// The grant to the group reaches its member.
	if run, edit, _ := flags(t, h.browserFor(t, person), workflow.String()); !run || !edit {
		t.Errorf("the member may run %v and edit %v, want both, from Editor and the group's Runner", run, edit)
	}
	if status, _ := alice.api(http.MethodDelete, "/role-assignments/"+editor, nil); status != http.StatusNoContent {
		t.Errorf("revoking = %d, want 204", status)
	}
	if status, _ := alice.api(http.MethodDelete, "/role-assignments/"+editor, nil); status != http.StatusNotFound {
		t.Errorf("revoking again = %d, want 404", status)
	}
	if status, _ := h.browserFor(t, person).api(http.MethodGet, "/role-assignments", nil); status != http.StatusForbidden {
		t.Errorf("a member listing every grant = %d, want 403", status)
	}
}

// A workflow's grants replace its sharing: its administrator grants and revokes there, and nowhere else.
func TestAWorkflowAdministratorGrantsOnTheirOwnWorkflowAlone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, other, grantee := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	mine, theirs := h.ownedWorkflow(t, owner), h.ownedWorkflow(t, other)
	ob, gb, tb := h.browserFor(t, owner), h.browserFor(t, grantee), h.browserFor(t, other)

	_, people := ob.api(http.MethodGet, "/people?q="+h.emailOf(t, grantee), nil)
	if got := listed(people, "people", "id"); !slices.Equal(got, []string{grantee.String()}) {
		t.Errorf("the owner looking the grantee up found %v", got)
	}
	status, granted := ob.api(http.MethodPost, "/workflows/"+mine.String()+"/grants", map[string]any{"role": "runner", "userId": grantee.String()})
	grant := idOf(t, granted, "grant")
	if got, _ := granted["grant"].(map[string]any); status != http.StatusCreated || got["workflowId"] != mine.String() || got["userEmail"] == "" {
		t.Fatalf("the owner granting Runner = %d %v", status, granted)
	}
	if status, _ := ob.api(http.MethodPost, "/workflows/"+mine.String()+"/grants", map[string]any{"role": "administrator", "userId": grantee.String()}); status != http.StatusUnprocessableEntity {
		t.Errorf("granting Administrator on a workflow = %d, want 422", status)
	}

	// The grantee runs it and reads who holds what, and changes none of it.
	if run, edit, administer := flags(t, gb, mine.String()); !run || edit || administer {
		t.Errorf("the runner may run %v, edit %v, administer %v; want only run", run, edit, administer)
	}
	_, listing := gb.api(http.MethodGet, "/workflows/"+mine.String()+"/grants", nil)
	if got := listed(listing, "grants", "role"); !slices.Contains(got, "runner") || !slices.Contains(got, "workflow_administrator") {
		t.Errorf("the runner reads the grants %v, want the owner's and theirs", got)
	}
	if status, _ := gb.api(http.MethodPost, "/workflows/"+mine.String()+"/grants", map[string]any{"role": "editor", "userId": grantee.String()}); status != http.StatusForbidden {
		t.Errorf("a runner granting = %d, want 403", status)
	}
	if status, _ := gb.api(http.MethodDelete, "/workflows/"+mine.String()+"/grants/"+grant, nil); status != http.StatusForbidden {
		t.Errorf("a runner revoking = %d, want 403", status)
	}

	// Another's workflow: unseen it is not there; seen as a runner it is not the owner's to grant on.
	if status, _ := ob.api(http.MethodGet, "/workflows/"+theirs.String()+"/grants", nil); status != http.StatusNotFound {
		t.Errorf("reading the grants of a workflow one cannot see = %d, want 404", status)
	}
	if status, _ := ob.api(http.MethodPost, "/workflows/"+theirs.String()+"/grants", map[string]any{"role": "runner", "userId": grantee.String()}); status != http.StatusNotFound {
		t.Errorf("granting on a workflow one cannot see = %d, want 404", status)
	}
	if status, answer := tb.api(http.MethodPost, "/workflows/"+theirs.String()+"/grants", map[string]any{"role": "runner", "userId": owner.String()}); status != http.StatusCreated {
		t.Fatalf("the other owner sharing with the first = %d %v", status, answer)
	}
	if status, _ := ob.api(http.MethodPost, "/workflows/"+theirs.String()+"/grants", map[string]any{"role": "editor", "userId": grantee.String()}); status != http.StatusForbidden {
		t.Errorf("a runner of another's workflow granting on it = %d, want 403", status)
	}
	if status, _ := ob.api(http.MethodDelete, "/workflows/"+theirs.String()+"/grants/"+grant, nil); status != http.StatusForbidden {
		t.Errorf("revoking through a workflow one does not administer = %d, want 403", status)
	}
	err := asApp(ctx, h.super, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, workflow_id, user_id) VALUES ('editor', $1, $2)`, theirs, grantee)
		return err
	})
	permissionDenied(t, "a runner granting on another's workflow as rotor_app", err)
	if status, _ := ob.api(http.MethodPost, "/role-assignments", map[string]any{"role": "editor", "userId": grantee.String()}); status != http.StatusForbidden {
		t.Errorf("a workflow administrator granting over the installation = %d, want 403", status)
	}

	if status, _ := ob.api(http.MethodDelete, "/workflows/"+mine.String()+"/grants/"+grant, nil); status != http.StatusNoContent {
		t.Errorf("the owner revoking = %d, want 204", status)
	}
	if h.sees(t, grantee, mine) {
		t.Error("the grantee still sees the workflow once the grant is gone")
	}
}

// Who may be granted a role is looked up by those who grant, and nobody else; rotor_app reads one's own row alone.
func TestPeopleAreLookedUpOnlyByThoseWhoGrant(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice := newBrowser(t)
	alice.signIn("alice")
	member, off := h.addPerson(t, "member"), h.addPerson(t, "member")
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, off); err != nil {
		t.Fatal(err)
	}
	if status, answer := h.browserFor(t, member).api(http.MethodGet, "/people?q=a", nil); status != http.StatusForbidden {
		t.Errorf("a member looking people up = %d %v, want 403", status, answer)
	}
	var seen int
	if err := asApp(ctx, h.super, member, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM app_user`).Scan(&seen)
	}); err != nil || seen != 1 {
		t.Errorf("a member reads %d people as rotor_app (%v), want themselves alone", seen, err)
	}
	_, found := alice.api(http.MethodGet, "/people?q="+h.emailOf(t, member), nil)
	if got := listed(found, "people", "id"); !slices.Equal(got, []string{member.String()}) {
		t.Errorf("an administrator looking the member up found %v", got)
	}
	if _, found := alice.api(http.MethodGet, "/people?q="+h.emailOf(t, off), nil); len(listed(found, "people", "id")) != 0 {
		t.Errorf("a switched-off person is offered: %v", found)
	}
}
