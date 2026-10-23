package workspaces

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrNotAdministrator is a change by somebody row level security does not let change the setting.
var ErrNotAdministrator = errors.New("only an administrator changes what workspaces may use, in person")

// Service reads and changes the setting.
type Service struct{ db *db.Cluster }

// New returns the setting's service.
func New(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const columns = `images, package_mirror, max_step_time, max_volume, updated_at`

func scan(row pgx.Row) (Setting, error) {
	var s Setting
	var images []byte
	if err := row.Scan(&images, &s.PackageMirror, &s.MaxStepTime, &s.MaxVolume, &s.UpdatedAt); err != nil {
		return Setting{}, err
	}
	s.Images = []Image{}
	return s, json.Unmarshal(images, &s.Images)
}

// Get is the setting as the signed-in person reads it.
func (s *Service) Get(ctx context.Context) (Setting, error) {
	var out Setting
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM workspace_setting`))
		return err
	})
	return out, err
}

// Current is the setting as Rotor reads it, for the worker that starts a run's sandbox.
func (s *Service) Current(ctx context.Context) (Setting, error) {
	var out Setting
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM workspace_setting`))
		return err
	})
	return out, err
}

// ImageNames are the images workflows may name, as the person saving one reads them.
func (s *Service) ImageNames(ctx context.Context) ([]string, error) {
	setting, err := s.Get(ctx)
	return setting.Names(), err
}

// Set changes the setting as an administrator, in person, and records what it was and what it is.
func (s *Service) Set(ctx context.Context, in Setting) (Setting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Setting{}, auth.ErrNoSession
	}
	if in.Images == nil {
		in.Images = []Image{}
	}
	if err := in.Check(); err != nil {
		return Setting{}, err
	}
	images, err := json.Marshal(in.Images)
	if err != nil {
		return Setting{}, err
	}
	var out Setting
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var before Setting
		var err error
		if before, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM workspace_setting`)); err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, `UPDATE workspace_setting
			SET images = $1, package_mirror = $2, max_step_time = $3, max_volume = $4, changed_by = $5 RETURNING `+columns,
			images, in.PackageMirror, in.MaxStepTime, in.MaxVolume, p.UserID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAdministrator
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWorkspacesChanged, TargetType: "workspace_setting", Actor: p.UserID,
			Data: map[string]any{"images": out.Images, "packageMirror": out.PackageMirror, "maxStepTime": out.MaxStepTime,
				"maxVolume": out.MaxVolume, "previousImages": before.Images}})
	})
	return out, err
}
