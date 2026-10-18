package oidc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Service runs the sign-in flow against the one provider the environment names.
type Service struct {
	db       *db.Cluster
	sessions *auth.Service
	cfg      config.OIDC
	http     *http.Client

	// discovery is fetched on first use, so the api starts while the provider is still coming up.
	mu       sync.Mutex
	provider *coreoidc.Provider
}

// NewService returns the sign-in flow; with no issuer configured every call answers ErrNotConfigured.
func NewService(cluster *db.Cluster, sessions *auth.Service, cfg config.OIDC) *Service {
	return &Service{db: cluster, sessions: sessions, cfg: cfg, http: HTTPClient(ParseRewrites(cfg.Backchannel))}
}

// discover returns the provider's endpoints and key set, fetching them once it answers.
func (s *Service) discover(ctx context.Context) (*coreoidc.Provider, error) {
	if s.cfg.Issuer == "" {
		return nil, ErrNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil {
		return s.provider, nil
	}
	found, err := coreoidc.NewProvider(s.providerContext(ctx), s.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: ask %s who it is: %w", ErrUnreachable, s.cfg.Issuer, err)
	}
	s.provider = found
	return found, nil
}

func (s *Service) oauthConfig(p *coreoidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID: s.cfg.ClientID, ClientSecret: s.cfg.ClientSecret,
		Endpoint: p.Endpoint(), RedirectURL: s.cfg.RedirectURL, Scopes: scopes,
	}
}

// Start begins a sign-in that lands on next afterwards, and returns the URL to send the browser to.
func (s *Service) Start(ctx context.Context, next string) (string, error) {
	provider, err := s.discover(ctx)
	if err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	// The state ties the callback to this attempt, the nonce the id token, and the PKCE verifier
	// proves whoever redeems the code is who asked for it.
	verifier := oauth2.GenerateVerifier()
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_login WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_login (state_hash, nonce, code_verifier, next, expires_at) VALUES ($1, $2, $3, $4, $5)`,
			auth.Hash(state), nonce, verifier, next, time.Now().Add(config.OIDCLoginTTL))
		return err
	})
	if err != nil {
		return "", fmt.Errorf("remember this sign-in: %w", err)
	}
	return s.oauthConfig(provider).AuthCodeURL(state, coreoidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Exchange trades the code for a verified id token, and returns its identity and where the browser was headed.
func (s *Service) Exchange(ctx context.Context, state, code string) (*Identity, string, error) {
	var nonce, verifier, next string
	// Consumed as it is read: a state is good for exactly one sign-in, so a replayed callback opens nothing.
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM oidc_login WHERE state_hash = $1 AND expires_at > now() RETURNING nonce, code_verifier, next`,
			auth.Hash(state)).Scan(&nonce, &verifier, &next)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownLogin
		}
		return err
	})
	if err != nil {
		return nil, "", err
	}
	provider, err := s.discover(ctx)
	if err != nil {
		return nil, "", err
	}
	ctx = s.providerContext(ctx)
	tokens, err := s.oauthConfig(provider).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", fmt.Errorf("exchange the code: %w", err)
	}
	raw, ok := tokens.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, "", errors.New("the identity provider returned no id token")
	}
	// Verified against the issuer's published keys, for this client, unexpired.
	verified, err := provider.Verifier(&coreoidc.Config{ClientID: s.cfg.ClientID}).Verify(ctx, raw)
	if err != nil {
		return nil, "", fmt.Errorf("verify the id token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(nonce)) != 1 {
		return nil, "", errors.New("the id token belongs to a different sign-in")
	}
	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return nil, "", fmt.Errorf("read the token's claims: %w", err)
	}
	identity, err := identityFromClaims(verified.Issuer, verified.Subject, claims)
	if err != nil {
		return nil, "", err
	}
	return identity, next, nil
}

// randomToken is randomTokenBytes of randomness, URL safe.
func randomToken() (string, error) {
	buf := make([]byte, randomTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
