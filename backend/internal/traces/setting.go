// Package traces keeps each run's trace in Postgres for whoever reads the run (#242): the store, the cap's numbers,
// sealing a span's text with the run's key, the retention setting and the sweep. docs/api-contract-m18.md says what.
package traces

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
	"github.com/cloudster1/rotor/backend/internal/envelope"
)

// ErrDays is a retention outside what Rotor keeps traces for.
var ErrDays = errors.New("the trace retention is out of range")

// DaysSentence says what an administrator may set, for the field they set it in.
var DaysSentence = fmt.Sprintf("Keep traces for a whole number of days from 1 to %d.", config.MaxTraceRetentionDays)

// Setting is how long traces are kept and whether their text is, and who changed it last.
type Setting struct {
	RetentionDays int
	KeepContent   bool
	ChangedBy     *string
	UpdatedAt     time.Time
}

// Service reads and changes the setting, stores runs' spans and sweeps those past the retention.
type Service struct {
	db *db.Cluster
	// Keys seals a span's text with its run's data key; nil stores no text.
	Keys *envelope.Keys
	log  *slog.Logger
	now  func() time.Time

	mu       sync.Mutex
	keep     bool
	readKeep time.Time

	sweepMu sync.Mutex
	swept   time.Time
}

// NewService returns the traces kept in cluster.
func NewService(cluster *db.Cluster, log *slog.Logger) *Service {
	return &Service{db: cluster, log: log, now: time.Now}
}

// Get is the setting, for whoever is signed in.
func (s *Service) Get(ctx context.Context) (Setting, error) {
	var out Setting
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT t.retention_days, t.keep_content, u.name, t.updated_at FROM trace_setting t
			LEFT JOIN app_user u ON u.id = t.changed_by`).Scan(&out.RetentionDays, &out.KeepContent, &out.ChangedBy, &out.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Setting{}, auth.ErrNoSession
	}
	return out, err
}

// Set changes the setting as the administrator on the context, audited; the database refuses anyone else.
func (s *Service) Set(ctx context.Context, days int, keepContent bool) (Setting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Setting{}, auth.ErrNoSession
	}
	if days < 1 || days > config.MaxTraceRetentionDays {
		return Setting{}, ErrDays
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE trace_setting SET retention_days = $1, keep_content = $2, changed_by = $3`,
			days, keepContent, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// The policy lets administrators alone change it, so nothing was.
			return auth.ErrNoSession
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTracesChanged, TargetType: "trace_setting", Actor: p.UserID,
			Data: map[string]any{"retentionDays": days, "keepContent": keepContent}})
	})
	if err != nil {
		return Setting{}, err
	}
	s.mu.Lock()
	s.readKeep = time.Time{}
	s.mu.Unlock()
	return s.Get(ctx)
}

// KeepContent says whether Rotor keeps spans' text now, read again after config.TraceSettingTTL; a setting that
// cannot be read keeps what was read last, else none.
func (s *Service) KeepContent(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.readKeep.IsZero() && s.now().Sub(s.readKeep) < config.TraceSettingTTL {
		return s.keep
	}
	var keep bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT keep_content FROM trace_setting`).Scan(&keep)
	})
	if err != nil {
		if s.log != nil {
			s.log.Debug("could not read whether traces keep their text", "error", err)
		}
		return s.keep && !s.readKeep.IsZero()
	}
	s.keep, s.readKeep = keep, s.now()
	return keep
}
