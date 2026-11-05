package perm

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/migrations"
)

var (
	here      = uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	elsewhere = uuid.MustParse("019a0000-0000-7000-8000-000000000002")
)

func installationWide(role Role) Grant { return Grant{Role: role} }

func onWorkflow(role Role, id uuid.UUID) Grant { return Grant{Role: role, Workflow: id} }

func TestEveryWorkflowRoleCanRead(t *testing.T) {
	for _, role := range []Role{Administrator, WorkflowAdministrator, Editor, Runner} {
		if !role.Allows(WorkflowRead) {
			t.Errorf("%s cannot see the workflow it is granted on", role)
		}
	}
}

// A runner is the whole point of having a role that changes nothing.
func TestARunnerChangesNothing(t *testing.T) {
	for _, p := range []Permission{WorkflowEdit, WorkflowAdminister, WorkflowCreate, RotorAdminister} {
		if Runner.Allows(p) {
			t.Errorf("a runner is allowed %s", p)
		}
	}
}

func TestAnEditorSavesWithoutAdministering(t *testing.T) {
	if !Editor.Allows(WorkflowEdit) || !Editor.Allows(WorkflowRun) {
		t.Error("an editor cannot save or run")
	}
	for _, p := range []Permission{WorkflowAdminister, RotorAdminister} {
		if Editor.Allows(p) {
			t.Errorf("an editor is allowed %s", p)
		}
	}
}

func TestAWorkflowAdministratorStopsAtTheWorkflowBoundary(t *testing.T) {
	if !WorkflowAdministrator.Allows(WorkflowAdminister) {
		t.Error("a workflow administrator cannot administer a workflow")
	}
	for _, p := range []Permission{RotorAdminister, WorkflowCreate} {
		if WorkflowAdministrator.Allows(p) {
			t.Errorf("a workflow administrator is allowed %s, which is an installation-wide act", p)
		}
	}
}

func TestOnlyTheAdministratorAdministersRotor(t *testing.T) {
	for _, role := range Roles {
		if role.Allows(RotorAdminister) != (role == Administrator) {
			t.Errorf("%s and administering Rotor disagree", role)
		}
	}
	for _, p := range AllPermissions {
		if !Administrator.Allows(p) {
			t.Errorf("the administrator lacks %s", p)
		}
	}
}

func TestAdministrationIsNotAWorkflowRole(t *testing.T) {
	for _, role := range []Role{Administrator, Author} {
		if !role.InstallationWideOnly() {
			t.Errorf("%s can be scoped to one workflow", role)
		}
	}
	for _, role := range []Role{WorkflowAdministrator, Editor, Runner} {
		if role.InstallationWideOnly() {
			t.Errorf("%s cannot be granted over one workflow", role)
		}
	}
}

// The rule that matters most: a role given over one workflow must not answer for another one.
func TestAWorkflowGrantDoesNotLeak(t *testing.T) {
	set := NewSet([]Grant{onWorkflow(Editor, here)})
	if !set.Can(WorkflowEdit, here) {
		t.Error("the grant does not apply where it was given")
	}
	if set.Can(WorkflowEdit, elsewhere) || set.Can(WorkflowRead, elsewhere) {
		t.Error("a grant over one workflow answered for another")
	}
}

func TestAnInstallationWideGrantAppliesEverywhere(t *testing.T) {
	set := NewSet([]Grant{installationWide(Runner)})
	if !set.Can(WorkflowRun, here) || !set.Can(WorkflowRun, elsewhere) || !set.Can(WorkflowRun, uuid.New()) {
		t.Error("an installation-wide grant did not reach every workflow, including one made later")
	}
}

func TestGrantsAddUpRatherThanOverride(t *testing.T) {
	set := NewSet([]Grant{installationWide(Runner), onWorkflow(Editor, here)})
	if !set.Can(WorkflowRead, elsewhere) {
		t.Error("the installation-wide runner was lost")
	}
	if !set.Can(WorkflowEdit, here) || set.Can(WorkflowEdit, elsewhere) {
		t.Error("the workflow grant was lost, or applied where it was not given")
	}
}

func TestInstallationQuestionsIgnoreWorkflowGrants(t *testing.T) {
	set := NewSet([]Grant{onWorkflow(WorkflowAdministrator, here)})
	if set.CanInstallation(WorkflowAdminister) || set.CanInstallation(RotorAdminister) {
		t.Error("a workflow grant answered an installation-wide question")
	}
}

