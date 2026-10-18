package vault

import (
	"context"
	"fmt"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	openbao "github.com/openbao/openbao/api/v2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// ensure logs in unless a token is held; Run renews it from then on.
func (c *Client) ensure(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loggedIn {
		return nil
	}
	_, err := c.loginLocked(ctx)
	return err
}

// login replaces the token with a fresh one, for Run when renewal has ended.
func (c *Client) login(ctx context.Context) (*openbao.Secret, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

func (c *Client) loginLocked(ctx context.Context) (*openbao.Secret, error) {
	ctx, cancel := context.WithTimeout(ctx, config.VaultTimeout)
	defer cancel()
	jwt, err := c.serviceAccountToken(ctx)
	if err != nil {
		return nil, err
	}
	secret, err := c.api.Logical().WriteWithContext(ctx, "auth/jwt/login", map[string]any{"role": c.cfg.Role, "jwt": jwt})
	if err != nil {
		return nil, fmt.Errorf("%w: OpenBao refused the realm's token for role %s: %w", ErrLogin, c.cfg.Role, err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return nil, fmt.Errorf("%w: OpenBao answered the login without a token", ErrLogin)
	}
	c.api.SetToken(secret.Auth.ClientToken)
	c.loggedIn = true
	c.log.Info("signed in to OpenBao", "role", c.cfg.Role, "ttl", time.Duration(secret.Auth.LeaseDuration)*time.Second)
	return secret, nil
}

// serviceAccountToken is a client-credentials token of the realm client OpenBao's JWT role is bound to.
func (c *Client) serviceAccountToken(ctx context.Context) (string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.cfg.HTTP)
	if c.tokens == nil {
		provider, err := coreoidc.NewProvider(coreoidc.ClientContext(ctx, c.cfg.HTTP), c.cfg.Issuer)
		if err != nil {
			return "", fmt.Errorf("%w: ask %s for its token endpoint: %w", ErrLogin, c.cfg.Issuer, err)
		}
		c.tokens = &clientcredentials.Config{
			ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret, TokenURL: provider.Endpoint().TokenURL,
		}
	}
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: the realm refused client %s: %w", ErrLogin, c.cfg.ClientID, err)
	}
	return token.AccessToken, nil
}

// Run keeps the api signed in to OpenBao until ctx ends: it renews the token while OpenBao allows and logs in
// again when renewal ends, so a token never expires under a request.
func (c *Client) Run(ctx context.Context) {
	for ctx.Err() == nil {
		secret, err := c.login(ctx)
		if err != nil {
			c.log.Warn("cannot sign in to OpenBao, trying again", "error", err, "pause", config.VaultLoginRetry)
			pause(ctx, config.VaultLoginRetry)
			continue
		}
		c.watch(ctx, secret)
	}
}

// watch renews the token until OpenBao stops renewing it, or ctx ends.
func (c *Client) watch(ctx context.Context, secret *openbao.Secret) {
	watcher, err := c.api.NewLifetimeWatcher(&openbao.LifetimeWatcherInput{Secret: secret})
	if err != nil {
		c.log.Warn("cannot renew the OpenBao token", "error", err)
		pause(ctx, config.VaultLoginRetry)
		return
	}
	go watcher.Start()
	defer watcher.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-watcher.DoneCh():
			c.log.Info("the OpenBao token is at the end of its life, signing in again", "error", err)
			return
		case renewal := <-watcher.RenewCh():
			c.log.Debug("renewed the OpenBao token", "ttl", time.Duration(renewal.Secret.Auth.LeaseDuration)*time.Second)
		}
	}
}

func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
