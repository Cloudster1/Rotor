package oidc

// Setting single sign-on up from Admin's Access page (#204), as Armature's Service.Save does, with the secret in
// OpenBao rather than in a column.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Provider is single sign-on as an administrator sees it; the client secret is never part of it, only whether one is held.
type Provider struct {
	Issuer       string
	ClientID     string
	GroupsClaim  string
	AdmitByGroup bool
	Enabled      bool
	HasSecret    bool
	ChangedBy    *string
	UpdatedAt    time.Time

	secretSetAt *time.Time
}

// ProviderInput changes single sign-on; a blank ClientSecret keeps the one held.
type ProviderInput struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	GroupsClaim  string
	AdmitByGroup bool
	Enabled      bool
}

// ProviderError is a provider that could not work, with a sentence for each field to fix.
type ProviderError struct {
	Message string
	Fields  map[string]string
}

func (e *ProviderError) Error() string { return e.Message }

var (
	// ErrOnlyWayIn refuses turning single sign-on off while it is the only way into Rotor.
	ErrOnlyWayIn = errors.New("single sign-on is the only way into Rotor, so turning it off would lock everybody out")
	// ErrNoSecretStore is a new client secret on an installation without OpenBao to keep it in.
	ErrNoSecretStore = errors.New("there is no OpenBao to keep a client secret in")
)

// CanTurnOff says whether single sign-on may be turned off: never while Rotor has no other way in, and people sign in
// through the provider alone (docs/decisions.md, sign-in for M0).
func (s *Service) CanTurnOff() bool { return false }

// RedirectURL is where the provider sends people back, which its client must allow.
func (s *Service) RedirectURL() string { return s.cfg.RedirectURL }

// Provider is single sign-on as the database keeps it, read as the administrator on the context; nil when there is none.
func (s *Service) Provider(ctx context.Context) (*Provider, error) {
	var p Provider
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT issuer, client_id, groups_claim, admit_by_group, enabled, secret_set_at,
			(SELECT u.name FROM app_user u WHERE u.id = changed_by), updated_at FROM oidc_provider`).
			Scan(&p.Issuer, &p.ClientID, &p.GroupsClaim, &p.AdmitByGroup, &p.Enabled, &p.secretSetAt, &p.ChangedBy, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.HasSecret = p.secretSetAt != nil || s.envSecretFits(p.Issuer, p.ClientID)
	return &p, nil
}

// Save sets single sign-on up or changes it, as the administrator on the context, and applies from the next sign-in.
// A provider that could not work is refused: no issuer, no client, no secret, or an issuer that does not answer.
func (s *Service) Save(ctx context.Context, in ProviderInput) (*Provider, error) {
	actor := auth.PrincipalFrom(ctx)
	if actor == nil {
		return nil, auth.ErrNoSession
	}
	in.Issuer = strings.TrimSuffix(strings.TrimSpace(in.Issuer), "/")
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.GroupsClaim = strings.TrimSpace(in.GroupsClaim); in.GroupsClaim == "" {
		in.GroupsClaim = config.DefaultGroupsClaim
	}
	if strings.TrimSpace(in.ClientSecret) == "" {
		in.ClientSecret = ""
	}
	if err := checkProvider(in); err != nil {
		return nil, err
	}
	if !in.Enabled && !s.CanTurnOff() {
		return nil, ErrOnlyWayIn
	}
	held, err := s.Provider(ctx)
	if err != nil {
		return nil, err
	}
	secret, secretSetAt, change, err := s.secretFor(in, held)
	if err != nil {
		return nil, err
	}
	if err := s.probe(ctx, in.Issuer); err != nil {
		return nil, err
	}
	if secret != "" {
		if secretSetAt, err = s.keepSecret(ctx, secret); err != nil {
			return nil, err
		}
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO oidc_provider (issuer, client_id, groups_claim, admit_by_group, enabled, secret_set_at, changed_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (one_row) DO UPDATE SET issuer = EXCLUDED.issuer, client_id = EXCLUDED.client_id,
				groups_claim = EXCLUDED.groups_claim, admit_by_group = EXCLUDED.admit_by_group, enabled = EXCLUDED.enabled,
				secret_set_at = EXCLUDED.secret_set_at, changed_by = EXCLUDED.changed_by`,
			in.Issuer, in.ClientID, in.GroupsClaim, in.AdmitByGroup, in.Enabled, secretSetAt, actor.UserID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOIDCSaved, TargetType: "oidc_provider", Actor: actor.UserID,
			Data: map[string]any{"issuer": in.Issuer, "clientId": in.ClientID, "groupsClaim": in.GroupsClaim,
				"admitByGroup": in.AdmitByGroup, "enabled": in.Enabled, "secret": change}})
	})
	if err != nil {
		return nil, err
	}
	// This replica forgets at once; the others see the newer row at their next sign-in.
	s.forget()
	return s.Provider(ctx)
}