func TestCanSomewhereIsAboutOfferingNotAllowing(t *testing.T) {
	set := NewSet([]Grant{onWorkflow(Editor, here)})
	if !set.CanSomewhere(WorkflowEdit) {
		t.Error("somebody who edits somewhere is not offered it anywhere")
	}
	if set.CanSomewhere(RotorAdminister) {
		t.Error("a permission nobody holds is offered")
	}
}

func TestSomebodyWithNoRolesHoldsNothing(t *testing.T) {
	set := NewSet(nil)
	if !set.Empty() || set.Can(WorkflowRead, here) || set.CanInstallation(WorkflowCreate) {
		t.Error("somebody with no roles holds something")
	}
	var zero Set
	if zero.Can(WorkflowRead, here) || zero.CanInstallation(RotorAdminister) {
		t.Error("a set nobody resolved holds something")
	}
}

// A role nobody recognises grants nothing, rather than everything.
func TestAnUnknownRoleGrantsNothing(t *testing.T) {
	if set := NewSet([]Grant{{Role: "superuser"}}); !set.Empty() {
		t.Errorf("an unknown role granted %v", set.Grants())
	}
}

func TestSetRemembersWhereItCameFrom(t *testing.T) {
	got := NewSet([]Grant{installationWide(Author), onWorkflow(Runner, here)}).Grants()
	if len(got) != 2 || got[0].Role != Author || got[1].Role != Runner || got[1].Workflow != here || !got[0].Installation() {
		t.Errorf("grants = %+v, want the two it was built from with their scopes", got)
	}
}

func TestWorkflowsNamesOnlyWhatWasGrantedExplicitly(t *testing.T) {
	set := NewSet([]Grant{installationWide(Runner), onWorkflow(Editor, here)})
	if got := set.Workflows(); len(got) != 1 || got[0] != here {
		t.Errorf("workflows = %v, want just the one named explicitly", got)
	}
	if got := set.WorkflowPermissions()[here]; !slices.Equal(got, []Permission{WorkflowRead, WorkflowRun, WorkflowEdit}) {
		t.Errorf("permissions on the workflow = %v", got)
	}
	if got := set.InstallationPermissions(); !slices.Equal(got, []Permission{WorkflowRead, WorkflowRun}) {
		t.Errorf("installation-wide permissions = %v", got)
	}
}

// The installation's own roles decide: a role the built-in table never had grants what its row says, and an edited
// built-in grants what it says now.
func TestTheInstallationsOwnRolesDecide(t *testing.T) {
	defs := Definitions{"reviewer": {WorkflowRead}, Runner: {WorkflowRead}}
	s := NewSetWith([]Grant{onWorkflow("reviewer", here), installationWide(Runner)}, defs)
	if !s.Can(WorkflowRead, here) || s.Can(WorkflowRun, here) {
		t.Error("an edited runner still grants what the built-in table said")
	}
	if NewSetWith([]Grant{installationWide(Administrator)}, Definitions{}).CanInstallation(RotorAdminister) {
		t.Error("a role the installation does not define grants something")
	}
}

func TestEveryPermissionHasWordsForTheMatrix(t *testing.T) {
	for _, p := range AllPermissions {
		if strings.TrimSpace(PermissionWords[p]) == "" {
			t.Errorf("%s has no words", p)
		}
	}
	if len(PermissionWords) != len(AllPermissions) {
		t.Errorf("%d words for %d permissions", len(PermissionWords), len(AllPermissions))
	}
}

