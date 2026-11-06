// Package perm answers whether a person may do a thing, over the installation or on one workflow or skill, from the
// roles granted to them and their groups; row level security asks the same grants through actor_can().
package perm

import (
	"slices"

	"github.com/google/uuid"
)

// Role is a named bundle of permissions.
type Role string

const (
	// Administrator administers the installation: its tools, providers, settings, audit log and every workflow.
	Administrator Role = "administrator"
	// WorkflowAdministrator shares one workflow, archives it and sees everyone's runs of it.
	WorkflowAdministrator Role = "workflow_administrator"
	// Editor saves new versions of one workflow and runs it.
	Editor Role = "editor"
	// Runner sees one workflow and starts runs of it.
	Runner Role = "runner"
	// Author creates workflows and skills of their own; Everyone holds it, so anyone who signs in may.
	Author Role = "author"
	// SkillAdministrator saves, shares and archives one skill; whoever creates a skill holds it there (#219).
	SkillAdministrator Role = "skill_administrator"
	// SkillEditor saves new versions of one skill.
	SkillEditor Role = "skill_editor"
	// SkillReader reads one skill, which lets their workflows' agents use it.
	SkillReader Role = "skill_reader"
)

// Roles are listed most powerful first, which is the order they should be offered in and a listing reads best in.
var Roles = []Role{Administrator, WorkflowAdministrator, Editor, Runner, Author, SkillAdministrator, SkillEditor, SkillReader}

// Valid reports whether the role is one of the built-in ones.
func (r Role) Valid() bool { return slices.Contains(Roles, r) }

// InstallationWideOnly reports whether a built-in role only makes sense over the whole installation. Administering
// Rotor from inside one workflow is not a smaller version of the same thing; it is a different thing.
func (r Role) InstallationWideOnly() bool { return r == Administrator || r == Author }

// Permission is one thing somebody may do.
type Permission string

const (
	// WorkflowRead is seeing a workflow, its versions and what changed between them.
	WorkflowRead Permission = "workflow.read"
	// WorkflowRun is starting runs of a workflow and seeing one's own.
	WorkflowRun Permission = "workflow.run"
	// WorkflowEdit is saving new versions of a workflow.
	WorkflowEdit Permission = "workflow.edit"
	// WorkflowAdminister is granting roles on a workflow, archiving and restoring it, and seeing and cancelling everyone's
	// runs of it.
	WorkflowAdminister Permission = "workflow.administer"
	// WorkflowCreate is making a new workflow, which is an installation-wide act.
	WorkflowCreate Permission = "workflow.create"
	// SkillRead is reading a skill, its versions and files, and so giving it to an agent (#219).
	SkillRead Permission = "skill.read"
	// SkillEdit is saving new versions of a skill.
	SkillEdit Permission = "skill.edit"
	// SkillAdminister is granting roles on a skill, archiving and restoring it.
	SkillAdminister Permission = "skill.administer"
	// SkillCreate is making a new skill, which is an installation-wide act.
	SkillCreate Permission = "skill.create"
	// RotorAdminister is the installation's own settings: tools, providers, limits, the audit log and everyone's grants.
	RotorAdminister Permission = "rotor.administer"
)

// grants is what each built-in role can do. A role's permissions are exactly this list; there is no inheritance
// between roles, because "an editor is a runner who saves" stops being true the moment somebody wants one who cannot run.
var grants = map[Role][]Permission{
	Administrator:         AllPermissions,
	WorkflowAdministrator: {WorkflowRead, WorkflowRun, WorkflowEdit, WorkflowAdminister},
	Editor:                {WorkflowRead, WorkflowRun, WorkflowEdit},
	Runner:                {WorkflowRead, WorkflowRun},
	Author:                {WorkflowCreate, SkillCreate},
	SkillAdministrator:    {SkillRead, SkillEdit, SkillAdminister},
	SkillEditor:           {SkillRead, SkillEdit},
	SkillReader:           {SkillRead},
}

// Allows reports whether a built-in role grants a permission.
func (r Role) Allows(p Permission) bool { return slices.Contains(grants[r], p) }

// Permissions lists what a built-in role grants, for a page that has to explain itself.
func (r Role) Permissions() []Permission { return slices.Clone(grants[r]) }

// Grant is one role held over one scope: a workflow, a skill, or with neither the whole installation.
type Grant struct {
	Role     Role
	Workflow uuid.UUID
	Skill    uuid.UUID
}

