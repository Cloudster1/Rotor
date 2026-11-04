//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// ownedWorkflow makes a workflow behind the policies' back, with its first version, removed after the test.
func (h *harness) ownedWorkflow(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ($1, $2) RETURNING id`, uniqueName("granted"), owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, id) })
	if _, err := h.super.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash, created_by)
		VALUES ($1, 1, 'x', '{}', 'h', $2)`, id, owner); err != nil {
		t.Fatal(err)
	}
	return id
}

// localGroup makes a group by hand with the people given, removed after the test.
func (h *harness) localGroup(t *testing.T, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO user_group (name) VALUES ($1) RETURNING id`, uniqueName("team")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(ctx, `DELETE FROM user_group WHERE id = $1`, id) })
	for _, m := range members {
		if _, err := h.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, id, m); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// sees says whether person sees a workflow straight through SQL as rotor_app.
func (h *harness) sees(t *testing.T, person, workflow uuid.UUID) bool {
	t.Helper()
	var n int
	if err := h.cluster.Read(db.WithUser(context.Background(), person), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM workflow WHERE id = $1`, workflow).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestAGrantToAGroupReachesItsMembers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, member, outsider := h.addPerson(t, "member"), h.addPerson(t, "member"), h.addPerson(t, "member")
	workflow := h.ownedWorkflow(t, owner)
	group := h.localGroup(t, member)
	store := perm.NewStore(h.cluster)

	// The owner administers what they made, so the policies let them grant on it.
	if _, err := store.Grant(db.WithUser(ctx, owner), perm.GrantInput{Role: perm.Runner, Workflow: workflow, GroupID: &group}, owner); err != nil {
		t.Fatalf("the owner granting runner to a group: %v", err)
	}
	if !h.sees(t, member, workflow) || h.sees(t, outsider, workflow) {
		t.Errorf("after the grant the member sees it %v, the outsider %v; want true, false", h.sees(t, member, workflow), h.sees(t, outsider, workflow))
	}
	set, err := store.ResolveFor(ctx, member)
	if err != nil {
		t.Fatal(err)
	}
	if !set.Can(perm.WorkflowRun, workflow) || set.Can(perm.WorkflowEdit, workflow) || set.Can(perm.WorkflowRun, uuid.New()) {
		t.Errorf("the member resolves to %v on the workflow", set.WorkflowPermissions()[workflow])
	}
	if _, err := store.Grant(db.WithUser(ctx, member), perm.GrantInput{Role: perm.Editor, Workflow: workflow, UserID: &member}, member); !errors.Is(err, perm.ErrForbidden) {
		t.Errorf("a runner granting themselves editor = %v, want ErrForbidden", err)
	}
	for name, who := range map[string]uuid.UUID{"the runner": member, "an outsider": outsider} {
		err := h.cluster.Write(db.WithUser(ctx, who), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, workflow_id, user_id) VALUES ('editor', $1, $2)`, workflow, who)
			return err
		})
		permissionDenied(t, name+" granting straight through SQL as rotor_app", err)
	}

	if _, err := h.super.Exec(ctx, `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, group, member); err != nil {
		t.Fatal(err)
	}
	if h.sees(t, member, workflow) {
		t.Error("someone who left the group still sees what it was granted")
	}
}