// checkProvider refuses an issuer or a client that could not work, naming each.
func checkProvider(in ProviderInput) error {
	fields := map[string]string{}
	if in.Issuer == "" {
		fields["issuer"] = "Give the issuer, the address of the provider's realm, such as https://id.example.com/realms/dynamo."
	} else if u, err := url.Parse(in.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		fields["issuer"] = "The issuer is an address starting with https://, such as https://id.example.com/realms/dynamo."
	}
	if in.ClientID == "" {
		fields["clientId"] = "Give the ID of Rotor's client at the provider, such as rotor."
	}
	if len(fields) == 0 {
		return nil
	}
	return &ProviderError{Message: "Single sign-on needs an issuer and a client ID to work. Fill them in, then save again.", Fields: fields}
}

// secretFor decides what the saved provider signs in with: a new secret to keep, the one OpenBao holds, or the
// environment's while it still fits. A secret answered here goes to OpenBao before the row is written.
func (s *Service) secretFor(in ProviderInput, held *Provider) (string, *time.Time, string, error) {
	switch {
	case in.ClientSecret != "":
		return in.ClientSecret, nil, "set", nil
	case held != nil && held.secretSetAt != nil:
		return "", held.secretSetAt, "kept", nil
	case s.envSecretFits(in.Issuer, in.ClientID):
		return "", nil, "kept", nil
	case held != nil && s.envSecretFits(held.Issuer, held.ClientID):
		// The issuer or client changes but the secret is kept, so the environment's goes to OpenBao, where it outlives it.
		return s.cfg.ClientSecret, nil, "kept", nil
	}
	return "", nil, "", &ProviderError{Message: "Single sign-on needs the client's secret to work. Paste it, then save again.",
		Fields: map[string]string{"clientSecret": "Paste the secret the provider issued for this client."}}
}

// probe reads the issuer's discovery document, so a typo is refused now rather than locking everybody out at sign-in.
func (s *Service) probe(ctx context.Context, issuer string) error {
	ctx, cancel := context.WithTimeout(ctx, config.ProviderTimeout)
	defer cancel()
	if _, err := coreoidc.NewProvider(s.providerContext(ctx), issuer); err != nil {
		return &ProviderError{Message: "Rotor could not reach the identity provider at that issuer, so nobody could sign in through it. " +
			"Check the issuer, then save again.",
			Fields: map[string]string{"issuer": fmt.Sprintf("Rotor could not read %s/.well-known/openid-configuration.", issuer)}}
	}
	return nil
}

// keepSecret puts the client secret in OpenBao over whatever version is there, and says when.
func (s *Service) keepSecret(ctx context.Context, secret string) (*time.Time, error) {
	if s.Secrets == nil {
		return nil, ErrNoSecretStore
	}
	_, version, err := s.Secrets.OIDCClientSecret(ctx)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err := s.Secrets.PutOIDCClientSecret(ctx, vault.OIDCClientSecret{Secret: secret, SetAt: now}, version); err != nil {
		return nil, fmt.Errorf("keep the client secret in OpenBao: %w", err)
	}
	return &now, nil
}
