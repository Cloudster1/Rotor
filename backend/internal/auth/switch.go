package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

var (
	// ErrNoSuchPerson is switching somebody Rotor does not know.
	ErrNoSuchPerson = errors.New("that person is not known to Rotor")
	// ErrLastActiveAdministrator refuses switching off the last person who may administer Rotor, as the database does.
	ErrLastActiveAdministrator = errors.New("the last active administrator cannot be switched off. Make somebody else an administrator first")
)

// keepsAnAdministrator is the database's refusal of the same, raised by app_user_keeps_an_administrator.
const keepsAnAdministrator = "app_user_keeps_an_administrator"

func violates(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == constraint
}

// SwitchOn lets a switched-off person sign in again; they rejoin Everyone, and their own grants answer once more.
func (s *Service) SwitchOn(ctx context.Context, actor, userID uuid.UUID) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE app_user SET is_active = true WHERE id = $1 AND NOT is_active`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var known bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1)`, userID).Scan(&known); err != nil {
				return err
			}
			if !known {
				return fmt.Errorf("switch on %s: %w", userID, ErrNoSuchPerson)
			}
			return nil
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSwitchedOn, TargetType: "user", TargetID: userID.String(), Actor: actor})
	})
}