func TestEveryoneReachesEveryoneWhoIsLetIn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, admin := h.addPerson(t, "member"), h.addPerson(t, "admin")
	workflow := h.ownedWorkflow(t, owner)
	// Sharing with everyone is a runner grant to Everyone, made as the owner straight through SQL as rotor_app.
	if err := h.cluster.Write(db.WithUser(ctx, owner), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE workflow SET sharing = 'everyone' WHERE id = $1`, workflow)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	newcomer := h.addPerson(t, "member")
	if !h.sees(t, newcomer, workflow) {
		t.Error("someone let in after the share does not see a workflow shared with everyone")
	}
	var everyone uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT id FROM user_group WHERE builtin = 'everyone'`).Scan(&everyone); err != nil {
		t.Fatal(err)
	}

	err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, everyone, newcomer)
		return err
	})
	refusedBy(t, "taking a person out of Everyone by hand", err, "23514", "")
	err = h.cluster.Write(db.WithUser(ctx, admin), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, everyone, newcomer)
		return err
	})
	permissionDenied(t, "an administrator taking a person out of Everyone as rotor_app", err)
	err = h.cluster.Write(db.WithUser(ctx, admin), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE user_group SET name = 'Some' WHERE id = $1`, everyone)
		return err
	})
	refusedBy(t, "an administrator renaming Everyone", err, "23514", "")

	// Everyone is never made an administrator, by a grant or by a role it holds coming to administer.
	store := perm.NewStore(h.cluster)
	if _, err := store.Grant(db.WithUser(ctx, admin), perm.GrantInput{Role: perm.Administrator, GroupID: &everyone}, admin); !errors.Is(err, perm.ErrEveryoneAdministers) {
		t.Errorf("granting Everyone administrator = %v, want ErrEveryoneAdministers", err)
	}
	err = h.cluster.Write(db.WithUser(ctx, admin), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, group_id) VALUES ('administrator', $1)`, everyone)
		return err
	})
	refusedBy(t, "granting Everyone administrator straight through SQL", err, "23514", perm.EveryoneAdministers)
	err = h.cluster.Write(db.WithUser(ctx, admin), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE role SET permissions = permissions || '{rotor.administer}' WHERE key = 'author'`)
		return err
	})
	refusedBy(t, "making the role Everyone holds administer Rotor", err, "23514", perm.EveryoneAdministers)

	// Switched off, a person leaves Everyone and holds nothing through it.
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, newcomer); err != nil {
		t.Fatal(err)
	}
	var still int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM group_member WHERE group_id = $1 AND user_id = $2`, everyone, newcomer).Scan(&still); err != nil || still != 0 {
		t.Errorf("a switched-off person is still in Everyone: %d, %v", still, err)
	}
}

func TestAdministrationIsNeverScopedToOneWorkflow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, person := h.addPerson(t, "admin"), h.addPerson(t, "member")
	workflow := h.ownedWorkflow(t, admin)
	store := perm.NewStore(h.cluster)
	for _, role := range []perm.Role{perm.Administrator, perm.Author} {
		if _, err := store.Grant(db.WithUser(ctx, admin), perm.GrantInput{Role: role, Workflow: workflow, UserID: &person}, admin); !errors.Is(err, perm.ErrScope) {
			t.Errorf("granting %s over one workflow = %v, want ErrScope", role, err)
		}
		err := h.cluster.Write(db.WithUser(ctx, admin), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, workflow_id, user_id) VALUES ($1, $2, $3)`, string(role), workflow, person)
			return err
		})
		refusedBy(t, "granting "+string(role)+" over one workflow straight through SQL", err, "23514", "role_assignment_guard")
	}
	// Administering one workflow is not a licence to administer Rotor.
	if _, err := store.Grant(db.WithUser(ctx, admin), perm.GrantInput{Role: perm.WorkflowAdministrator, Workflow: workflow, UserID: &person}, admin); err != nil {
		t.Fatal(err)
	}
	var administers bool
	if err := h.cluster.Read(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT current_actor_is_admin()`).Scan(&administers)
	}); err != nil || administers {
		t.Errorf("a workflow administrator administers Rotor: %v, %v", administers, err)
	}
}

