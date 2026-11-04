//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// groupID is a group as /groups lists it to b: Everyone for an empty realm name, else the realm's group of that name.
func groupID(t *testing.T, b *browser, realm string) string {
	t.Helper()
	status, answer := b.api(http.MethodGet, "/groups", nil)
	if status != http.StatusOK {
		t.Fatalf("listing the groups = %d %v", status, answer)
	}
	list, _ := answer["groups"].([]any)
	for _, each := range list {
		g, _ := each.(map[string]any)
		if (realm == "" && g["builtin"] == "everyone") || (realm != "" && g["externalRef"] == realm) {
			id, _ := g["id"].(string)
			return id
		}
	}
	t.Fatalf("no group %q among %v", realm, list)
	return ""
}

// grantOn gives a group a role on a workflow through the workflow's grants, as b, and answers the grant's id.
func grantOn(t *testing.T, b *browser, workflowID, role, group string) string {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows/"+workflowID+"/grants", map[string]any{"role": role, "groupId": group})
	if status != http.StatusCreated {
		t.Fatalf("granting %s on the workflow = %d %v", role, status, answer)
	}
	return idOf(t, answer, "grant")
}

// userID is the id /auth/me gives the person signed in to b.
func userID(t *testing.T, b *browser) string {
	t.Helper()
	_, me := b.me()
	user, _ := me["user"].(map[string]any)
	id, _ := user["id"].(string)
	if id == "" {
		t.Fatalf("/auth/me names nobody: %v", me)
	}
	return id
}

// grantPerson gives a person a role on a workflow behind the policies' back.
func (h *harness) grantPerson(t *testing.T, role string, workflow, person uuid.UUID) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `INSERT INTO role_assignment (role, workflow_id, user_id) VALUES ($1, $2, $3)`,
		role, workflow, person); err != nil {
		t.Fatal(err)
	}
}

// Archiving is undone by restoring, which takes administering the workflow: straight through SQL as rotor_app, an
// editor and a runner are refused and its administrator is not.
func TestRestoringAWorkflowTakesAdministeringIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, editor, runner := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	workflow := h.ownedWorkflow(t, owner)
	h.grantPerson(t, "editor", workflow, editor)
	h.grantPerson(t, "runner", workflow, runner)
	if _, err := h.super.Exec(ctx, `UPDATE workflow SET archived_at = now() WHERE id = $1`, workflow); err != nil {
		t.Fatal(err)
	}
	restore := func(person uuid.UUID) (int64, error) {
		var touched int64
		err := h.cluster.Write(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `UPDATE workflow SET archived_at = NULL WHERE id = $1`, workflow)
			touched = tag.RowsAffected()
			return err
		})
		return touched, err
	}

	// An editor may update the row, for saving versions, so the guard is what refuses them.
	_, err := restore(editor)
	refusedBy(t, "an editor restoring as rotor_app", err, "42501", "workflow_change_guard")
	// A runner may not update the row at all, so the policy hides it from the update.
	if touched, err := restore(runner); err != nil || touched != 0 {
		t.Errorf("a runner restoring as rotor_app touched %d rows (%v), want none", touched, err)
	}
	if archived := h.archived(t, workflow); !archived {
		t.Fatal("the workflow left the archive for somebody who does not administer it")
	}

	if touched, err := restore(owner); err != nil || touched != 1 {
		t.Fatalf("its administrator restoring as rotor_app touched %d rows (%v), want 1", touched, err)
	}
	if h.archived(t, workflow) {
		t.Error("the workflow is still archived after its administrator restored it")
	}
}

func (h *harness) archived(t *testing.T, workflow uuid.UUID) bool {
	t.Helper()
	var archived bool
	if err := h.super.QueryRow(context.Background(), `SELECT archived_at IS NOT NULL FROM workflow WHERE id = $1`, workflow).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	return archived
}

// Through the api: a runner and an editor are refused restoring with 403; its administrator restores, audited.
func TestArchivingIsUndoneThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	bob, carol := newBrowser(t), newBrowser(t)
	bob.signIn("bob")
	carol.signIn("carol")
	id := createWorkflow(t, bob, approvalOnly(uniqueName("undo"), "Go on?"))
	if status, answer := bob.api(http.MethodDelete, "/workflows/"+id, nil); status != http.StatusNoContent {
		t.Fatalf("archiving = %d %v", status, answer)
	}
	for _, role := range []string{"runner", "editor"} {
		status, answer := bob.api(http.MethodPost, "/workflows/"+id+"/grants", map[string]any{"role": role, "userId": userID(t, carol)})
		if status != http.StatusCreated {
			t.Fatalf("granting carol %s = %d %v", role, status, answer)
		}
		if status, _ := carol.api(http.MethodPost, "/workflows/"+id+"/restore", nil); status != http.StatusForbidden {
			t.Errorf("a %s restoring = %d, want 403", role, status)
		}
	}
	status, answer := bob.api(http.MethodPost, "/workflows/"+id+"/restore", nil)
	if wf, _ := answer["workflow"].(map[string]any); status != http.StatusOK || wf["archived"] != false {
		t.Fatalf("its administrator restoring = %d %v", status, answer)
	}
	if !h.audited(t, audit.ActionWorkflowRestored, id) {
		t.Error("restoring the workflow wrote no audit entry")
	}
	// Carol holds two roles, which is one person besides the owner.
	_, answer = bob.api(http.MethodGet, "/workflows/"+id, nil)
	if wf, _ := answer["workflow"].(map[string]any); wf["sharedWith"] != float64(1) || wf["sharing"] != nil {
		t.Errorf("the workflow says %v, want it shared with one and no sharing field", wf)
	}
}

// The role word is gone from the table; administrators are still administrators, by their grants alone.
func TestTheRoleColumnIsGoneAndAdministratorsStayAdministrators(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var columns int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND ((table_name = 'app_user' AND column_name = 'role')
			OR (table_name = 'workflow' AND column_name IN ('sharing', 'shared_groups')))`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Errorf("%d of app_user.role, workflow.sharing and workflow.shared_groups are left", columns)
	}
	admin, member := h.addPerson(t, "admin"), h.addPerson(t, "member")
	for person, want := range map[uuid.UUID]bool{admin: true, member: false} {
		var is bool
		if err := asApp(ctx, h.super, person, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT current_actor_is_admin()`).Scan(&is)
		}); err != nil {
			t.Fatal(err)
		}
		if is != want {
			t.Errorf("current_actor_is_admin() for an %v administrator = %v", want, is)
		}
	}
	alice := newBrowser(t)
	alice.signIn("alice")
	if _, me := alice.me(); me["role"] != "admin" {
		t.Errorf("alice in rotor-administrators is %v to /auth/me", me["role"])
	}
}
