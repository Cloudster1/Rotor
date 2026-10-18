// Package vault keeps Rotor's long-lived secrets in OpenBao: people's refresh tokens and the tools' client
// secrets. Nothing outside it names a path, and nothing it holds reaches a log line.
package vault

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	openbao "github.com/openbao/openbao/api/v2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Errors a caller tells apart.
var (
	// ErrNotFound is a secret OpenBao does not hold.
	ErrNotFound = errors.New("no such secret in OpenBao")
	// ErrConflict is a write whose version is no longer the current one.
	ErrConflict = errors.New("the secret changed in OpenBao since it was read")
	// ErrSealed, ErrUninitialized and ErrUnreachable say why OpenBao cannot serve.
	ErrSealed        = errors.New("OpenBao is sealed")
	ErrUninitialized = errors.New("OpenBao is not initialized")
	ErrUnreachable   = errors.New("OpenBao does not answer")
	// ErrLogin is OpenBao or the identity provider refusing the api's own sign-in.
	ErrLogin = errors.New("Rotor cannot sign in to OpenBao")
)

// Config is how the client reaches OpenBao and signs in to it.
type Config struct {
	config.OpenBao
	// Issuer is the realm whose client-credentials token OpenBao's JWT auth trusts.
	Issuer string
	// HTTP reaches the identity provider, rewriting its public address where the api cannot reach it.
	HTTP *http.Client
	Log  *slog.Logger
}

// Client is an OpenBao session as the control plane; it is safe for concurrent use.
type Client struct {
	cfg Config
	api *openbao.Client
	kv  *openbao.KVv2
	log *slog.Logger

	// mu guards the sign-in: the first call logs in, and Run keeps the token alive after that.
	mu       sync.Mutex
	loggedIn bool
	tokens   *clientcredentials.Config
}

// New builds a client without calling anything, so the api starts while OpenBao is still coming up.
func New(cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		return nil, errors.New("vault: no OpenBao address configured")
	}
	settings := openbao.DefaultConfig()
	settings.Address = cfg.Addr
	settings.Timeout = config.VaultTimeout
	// The client retries on its own otherwise, which would hide an outage from readiness for a minute.
	settings.MaxRetries = 0
	api, err := openbao.NewClient(settings)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	api.ClearToken()
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: config.VaultTimeout}
	}
	return &Client{cfg: cfg, api: api, kv: api.KVv2(cfg.Mount), log: log}, nil
}

// Ready says whether OpenBao can serve: initialized, unsealed and answering.
func (c *Client) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, config.VaultTimeout)
	defer cancel()
	health, err := c.api.Sys().HealthWithContext(ctx)
	switch {
	case err != nil:
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	case !health.Initialized:
		return ErrUninitialized
	case health.Sealed:
		return ErrSealed
	}
	return nil
}
