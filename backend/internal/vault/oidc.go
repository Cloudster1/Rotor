package vault

import (
	"context"
	"time"
)

// OIDCClientSecret is the secret Rotor signs in to its identity provider with, which an administrator gave (#204).
type OIDCClientSecret struct {
	Secret string    `json:"secret"`
	SetAt  time.Time `json:"set_at"`
}

const oidcSecretPath = "settings/oidc/client-secret"

// OIDCClientSecret reads the identity provider's client secret and its version.
func (c *Client) OIDCClientSecret(ctx context.Context) (OIDCClientSecret, int, error) {
	var s OIDCClientSecret
	version, err := c.get(ctx, oidcSecretPath, &s)
	return s, version, err
}

// PutOIDCClientSecret writes the client secret over the version read; 0 writes the first one.
func (c *Client) PutOIDCClientSecret(ctx context.Context, s OIDCClientSecret, version int) (int, error) {
	return c.put(ctx, oidcSecretPath, s, version)
}
