package perm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// RoleInput is a role as the matrix sends it; nil leaves a field alone on an edit.
type RoleInput struct {
	Key                  Role
	Name                 *string
	Description          *string
	InstallationWideOnly *bool
	Permissions          []Permission
}

var (
	// ErrRoleNameTaken is returned when another role already has the name.
	ErrRoleNameTaken = errors.New("there is already a role with that name, so choose another")
	// ErrRoleKey is returned for a key that is not a short lowercase word.
	ErrRoleKey = errors.New("a role's key is 2 to 40 lowercase letters, digits and underscores, starting with a letter")
	// ErrRoleName is returned for a missing name.
	ErrRoleName = errors.New("give the role a name")
	// ErrUnknownPermission is returned for a permission the code does not check for.
	ErrUnknownPermission = errors.New("that is not a permission; choose one the matrix lists")
	// ErrLockout is returned when the Administrator would stop administering Rotor.
	ErrLockout = errors.New("the administrator keeps administering Rotor, or nobody could undo this")
)

var roleKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// KeyFromName makes a key out of a name: lowercase words joined by underscores, which is what the built-in keys look
// like.
func KeyFromName(name string) Role {
	var b strings.Builder
	last := '_'
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			last = r
		case last != '_':
			b.WriteRune('_')
			last = '_'
		}
	}
	key := strings.Trim(b.String(), "_")
	if key != "" && (key[0] < 'a' || key[0] > 'z') {
		key = "r_" + key
	}
	if len(key) > config.MaxRoleKey {
		key = strings.TrimRight(key[:config.MaxRoleKey], "_")
	}
	return Role(key)
}

// cleanPermissions keeps the permissions in the matrix's order, once each, and refuses one the code does not know.
func cleanPermissions(in []Permission) ([]string, error) {
	for _, p := range in {
		if !slices.Contains(AllPermissions, p) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownPermission, p)
		}
	}
	out := []string{}
	for _, p := range AllPermissions {
		if slices.Contains(in, p) {
			out = append(out, string(p))
		}
	}
	return out, nil
}

// CreateRole adds a role of the installation's own, as the person on the context, whom the policies hold to
// administering Rotor.
func (s *Store) CreateRole(ctx context.Context, in RoleInput, actor uuid.UUID) (*Definition, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, ErrRoleName
	}
	key := in.Key
	if key == "" {
		key = KeyFromName(name)
	}
	if !roleKey.MatchString(string(key)) {
		return nil, ErrRoleKey
	}
	perms, err := cleanPermissions(in.Permissions)
	if err != nil {
		return nil, err
	}
	description := ""
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	wholeInstallation := in.InstallationWideOnly != nil && *in.InstallationWideOnly

	var out *Definition
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO role (key, name, description, installation_wide_only, permissions)
			VALUES ($1, $2, $3, $4, $5)`, string(key), name, description, wholeInstallation, perms)
		switch {
		case isUniqueViolation(err):
			return ErrRoleNameTaken
		case isPermissionDenied(err):
			return ErrForbidden
		case isCheckViolation(err):
			return ErrRoleKey
		case err != nil:
			return fmt.Errorf("create role: %w", err)
		}
		out, err = scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, string(key)))
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRoleDefined, TargetType: "role", TargetID: string(key),
			Actor: actor, Data: map[string]any{"name": name, "permissions": perms}})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateRole renames a role, describes it, or changes what it grants; every grant of it follows from the next request.
func (s *Store) UpdateRole(ctx context.Context, in RoleInput, actor uuid.UUID) (*Definition, error) {
	var perms []string
	if in.Permissions != nil {
		cleaned, err := cleanPermissions(in.Permissions)
		if err != nil {
			return nil, err
		}
		perms = cleaned
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, ErrRoleName
	}
	if in.Key == Administrator && perms != nil && !slices.Contains(perms, string(RotorAdminister)) {
		return nil, ErrLockout
	}
	var out *Definition
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1 FOR UPDATE OF r`, string(in.Key)))
		if err != nil {
			return err
		}
		name, description, wholeInstallation := current.Name, current.Description, current.InstallationWideOnly
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.InstallationWideOnly != nil {
			wholeInstallation = *in.InstallationWideOnly
		}
		if perms == nil {
			perms = make([]string, 0, len(current.Permissions))
			for _, p := range current.Permissions {
				perms = append(perms, string(p))
			}
		}
		tag, err := tx.Exec(ctx, `
			UPDATE role SET name = $2, description = $3, installation_wide_only = $4, permissions = $5
			WHERE key = $1`, string(in.Key), name, description, wholeInstallation, perms)
		switch {
		case isUniqueViolation(err):
			return ErrRoleNameTaken
		case violates(err, EveryoneAdministers):
			return ErrEveryoneAdministers
		case isCheckViolation(err):
			return ErrLockout
		case err != nil:
			return fmt.Errorf("update role: %w", err)
		case tag.RowsAffected() == 0:
			return ErrForbidden
		}
		out, err = scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, string(in.Key)))
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRoleRedefined, TargetType: "role", TargetID: string(in.Key),
			Actor: actor, Data: map[string]any{"name": name, "permissions": perms}})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
