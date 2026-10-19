// Package retention deletes a finished run's data once it has been kept long enough (#41): the installation's
// days, or a workflow's own when shorter. What a run did stays in the audit log, which nothing deletes.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrDays is a retention outside what an installation may keep.
var ErrDays = errors.New("the retention is out of range")

// DaysSentence says what an administrator may set, for the field they set it in.
var DaysSentence = fmt.Sprintf("Keep run data for a whole number of days from 1 to %d.", config.MaxRunDataRetentionDays)

// Setting is the installation's retention, and who set it last.
type Setting struct {
	RunDataDays int
	ChangedBy   *string
	UpdatedAt   time.Time
}

// Service reads and changes the retention, and deletes what is past it.
type Service struct {
	db  *db.Cluster
	log *slog.Logger

	mu   sync.Mutex
	last time.Time
	now  func() time.Time
}

// NewService returns the retention of the installation's runs.
func NewService(cluster *db.Cluster, log *slog.Logger) *Service {
	return &Service{db: cluster, log: log, now: time.Now}
}

// Get is the installation's retention, for whoever is signed in.
func (s *Service) Get(ctx context.Context) (Setting, error) {
	var out Setting
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT r.run_data_days, u.name, r.updated_at FROM retention_setting r
			LEFT JOIN app_user u ON u.id = r.changed_by`).Scan(&out.RunDataDays, &out.ChangedBy, &out.UpdatedAt)
	})
	return out, err
}

// Set changes how many days a finished run's data is kept, for an administrator, audited with the days before.
func (s *Service) Set(ctx context.Context, days int) (Setting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Setting{}, auth.ErrNoSession
	}
	if days < 1 || days > config.MaxRunDataRetentionDays {
		return Setting{}, ErrDays
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var previous int
		err := tx.QueryRow(ctx, `WITH before AS (SELECT run_data_days FROM retention_setting)
			UPDATE retention_setting SET run_data_days = $1, changed_by = $2 RETURNING (SELECT run_data_days FROM before)`,
			days, p.UserID).Scan(&previous)
		if errors.Is(err, pgx.ErrNoRows) {
			// The policy hides the row from anyone but an administrator, so nothing is updated.
			return auth.ErrNoSession
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRetentionChanged, TargetType: "retention_setting", Actor: p.UserID,
			Data: map[string]any{"runDataDays": days, "previous": previous}})
	})
	if err != nil {
		return Setting{}, err
	}
	return s.Get(ctx)
}
