// Package langsmith lets an administrator send runs' spans to LangSmith (#37), off by default; the control plane
// sends them itself, so the key never reaches a sandbox. docs/api-contract-m7-telemetry.md says what is sent.
package langsmith

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

var (
	// ErrNotAcknowledged is turning LangSmith on without saying run data may leave the installation.
	ErrNotAcknowledged = errors.New("turning LangSmith on needs the acknowledgement that run data leaves the installation")
	// ErrNoKey is turning LangSmith on with no key given now or before.
	ErrNoKey = errors.New("LangSmith needs a key before it is turned on")
	// ErrBadInput is a key or project of a shape LangSmith never issues.
	ErrBadInput = errors.New("the key or the project is not of a shape LangSmith takes")
)

// Setting is whether runs' spans go to LangSmith; the key itself is never answered.
type Setting struct {
	Enabled   bool
	Project   string
	KeySetAt  *time.Time
	ChangedBy *string
	UpdatedAt time.Time
}

// Input changes the setting; a nil Key keeps the one held.
type Input struct {
	Enabled               bool
	Project               string
	Key                   *string
	AcknowledgeDataLeaves bool
}

// Keys is where the key is kept; *vault.Client is the real one.
type Keys interface {
	LangSmithKey(ctx context.Context) (vault.LangSmithKey, int, error)
	PutLangSmithKey(ctx context.Context, k vault.LangSmithKey, version int) (int, error)
	DeleteLangSmithKey(ctx context.Context) error
}

// Service keeps the setting and, while LangSmith is on, the exporter that sends spans there.
type Service struct {
	db       *db.Cluster
	keys     Keys
	endpoint string

	mu     sync.Mutex
	cached *destination
}

// NewService returns the LangSmith setting, which sends to endpoint once turned on.
func NewService(cluster *db.Cluster, keys Keys, endpoint string) *Service {
	return &Service{db: cluster, keys: keys, endpoint: endpoint}
}

// Get is the setting, for an administrator; the database's policy shows it to nobody else.
func (s *Service) Get(ctx context.Context) (Setting, error) {
	var out Setting
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT l.enabled, l.project, l.key_set_at, (SELECT u.name FROM app_user u WHERE u.id = l.changed_by),
			l.updated_at FROM langsmith_setting l`).Scan(&out.Enabled, &out.Project, &out.KeySetAt, &out.ChangedBy, &out.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Setting{}, auth.ErrNoSession
	}
	return out, err
}

// Set changes the setting as the administrator on the context: a new key goes to OpenBao first, and turning it on
// needs a key and the acknowledgement.
func (s *Service) Set(ctx context.Context, in Input) (Setting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Setting{}, auth.ErrNoSession
	}
	in.Project = strings.TrimSpace(in.Project)
	if in.Project == "" {
		in.Project = config.DefaultLangSmithProject
	}
	if len(in.Project) > config.MaxLangSmithProjectChars {
		return Setting{}, ErrBadInput
	}
	if in.Enabled && !in.AcknowledgeDataLeaves {
		return Setting{}, ErrNotAcknowledged
	}
	current, err := s.Get(ctx)
	if err != nil {
		return Setting{}, err
	}
	keySetAt, keyChange := current.KeySetAt, "kept"
	if in.Key != nil {
		key := strings.TrimSpace(*in.Key)
		if key == "" || len(key) > config.MaxLangSmithKeyChars || strings.ContainsAny(key, " \t\r\n") {
			return Setting{}, ErrBadInput
		}
		_, version, err := s.keys.LangSmithKey(ctx)
		if err != nil && !errors.Is(err, vault.ErrNotFound) {
			return Setting{}, err
		}
		now := time.Now().UTC()
		if _, err := s.keys.PutLangSmithKey(ctx, vault.LangSmithKey{Key: key, SetAt: now}, version); err != nil {
			return Setting{}, err
		}
		keySetAt, keyChange = &now, "set"
	}
	if in.Enabled && keySetAt == nil {
		return Setting{}, ErrNoKey
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE langsmith_setting SET enabled = $1, project = $2, key_set_at = $3, changed_by = $4`,
			in.Enabled, in.Project, keySetAt, p.UserID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionLangSmithChanged, TargetType: "langsmith", Actor: p.UserID,
			Data: map[string]any{"enabled": in.Enabled, "project": in.Project, "key": keyChange, "acknowledged": in.AcknowledgeDataLeaves}})
	})
	if err != nil {
		return Setting{}, err
	}
	s.forget()
	return s.Get(ctx)
}

// RemoveKey forgets the key and turns LangSmith off.
func (s *Service) RemoveKey(ctx context.Context) (Setting, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Setting{}, auth.ErrNoSession
	}
	if _, err := s.Get(ctx); err != nil {
		return Setting{}, err
	}
	if err := s.keys.DeleteLangSmithKey(ctx); err != nil {
		return Setting{}, err
	}
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE langsmith_setting SET enabled = false, key_set_at = NULL, changed_by = $1`, p.UserID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionLangSmithChanged, TargetType: "langsmith", Actor: p.UserID,
			Data: map[string]any{"enabled": false, "key": "removed"}})
	})
	if err != nil {
		return Setting{}, err
	}
	s.forget()
	return s.Get(ctx)
}
