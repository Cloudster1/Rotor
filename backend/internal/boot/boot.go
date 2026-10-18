// Package boot builds what more than one binary starts with, such as the OpenBao client, so the api and the worker
// open them the same way.
package boot

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Secrets builds the OpenBao client and keeps it signed in until ctx ends; nil without OpenBao configured.
func Secrets(ctx context.Context, cfg config.Config, log *slog.Logger) (*vault.Client, error) {
	if !cfg.SecretsConfigured() {
		log.Warn("ROTOR_OPENBAO_ADDR is not set, so nobody can connect a tool; set it to OpenBao's address")
		return nil, nil
	}
	client, err := vault.New(vault.Config{
		OpenBao: cfg.OpenBao, Issuer: cfg.OIDC.Issuer, Log: log,
		HTTP: oidc.HTTPClient(oidc.ParseRewrites(cfg.OIDC.Backchannel)),
	})
	if err != nil {
		return nil, err
	}
	go client.Run(ctx)
	return client, nil
}

// ToolClient reaches the tools an administrator adds, at the addresses Rotor can reach them.
func ToolClient(cfg config.Config) *http.Client {
	return &http.Client{Timeout: config.ToolTimeout, Transport: oidc.HTTPClient(oidc.ParseRewrites(cfg.Tools.Backchannel)).Transport}
}
