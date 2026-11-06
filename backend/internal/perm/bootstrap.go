package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// LetInMembers lets in whom ROTOR_BOOTSTRAP_MEMBERS names (#171), as Stator's seed does: a name not yet signed in gets
// an account their first sign-in takes by address, and nobody is let out. It answers how many it let in now.
func LetInMembers(ctx context.Context, cluster *db.Cluster, members []config.Member) (int, error) {
	added := 0
	err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		added = 0
		for _, m := range members {
			id, now, err := letInByAddress(ctx, tx, m.Email)
			if err != nil {
				return fmt.Errorf("let %s in: %w", m.Email, err)
			}
			if !now {
				continue
			}
			added++
			role := StandingMember
			// As in the siblings, the list makes an administrator only of somebody it lets in, never of one already in.
			if m.Admin {
				if err := grantAdministrator(ctx, tx, id, uuid.Nil); err != nil {
					return err
				}
				role = string(Administrator)
			}
			if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionMemberAdmitted, TargetType: "user", TargetID: id.String(),
				Data: map[string]any{"role": role, "via": ViaBootstrap}}); err != nil {
				return err
			}
		}
		return nil
	})
	return added, err
}

// letInByAddress makes or finds the person with the address and lets them in, saying whether this did.
func letInByAddress(ctx context.Context, tx db.DBTX, email string) (uuid.UUID, bool, error) {
	var (
		id    uuid.UUID
		letIn bool
	)
	err := tx.QueryRow(ctx, `SELECT id, standing IS NOT NULL FROM app_user WHERE email = $1`, email).Scan(&id, &letIn)
	if errors.Is(err, pgx.ErrNoRows) {
		name, _, _ := strings.Cut(email, "@")
		// Another replica starting at the same moment may make the row first; then it has let them in.
		err = tx.QueryRow(ctx, `INSERT INTO app_user (email, name, standing) VALUES ($1, $2, $3)
			ON CONFLICT (email) DO NOTHING RETURNING id`, email, name, StandingMember).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return id, false, nil
		}
		return id, err == nil, err
	}
	if err != nil || letIn {
		return id, false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE app_user SET standing = $2 WHERE id = $1 AND standing IS NULL`, id, StandingMember); err != nil {
		return id, false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM join_request WHERE user_id = $1`, id); err != nil {
		return id, false, err
	}
	return id, true, nil
}
