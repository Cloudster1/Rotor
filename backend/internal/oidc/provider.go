package oidc

// The identity provider Rotor keeps in its database (#204), as Armature's oidc_provider: read on every use, so a change
// another replica saved applies at the next sign-in here too, while discovery and the secret are held until it does.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Secrets is where the client secret an administrator gives is kept; *vault.Client is the real one.
type Secrets interface {
	OIDCClientSecret(ctx context.Context) (vault.OIDCClientSecret, int, error)
	PutOIDCClientSecret(ctx context.Context, s vault.OIDCClientSecret, version int) (int, error)
}

// ErrNoClientSecret is a provider whose secret is in neither OpenBao nor the environment that seeded it.
var ErrNoClientSecret = errors.New("no client secret is held for the identity provider")

// settings is one version of the provider row, with what was fetched for it; a newer row replaces it whole.
type settings struct {
	issuer       string
	clientID     string
	groupsClaim  string
	admitByGroup bool
	secretSetAt  *time.Time
	updatedAt    time.Time

	mu         sync.Mutex
	discovery  *coreoidc.Provider
	secret     string
	secretRead bool
}

const providerColumns = `issuer, client_id, groups_claim, admit_by_group, enabled, secret_set_at, updated_at`

// current is the provider as the database has it now, or ErrNotConfigured when there is none or it is turned off.
func (s *Service) current(ctx context.Context) (*settings, error) {
	var (
		row     settings
		enabled bool
	)
	if s.db == nil {
		return s.environments()
	}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT `+providerColumns+` FROM oidc_provider`).Scan(&row.issuer, &row.clientID, &row.groupsClaim,
			&row.admitByGroup, &enabled, &row.secretSetAt, &row.updatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !enabled) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held == nil || !s.held.updatedAt.Equal(row.updatedAt) {
		s.held = &settings{issuer: row.issuer, clientID: row.clientID, groupsClaim: row.groupsClaim, admitByGroup: row.admitByGroup,
			secretSetAt: row.secretSetAt, updatedAt: row.updatedAt}
	}
	return s.held, nil
}

// environments is the provider the environment names, for a service without a database, such as one that only checks
// tokens; it is held as the seeded row would be.
func (s *Service) environments() (*settings, error) {
	if s.cfg.Issuer == "" {
		return nil, ErrNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held == nil {
		s.held = &settings{issuer: s.cfg.Issuer, clientID: s.cfg.ClientID, groupsClaim: s.cfg.GroupsClaim}
		if s.held.groupsClaim == "" {
			s.held.groupsClaim = config.DefaultGroupsClaim
		}
	}
	return s.held, nil
}

// forget drops what was fetched for the provider, so the next use reads it all again.
func (s *Service) forget() {
	s.mu.Lock()
	s.held = nil
	s.mu.Unlock()
}

// discover returns the provider and its endpoints and key set, fetched once it answers; the api starts while the
// provider is still coming up.
func (s *Service) discover(ctx context.Context) (*settings, *coreoidc.Provider, error) {
	st, err := s.current(ctx)
	if err != nil {
		return nil, nil, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.discovery == nil {
		found, err := coreoidc.NewProvider(s.providerContext(ctx), st.issuer)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: ask %s who it is: %w", ErrUnreachable, st.issuer, err)
		}
		st.discovery = found
	}
	return st, st.discovery, nil
}

// clientSecret is the secret for this version of the provider: OpenBao's once an administrator gave one, else the
// environment's while the provider is still the one it seeded.
func (s *Service) clientSecret(ctx context.Context, st *settings) (string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.secretRead {
		return st.secret, nil
	}
	switch {
	case st.secretSetAt != nil && s.Secrets != nil:
		held, _, err := s.Secrets.OIDCClientSecret(ctx)
		if err != nil {
			return "", fmt.Errorf("read the identity provider's client secret from OpenBao: %w", err)
		}
		st.secret = held.Secret
	case st.secretSetAt == nil && s.envSecretFits(st.issuer, st.clientID):
		st.secret = s.cfg.ClientSecret
	default:
		return "", ErrNoClientSecret
	}
	st.secretRead = true
	return st.secret, nil
}

// envSecretFits says whether ROTOR_OIDC_CLIENT_SECRET is the secret of this issuer and client, the ones it came with.
func (s *Service) envSecretFits(issuer, clientID string) bool {
	return s.cfg.ClientSecret != "" && issuer == s.cfg.Issuer && clientID == s.cfg.ClientID
}

// Seed writes the provider the environment names when the database has none, so a deployment that sets ROTOR_OIDC_*
// keeps signing people in; once there is one, the database's is used and the environment only says so in the log.
func (s *Service) Seed(ctx context.Context, log *slog.Logger) error {
	if s.cfg.Issuer == "" {
		return nil
	}
	var issuer, clientID string
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO oidc_provider (issuer, client_id, groups_claim) VALUES ($1, $2, $3)
			ON CONFLICT (one_row) DO NOTHING`, s.cfg.Issuer, s.cfg.ClientID, s.cfg.GroupsClaim); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT issuer, client_id FROM oidc_provider`).Scan(&issuer, &clientID)
	})
	if err != nil {
		return fmt.Errorf("seed single sign-on from ROTOR_OIDC_ISSUER: %w", err)
	}
	if issuer != s.cfg.Issuer || clientID != s.cfg.ClientID {
		log.Info("single sign-on is set up on Admin's Access page, so ROTOR_OIDC_ISSUER and ROTOR_OIDC_CLIENT_ID are not used",
			"issuer", issuer, "client", clientID)
	}
	return nil
}

// Configured says whether anybody can sign in: a provider is kept and turned on.
func (s *Service) Configured(ctx context.Context) bool {
	_, err := s.current(ctx)
	return err == nil
}
