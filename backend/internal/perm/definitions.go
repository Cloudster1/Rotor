package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Definition is one of the installation's roles: what it is called and what it grants. The built-in ones are rows
// like any other, editable but not deletable; a role an administrator added is wholly theirs.
type Definition struct {
	Key                  Role
	Name                 string
	Description          string
	InstallationWideOnly bool
	Builtin              bool
	Permissions          []Permission
	// InUse counts the grants that name it, so a listing can say what deleting it would take away.
	InUse int
}

// Definitions is what each of the installation's roles grants, by key.
type Definitions map[Role][]Permission

// builtinWords names and describes each built-in role, as the migration installs them.
var builtinWords = map[Role][2]string{
	Administrator:         {"Administrator", "Administers Rotor: its tools, providers, settings, audit log, and every workflow and grant in it."},
	WorkflowAdministrator: {"Workflow administrator", "Shares one workflow, archives it, and sees and cancels everyone's runs of it."},
	Editor:                {"Editor", "Saves new versions of one workflow and runs it."},
	Runner:                {"Runner", "Sees one workflow and starts runs of it."},
	Author:                {"Author", "Creates workflows and skills of their own."},
	SkillAdministrator:    {"Skill administrator", "Saves new versions of one skill, shares it and archives it."},
	SkillEditor:           {"Skill editor", "Saves new versions of one skill."},
	SkillReader:           {"Skill reader", "Reads one skill and its versions, and gives it to the agents of their workflows."},
}

// Builtins are the roles every installation starts with. NewSet without definitions reads from here, which is what a
// unit test wants and what a request never gets.
func Builtins() []Definition {
	out := make([]Definition, 0, len(Roles))
	for _, role := range Roles {
		out = append(out, Definition{
			Key: role, Name: builtinWords[role][0], Description: builtinWords[role][1],
			InstallationWideOnly: role.InstallationWideOnly(), Builtin: true, Permissions: role.Permissions(),
		})
	}
	return out
}

func builtinDefinitions() Definitions {
	out := Definitions{}
	for _, d := range Builtins() {
		out[d.Key] = d.Permissions
	}
	return out
}

// AllPermissions is every permission the code checks for, in the order the matrix lists them. The database's
// role_known_permissions constraint holds the same list.
var AllPermissions = []Permission{
	WorkflowRead, WorkflowRun, WorkflowEdit, WorkflowAdminister, WorkflowCreate,
	SkillRead, SkillEdit, SkillAdminister, SkillCreate, RotorAdminister,
}

// PermissionWords says what each permission lets somebody do, for a matrix that people read rather than parse.
var PermissionWords = map[Permission]string{
	WorkflowRead:       "See a workflow, its versions and what changed between them",
	WorkflowRun:        "Start runs of a workflow and follow their own",
	WorkflowEdit:       "Save new versions of a workflow",
	WorkflowAdminister: "Share and archive a workflow, and see and cancel everyone's runs of it",
	WorkflowCreate:     "Create workflows",
	SkillRead:          "Read a skill and its versions, and give it to agents",
	SkillEdit:          "Save new versions of a skill",
	SkillAdminister:    "Share and archive a skill",
	SkillCreate:        "Create skills",
	RotorAdminister:    "Administer Rotor",
}

var (
	// ErrRoleNotFound is returned for a role nobody defined.
	ErrRoleNotFound = errors.New("there is no such role")
	// ErrBuiltinRole is returned when a built-in role is deleted.
	ErrBuiltinRole = errors.New("a built-in role cannot be deleted; take its grants away instead")
)

const selectRoles = `
SELECT r.key, r.name, r.description, r.installation_wide_only, r.builtin, r.permissions,
       (SELECT count(*) FROM role_assignment a WHERE a.role = r.key)
FROM role r`

func scanDefinition(row pgx.Row) (*Definition, error) {
	var (
		d     Definition
		perms []string
	)
	err := row.Scan(&d.Key, &d.Name, &d.Description, &d.InstallationWideOnly, &d.Builtin, &perms, &d.InUse)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Permissions = make([]Permission, 0, len(perms))
	for _, p := range perms {
		d.Permissions = append(d.Permissions, Permission(p))
	}
	return &d, nil
}

// Roles lists the installation's roles, most powerful first as installed, then the administrators' own by name.
func (s *Store) Roles(ctx context.Context) ([]Definition, error) {
	out := []Definition{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRoles+` ORDER BY r.sort_order, lower(r.name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDefinition(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

// definitionsFor reads what each role grants, for resolving one person.
func definitionsFor(ctx context.Context, tx db.DBTX) (Definitions, error) {
	rows, err := tx.Query(ctx, `SELECT key, permissions FROM role`)
	if err != nil {
		return nil, fmt.Errorf("read roles: %w", err)
	}
	defer rows.Close()
	out := Definitions{}
	for rows.Next() {
		var (
			key   Role
			perms []string
		)
		if err := rows.Scan(&key, &perms); err != nil {
			return nil, err
		}
		list := make([]Permission, 0, len(perms))
		for _, p := range perms {
			list = append(list, Permission(p))
		}
		out[key] = list
	}
	return out, rows.Err()
}

// DeleteRole removes a role an administrator added, and every grant of it.
func (s *Store) DeleteRole(ctx context.Context, key Role, actor uuid.UUID) error {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, string(key)))
		if err != nil {
			return err
		}
		if current.Builtin {
			return ErrBuiltinRole
		}
		tag, err := tx.Exec(ctx, `DELETE FROM role WHERE key = $1`, string(key))
		switch {
		case isCheckViolation(err):
			return ErrBuiltinRole
		case err != nil:
			return fmt.Errorf("delete role: %w", err)
		case tag.RowsAffected() == 0:
			return ErrForbidden
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRoleRemoved, TargetType: "role", TargetID: string(key),
			Actor: actor, Data: map[string]any{"grants": current.InUse}})
	})
}