func TestTheSameGrantTwiceIsOneGrant(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, person := h.addPerson(t, "member"), h.addPerson(t, "member")
	workflow := h.ownedWorkflow(t, owner)
	store := perm.NewStore(h.cluster)
	in := perm.GrantInput{Role: perm.Editor, Workflow: workflow, UserID: &person}
	first, err := store.Grant(db.WithUser(ctx, owner), in, owner)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Grant(db.WithUser(ctx, owner), in, owner)
	if err != nil || second != first {
		t.Errorf("the same grant again = %s, %v; want %s", second, err, first)
	}
	err = h.cluster.Write(db.WithUser(ctx, owner), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, workflow_id, user_id) VALUES ('editor', $1, $2)`, workflow, person)
		return err
	})
	refusedBy(t, "the same grant again straight through SQL", err, "23505", "")
	var n int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE role = 'editor' AND workflow_id = $1`, workflow).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d grants, %v; want one", n, err)
	}
	// An installation-wide grant is one grant too: its null workflow is not distinct from another.
	err = h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		for range 2 {
			if _, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('runner', $1)`, person); err != nil {
				return err
			}
		}
		return nil
	})
	refusedBy(t, "the same installation-wide grant twice", err, "23505", "")
}

func TestABuiltinRoleIsEditedButNeverDeleted(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin, member := h.addPerson(t, "admin"), h.addPerson(t, "member")
	store := perm.NewStore(h.cluster)
	if err := store.DeleteRole(db.WithUser(ctx, admin), perm.Runner, admin); !errors.Is(err, perm.ErrBuiltinRole) {
		t.Errorf("deleting the runner role = %v, want ErrBuiltinRole", err)
	}
	as := func(person uuid.UUID, statement string) error {
		return h.cluster.Write(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, statement)
			return err
		})
	}
	refusedBy(t, "deleting the runner role straight through SQL", as(admin, `DELETE FROM role WHERE key = 'runner'`), "23514", "role_guard")
	refusedBy(t, "renaming the runner role's key", as(admin, `UPDATE role SET key = 'starter' WHERE key = 'runner'`), "23514", "role_guard")
	refusedBy(t, "the administrator giving up administering Rotor",
		as(admin, `UPDATE role SET permissions = '{workflow.read}' WHERE key = 'administrator'`), "23514", "role_guard")
	refusedBy(t, "a role naming a permission nothing checks",
		as(admin, `INSERT INTO role (key, name, permissions) VALUES ('rogue', 'Rogue', '{run.everything}')`), "23514", "role_known_permissions")
	permissionDenied(t, "a member adding a role", as(member, `INSERT INTO role (key, name) VALUES ('mine', 'Mine')`))

	// An administrator's own role is theirs to delete; a built-in one is edited and put back.
	key := perm.Role("reviewer_" + uuid.NewString()[:8])
	if err := as(admin, `INSERT INTO role (key, name, permissions) VALUES ('`+string(key)+`', '`+string(key)+`', '{workflow.read}')`); err != nil {
		t.Fatalf("an administrator adding a role: %v", err)
	}
	if err := store.DeleteRole(db.WithUser(ctx, admin), key, admin); err != nil {
		t.Errorf("deleting an administrator's own role: %v", err)
	}
	if err := as(admin, `UPDATE role SET description = 'Starts runs.' WHERE key = 'runner'`); err != nil {
		t.Errorf("editing the runner role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(ctx, `UPDATE role SET description = $1 WHERE key = 'runner'`, perm.Builtins()[3].Description)
	})
}

// The last Administrator grant stays, refused by the service and straight through SQL; it needs an installation of
// its own, since the shared one always has others.
func TestTheLastAdministratorStays(t *testing.T) {
	h := newHarness(t)
	s := h.newScratch(t, 0)
	ctx := context.Background()
	var person, groupGrant, personGrant uuid.UUID
	if err := s.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('last@suite.test', 'Last') RETURNING id`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if err := s.super.QueryRow(ctx, `SELECT id FROM role_assignment WHERE role = 'administrator'`).Scan(&groupGrant); err != nil {
		t.Fatalf("a migrated installation holds one administrator grant: %v", err)
	}
	if err := s.super.QueryRow(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('administrator', $1) RETURNING id`, person).Scan(&personGrant); err != nil {
		t.Fatal(err)
	}
	store := perm.NewStore(s.cluster)
	acting := db.WithUser(ctx, person)
	if err := store.Revoke(acting, groupGrant, person); err != nil {
		t.Fatalf("revoking one of two administrator grants: %v", err)
	}
	if err := store.Revoke(acting, personGrant, person); !errors.Is(err, perm.ErrLastAdministrator) {
		t.Errorf("revoking the last administrator grant = %v, want ErrLastAdministrator", err)
	}
	err := s.cluster.Write(acting, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, personGrant)
		return err
	})
	refusedBy(t, "revoking the last administrator grant straight through SQL", err, "23514", perm.KeepsAnAdministrator)
	_, err = s.super.Exec(ctx, `UPDATE role_assignment SET role = 'runner' WHERE id = $1`, personGrant)
	refusedBy(t, "turning the last administrator grant into another", err, "23514", perm.KeepsAnAdministrator)
	_, err = s.super.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, person)
	refusedBy(t, "removing the last administrator's account", err, "23514", perm.KeepsAnAdministrator)
}
