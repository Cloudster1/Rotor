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

// GrantInput names a role, a scope and a subject; a nil Workflow is the whole installation.
type GrantInput struct {
	Role     Role
	Workflow uuid.UUID
	// Exactly one of these is set.
	UserID  *uuid.UUID
	GroupID *uuid.UUID
}

// administratorsLock serializes taking Administrator away; the database's guard on role_assignment takes the same one.
const administratorsLock = "rotor.administrators"

// Grant gives a role to a person or a group, as the person on the context, whose own grants the policies check.
// Granting the same thing twice is not an error: it is the state the caller asked for, and the same one grant.
func (s *Store) Grant(ctx context.Context, in GrantInput, actor uuid.UUID) (uuid.UUID, error) {
	if in.Role == "" {
		return uuid.Nil, ErrRoleNotFound
	}
	if (in.UserID == nil) == (in.GroupID == nil) {
		return uuid.Nil, ErrSubject
	}
	var workflow *uuid.UUID
	if in.Workflow != uuid.Nil {
		workflow = &in.Workflow
	}
	var id uuid.UUID
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The role is the installation's own: whether it answers for the whole installation is its row's to say.
		var wholeInstallation bool
		err := tx.QueryRow(ctx, `SELECT installation_wide_only FROM role WHERE key = $1`, string(in.Role)).Scan(&wholeInstallation)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrRoleNotFound, in.Role)
		}
		if err != nil {
			return err
		}
		if wholeInstallation && workflow != nil {
			return fmt.Errorf("%w: %s", ErrScope, in.Role)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_assignment (role, workflow_id, user_id, group_id, created_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (role, workflow_id, user_id, group_id) DO NOTHING`,
			string(in.Role), workflow, in.UserID, in.GroupID, actor); err != nil {
			switch {
			case violates(err, EveryoneAdministers):
				return ErrEveryoneAdministers
			case isPermissionDenied(err):
				return ErrForbidden
			case isCheckViolation(err):
				return fmt.Errorf("%w: %s", ErrScope, in.Role)
			}
			return fmt.Errorf("grant role: %w", err)
		}
		err = tx.QueryRow(ctx, `
			SELECT id FROM role_assignment
			WHERE role = $1 AND workflow_id IS NOT DISTINCT FROM $2 AND user_id IS NOT DISTINCT FROM $3
			  AND group_id IS NOT DISTINCT FROM $4`, string(in.Role), workflow, in.UserID, in.GroupID).Scan(&id)
		if err != nil {
			return fmt.Errorf("read the grant back: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRoleGranted, TargetType: "grant", TargetID: id.String(), Actor: actor,
			Data: map[string]any{"role": in.Role, "workflow": workflow, "user": in.UserID, "group": in.GroupID}})
	})
	return id, err
}

// Revoke takes a grant away. The last Administrator grant cannot go: an installation nobody can administer is one
// where the fix is a database console.
func (s *Store) Revoke(ctx context.Context, id uuid.UUID, actor uuid.UUID) error {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var role Role
		err := tx.QueryRow(ctx, `SELECT role FROM role_assignment WHERE id = $1`, id).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if role == Administrator {
			// Counted under the lock, so two revokes at once cannot each count the other as the one that stays.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, administratorsLock); err != nil {
				return fmt.Errorf("wait for other revokes: %w", err)
			}
			var remaining int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE role = $1 AND id <> $2`,
				string(Administrator), id).Scan(&remaining); err != nil {
				return err
			}
			if remaining == 0 {
				return ErrLastAdministrator
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, id)
		if violates(err, KeepsAnAdministrator) {
			return ErrLastAdministrator
		}
		if err != nil {
			return fmt.Errorf("revoke role: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrForbidden
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRoleRevoked, TargetType: "grant", TargetID: id.String(), Actor: actor,
			Data: map[string]any{"role": role}})
	})
}
