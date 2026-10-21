package oauth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrNotAdministrator is a change to the setting by somebody row level security does not let change it.
var ErrNotAdministrator = errors.New("only an administrator switches assistants on or off, in person")

// Enabled says whether assistants may connect, read before anybody is signed in.
func (s *Service) Enabled(ctx context.Context) (bool, error) {
	var on bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT connections_on FROM assistant_setting`).Scan(&on)
	})
	return on, err
}

// Setting is whether assistants may connect, as the signed-in person reads it.
func (s *Service) Setting(ctx context.Context) (Setting, error) {
	var out Setting
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT connections_on, updated_at FROM assistant_setting`).Scan(&out.ConnectionsOn, &out.UpdatedAt)
	})
	return out, err
}

// Switch turns assistants on or off for the installation. Switching them off refuses every token at once, and
// keeps the grants, so switching back on lets them in again.
func (s *Service) Switch(ctx context.Context, in SettingInput) (Setting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Setting{}, auth.ErrNoSession
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var before bool
		err := tx.QueryRow(ctx, `WITH before AS (SELECT connections_on FROM assistant_setting)
			UPDATE assistant_setting SET connections_on = $1, changed_by = $2 RETURNING (SELECT connections_on FROM before)`,
			in.ConnectionsOn, p.UserID).Scan(&before)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAdministrator
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthSwitched, TargetType: "assistant_setting", Actor: p.UserID,
			Data: map[string]any{"connectionsOn": in.ConnectionsOn, "previous": before}})
	})
	if err != nil {
		return Setting{}, err
	}
	return s.Setting(ctx)
}