// The database's CHECK constraint and seeded roles are the catalogue here: a permission the code checks for that the
// table refuses, or a built-in seeded otherwise, would make the two disagree. Later migrations restate the constraint,
// seed more roles, add permissions to seeded ones and reword them, so the catalogue is every migration's Up, in order.
func TestTheDatabaseHoldsTheSameCatalogue(t *testing.T) {
	source := migrationsUp(t)
	all := regexp.MustCompile(`role_known_permissions CHECK \(\s*permissions <@ ARRAY\[([^\]]*)\]`).FindAllStringSubmatch(source, -1)
	if all == nil {
		t.Fatal("no migration holds a role_known_permissions constraint")
	}
	if got := quoted(all[len(all)-1][1]); !slices.Equal(got, words(AllPermissions)) {
		t.Errorf("the database knows %v, the code %v", got, words(AllPermissions))
	}
	for _, d := range Builtins() {
		row := regexp.MustCompile(`\('` + string(d.Key) + `', '([^']*)',\s*'((?:[^']|'')*)',\s*(true|false), ARRAY\[([^\]]*)\]`).FindStringSubmatch(source)
		if row == nil {
			t.Errorf("no migration seeds %s", d.Key)
			continue
		}
		description := row[2]
		reworded := regexp.MustCompile(`UPDATE role SET description = '((?:[^']|'')*)'\s*WHERE key = '`+string(d.Key)+`'`).
			FindAllStringSubmatch(source, -1)
		if len(reworded) > 0 {
			description = reworded[len(reworded)-1][1]
		}
		if row[1] != d.Name || description != strings.ReplaceAll(d.Description, "'", "''") || (row[3] == "true") != d.InstallationWideOnly {
			t.Errorf("%s is seeded as %q, %q, %s", d.Key, row[1], description, row[3])
		}
		seeded := quoted(row[4])
		added := regexp.MustCompile(`UPDATE role SET permissions = permissions \|\| ARRAY\[([^\]]*)\]::text\[\]\s*WHERE key = '`+
			string(d.Key)+`'`).FindAllStringSubmatch(source, -1)
		for _, more := range added {
			seeded = append(seeded, quoted(more[1])...)
		}
		got, want := slices.Sorted(slices.Values(seeded)), slices.Sorted(slices.Values(words(d.Permissions)))
		if !slices.Equal(got, want) {
			t.Errorf("%s is seeded with %v, the code grants %v", d.Key, got, want)
		}
	}
}

// migrationsUp is the Up half of every migration, in the order they run.
func migrationsUp(t *testing.T) string {
	t.Helper()
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found (%v)", err)
	}
	slices.Sort(files)
	var out strings.Builder
	for _, name := range files {
		text, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(text), "-- +goose Down")
		out.WriteString(up)
	}
	return out.String()
}

func quoted(list string) []string {
	out := []string{}
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(list, -1) {
		out = append(out, m[1])
	}
	return out
}

func words(perms []Permission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	return out
}

func onSkill(role Role, id uuid.UUID) Grant { return Grant{Role: role, Skill: id} }

// A grant on a skill answers there alone: never for another skill, a workflow, or the installation.
func TestASkillGrantDoesNotLeak(t *testing.T) {
	set := NewSet([]Grant{onSkill(SkillEditor, here)})
	if !set.CanSkill(SkillEdit, here) || !set.CanSkill(SkillRead, here) || set.CanSkill(SkillAdminister, here) {
		t.Error("Skill editor on a skill does not read and edit it alone")
	}
	if set.CanSkill(SkillRead, elsewhere) || set.CanInstallation(SkillRead) || set.Can(WorkflowRead, here) {
		t.Error("a grant over one skill answered for another skill, the installation or a workflow")
	}
	if !set.CanSomewhere(SkillEdit) || set.Empty() {
		t.Error("somebody who edits a skill is not offered editing")
	}
	if got := set.SkillPermissions()[here]; len(got) != 2 {
		t.Errorf("the skill's permissions are %v", got)
	}
}

func TestEverySkillRoleReadsAndOnlyItsAdministratorShares(t *testing.T) {
	for _, role := range []Role{SkillAdministrator, SkillEditor, SkillReader} {
		if !role.Allows(SkillRead) || role.InstallationWideOnly() || role.Allows(SkillCreate) {
			t.Errorf("%s does not read one skill, or reaches past it", role)
		}
		if role.Allows(SkillAdminister) != (role == SkillAdministrator) {
			t.Errorf("%s administers %v", role, role.Allows(SkillAdminister))
		}
	}
	if !Author.Allows(SkillCreate) || !Administrator.Allows(SkillAdminister) {
		t.Error("an author does not create skills, or the administrator does not administer them")
	}
	if !NewSet([]Grant{installationWide(Administrator)}).CanSkill(SkillEdit, uuid.New()) {
		t.Error("the administrator does not edit every skill")
	}
}
