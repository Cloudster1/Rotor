package triggers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrNotAdministrator is a change to the setting by somebody row level security does not let change it.
var ErrNotAdministrator = errors.New("only an administrator allows or forbids unattended writes, in person")

// UnattendedSetting is whether triggers may write through named calls with no approval (#128).
type UnattendedSetting struct {
	Allowed   bool
	UpdatedAt time.Time
}

// UnattendedAllowed says whether triggers may write through named calls with no approval, read as the person on
// the context, who is a trigger's owner at a firing.
func (s *Service) UnattendedAllowed(ctx context.Context) (bool, error) {
	setting, err := s.UnattendedSetting(ctx)
	return setting.Allowed, err
}

// UnattendedSetting is the setting as the signed-in person reads it.
func (s *Service) UnattendedSetting(ctx context.Context) (UnattendedSetting, error) {
	var out UnattendedSetting
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT allowed, updated_at FROM unattended_setting`).Scan(&out.Allowed, &out.UpdatedAt)
	})
	return out, err
}

// SetUnattended allows or forbids unattended writes for everyone. Forbidding turns off at once every trigger whose
// owner agreed to an unattended call; allowing them again turns none back on.
func (s *Service) SetUnattended(ctx context.Context, allowed bool) (UnattendedSetting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return UnattendedSetting{}, auth.ErrNoSession
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var before bool
		err := tx.QueryRow(ctx, `WITH before AS (SELECT allowed FROM unattended_setting)
			UPDATE unattended_setting SET allowed = $1, changed_by = $2 RETURNING (SELECT allowed FROM before)`,
			allowed, p.UserID).Scan(&before)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAdministrator
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionUnattendedSwitched, TargetType: "unattended_setting", Actor: p.UserID,
			Data: map[string]any{"allowed": allowed, "previous": before}})
	})
	if err != nil {
		return UnattendedSetting{}, err
	}
	if !allowed {
		if err := s.forbidUnattended(ctx); err != nil {
			return UnattendedSetting{}, err
		}
	}
	return s.UnattendedSetting(ctx)
}

// forbidUnattended turns off every enabled trigger whose owner agreed to an unattended call, as Rotor.
func (s *Service) forbidUnattended(ctx context.Context) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id FROM trigger WHERE enabled AND cardinality(allow_unattended) > 0 FOR UPDATE`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		var errs []error
		for _, id := range ids {
			errs = append(errs, turnOff(ctx, tx, id, ReasonUnattendedForbidden, ""))
		}
		return errors.Join(errs...)
	})
}
