package perm

// Letting people in (#204), ported from Armature's auth/joinrequests.go and oidc/signin.go: a person the provider knows
// waits until an administrator lets them in, or a realm group of theirs holding a grant does.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrNoSuchRequest is letting in or turning away somebody who is not waiting.
var ErrNoSuchRequest = errors.New("nobody by that name is waiting to be let in. Reload the page to see who is")

// StandingMember is the standing of everybody let in; administering Rotor is a grant, not a standing.
const StandingMember = "member"

// How a person came to be let in, as the audit log says.
const (
	ViaUsersPage = "users_page"
	ViaSignIn    = "sign_in"
	ViaBootstrap = "bootstrap"
)

// JoinRequest is a person who signed in through the provider and waits to be let in.
type JoinRequest struct {
	UserID      uuid.UUID
	Email       string
	Name        string
	RequestedAt time.Time
}

// JoinRequests lists who is waiting, oldest first, for an administrator of Rotor.
func (s *Store) JoinRequests(ctx context.Context) ([]JoinRequest, error) {
	out := []JoinRequest{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email::text, u.name, r.created_at
			FROM join_request r JOIN app_user u ON u.id = r.user_id
			ORDER BY r.created_at, u.email`)
		if err != nil {
			return fmt.Errorf("list who is waiting: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r JoinRequest
			if err := rows.Scan(&r.UserID, &r.Email, &r.Name, &r.RequestedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// AdmitJoinRequest lets a waiting person in as a member, and as an Administrator too when admin is set; they join
// Everyone by the database's trigger and sign in from their next attempt.
func (s *Store) AdmitJoinRequest(ctx context.Context, actor, userID uuid.UUID, admin bool) (*User, error) {
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM join_request WHERE user_id = $1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchRequest
		}
		if _, err := tx.Exec(ctx, `UPDATE app_user SET standing = $2 WHERE id = $1 AND standing IS NULL`, userID, StandingMember); err != nil {
			return fmt.Errorf("let the person in: %w", err)
		}
		role := StandingMember
		if admin {
			if err := grantAdministrator(ctx, tx, userID, actor); err != nil {
				return err
			}
			role = string(Administrator)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberAdmitted, TargetType: "user", TargetID: userID.String(),
			Actor: actor, Data: map[string]any{"role": role, "via": ViaUsersPage}})
	})
	if err != nil {
		return nil, err
	}
	return s.User(ctx, userID)
}

// DeclineJoinRequest turns a waiting person away: the request goes, the person stays outside and may ask again.
func (s *Store) DeclineJoinRequest(ctx context.Context, actor, userID uuid.UUID) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := require(ctx, tx, actorAdministers); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM join_request WHERE user_id = $1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchRequest
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberDeclined, TargetType: "user", TargetID: userID.String(), Actor: actor})
	})
}

// NoteJoinRequest keeps a waiting person's request; asking twice is one request, kept from the first time.
func NoteJoinRequest(ctx context.Context, tx db.DBTX, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `INSERT INTO join_request (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return fmt.Errorf("keep the request to be let in: %w", err)
	}
	return nil
}

// MappedGroups are the realm groups among claimed that hold a grant, by name; with administratorsOnly, only those
// holding one that administers Rotor, which let their members in whatever the setting says.
func MappedGroups(ctx context.Context, tx db.DBTX, claimed []string, administratorsOnly bool) ([]string, error) {
	if len(claimed) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT g.name FROM user_group g
		WHERE g.source = 'oidc' AND g.external_ref = ANY ($1)
		  AND EXISTS (SELECT 1 FROM role_assignment a JOIN role r ON r.key = a.role
		              WHERE a.group_id = g.id
		                AND (NOT $2 OR (a.workflow_id IS NULL AND a.skill_id IS NULL AND 'rotor.administer' = ANY (r.permissions))))
		ORDER BY g.name`, claimed, administratorsOnly)
	if err != nil {
		return nil, fmt.Errorf("find the groups that let people in: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// AdmitByGroup lets a waiting person in at sign-in through the first of their groups that holds a grant; it says
// whether it did. The roles come with the group's membership, which sign-in writes next.
func AdmitByGroup(ctx context.Context, tx db.DBTX, userID uuid.UUID, groups []string) (bool, error) {
	if len(groups) == 0 {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE app_user SET standing = $2 WHERE id = $1 AND standing IS NULL`, userID, StandingMember)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM join_request WHERE user_id = $1`, userID); err != nil {
		return false, err
	}
	return true, audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberAdmitted, TargetType: "user", TargetID: userID.String(),
		Actor: userID, Data: map[string]any{"group": groups[0], "role": StandingMember, "via": ViaSignIn}})
}

// grantAdministrator gives a person Administrator over the installation; holding it already is no change.
func grantAdministrator(ctx context.Context, tx db.DBTX, userID, actor uuid.UUID) error {
	var by *uuid.UUID
	if actor != uuid.Nil {
		by = &actor
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, user_id, created_by) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		string(Administrator), userID, by); err != nil {
		return fmt.Errorf("grant Administrator: %w", err)
	}
	return nil
}