// Installation reports whether the grant answers for the whole installation.
func (g Grant) Installation() bool { return g.Workflow == uuid.Nil && g.Skill == uuid.Nil }

// Set is everything one person may do. It is resolved once per request and carried on the principal, so a handler
// asks a question of memory rather than of the database.
type Set struct {
	// installation is what is granted everywhere, byWorkflow what is granted in one place. Keeping them apart is what
	// stops a workflow grant leaking.
	installation map[Permission]bool
	byWorkflow   map[uuid.UUID]map[Permission]bool
	bySkill      map[uuid.UUID]map[Permission]bool
	roles        []Grant
}

// NewSet turns a list of grants into the answers a request needs, reading what each role grants from the built-in table.
func NewSet(grants []Grant) Set {
	return NewSetWith(grants, builtinDefinitions())
}

// NewSetWith is NewSet over the installation's own roles. A grant of a role the definitions do not name grants nothing.
func NewSetWith(grants []Grant, defs Definitions) Set {
	s := Set{installation: map[Permission]bool{}, byWorkflow: map[uuid.UUID]map[Permission]bool{},
		bySkill: map[uuid.UUID]map[Permission]bool{}, roles: slices.Clone(grants)}
	for _, g := range grants {
		perms, ok := defs[g.Role]
		if !ok {
			continue
		}
		for _, p := range perms {
			switch {
			case g.Installation():
				s.installation[p] = true
			case g.Skill != uuid.Nil:
				allow(s.bySkill, g.Skill, p)
			default:
				allow(s.byWorkflow, g.Workflow, p)
			}
		}
	}
	return s
}

// InstallationPermissions lists what is held over the whole installation, for a client that draws by permission.
func (s Set) InstallationPermissions() []Permission {
	out := []Permission{}
	for _, p := range AllPermissions {
		if s.installation[p] {
			out = append(out, p)
		}
	}
	return out
}

// WorkflowPermissions lists what is held on each workflow named explicitly.
func (s Set) WorkflowPermissions() map[uuid.UUID][]Permission { return listed(s.byWorkflow) }

// SkillPermissions lists what is held on each skill named explicitly.
func (s Set) SkillPermissions() map[uuid.UUID][]Permission { return listed(s.bySkill) }

func listed(scoped map[uuid.UUID]map[Permission]bool) map[uuid.UUID][]Permission {
	out := map[uuid.UUID][]Permission{}
	for id, here := range scoped {
		list := []Permission{}
		for _, p := range AllPermissions {
			if here[p] {
				list = append(list, p)
			}
		}
		out[id] = list
	}
	return out
}

// Grants returns the roles this set was built from, for showing somebody why they can do what they can do.
func (s Set) Grants() []Grant { return slices.Clone(s.roles) }

// CanInstallation reports whether the permission is held over the whole installation: the right question for
// anything not about one workflow, such as creating one or administering Rotor.
func (s Set) CanInstallation(p Permission) bool { return s.installation[p] }

// Can reports whether the permission is held on one workflow, from an installation-wide grant or one made there alone.
func (s Set) Can(p Permission, workflow uuid.UUID) bool {
	if s.installation[p] {
		return true
	}
	return s.byWorkflow[workflow][p]
}

// CanSkill reports whether the permission is held on one skill, from an installation-wide grant or one made there.
func (s Set) CanSkill(p Permission, skill uuid.UUID) bool {
	return s.installation[p] || s.bySkill[skill][p]
}

// CanSomewhere reports whether the permission is held anywhere at all. It answers "should this be offered at all",
// never "may they do it here", which only Can answers.
func (s Set) CanSomewhere(p Permission) bool {
	if s.installation[p] {
		return true
	}
	for _, scoped := range []map[uuid.UUID]map[Permission]bool{s.byWorkflow, s.bySkill} {
		for _, here := range scoped {
			if here[p] {
				return true
			}
		}
	}
	return false
}

// Empty reports whether the set grants nothing at all, which is what somebody with no roles holds.
func (s Set) Empty() bool {
	return len(s.installation) == 0 && len(s.byWorkflow) == 0 && len(s.bySkill) == 0
}

// Workflows lists the workflows this set names explicitly, sorted.
func (s Set) Workflows() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(s.byWorkflow))
	for id := range s.byWorkflow {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

// allow records one permission on one workflow or skill, making the inner map on demand.
func allow(scoped map[uuid.UUID]map[Permission]bool, id uuid.UUID, p Permission) {
	here, ok := scoped[id]
	if !ok {
		here = map[Permission]bool{}
		scoped[id] = here
	}
	here[p] = true
}
