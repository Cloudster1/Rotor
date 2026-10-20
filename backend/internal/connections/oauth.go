package connections

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// OAuth error codes Rotor acts on (RFC 6749, section 5.2).
const (
	codeInvalidGrant  = "invalid_grant"
	codeInvalidClient = "invalid_client"
	codeAccessDenied  = "access_denied"
	codeInvalidScope  = "invalid_scope"
	// codeUnauthorizedClient is a client not registered for the exchange, which registering again asks for.
	codeUnauthorizedClient = "unauthorized_client"
)

// tokens is what a token endpoint answers; the access token is never stored.
type tokens struct {
	Access, Refresh, Scope string
	Expiry                 time.Time
}

// oauthError is a token endpoint refusing, with the code Rotor acts on.
type oauthError struct {
	Code, Description string
}

func (e *oauthError) Error() string {
	if e.Description != "" {
		return e.Code + ": " + e.Description
	}
	return e.Code
}

// oauthConfig is Rotor's client at a tool, authenticating as the tool asked at registration.
func oauthConfig(tool tools.Tool, client vault.ToolClient, redirectURL string) *oauth2.Config {
	style := oauth2.AuthStyleInHeader
	if client.AuthMethod == "client_secret_post" {
		style = oauth2.AuthStyleInParams
	}
	return &oauth2.Config{
		ClientID: client.ClientID, ClientSecret: client.ClientSecret, RedirectURL: redirectURL,
		Endpoint: oauth2.Endpoint{AuthURL: tool.AuthorizationEndpoint, TokenURL: tool.TokenEndpoint, AuthStyle: style},
		Scopes:   tool.RequestedScopes(),
	}
}

// classify turns a failed token call into what Rotor does about it: reconnect, register again, or retry later.
func classify(err error) error {
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		if retrieve.Response != nil && retrieve.Response.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%w: the token endpoint answered %d", ErrUnreachable, retrieve.Response.StatusCode)
		}
		return &oauthError{Code: retrieve.ErrorCode, Description: retrieve.ErrorDescription}
	}
	return fmt.Errorf("%w: %w", ErrUnreachable, err)
}

func (s *Service) oauthContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, s.http)
}

// exchange redeems an authorization code with the verifier it was started with.
func (s *Service) exchange(ctx context.Context, tool tools.Tool, client vault.ToolClient, code, verifier string) (tokens, error) {
	t, err := oauthConfig(tool, client, s.redirectURL).Exchange(s.oauthContext(ctx), code,
		oauth2.VerifierOption(verifier), oauth2.SetAuthURLParam("resource", tool.Resource))
	if err != nil {
		return tokens{}, classify(err)
	}
	return fromOAuth(t), nil
}

// refresh trades a refresh token for a new pair. It never sends scope: at Exciter a narrower one narrows the grant.
func (s *Service) refresh(ctx context.Context, tool tools.Tool, client vault.ToolClient, refreshToken string) (tokens, error) {
	cfg := oauthConfig(tool, client, s.redirectURL)
	cfg.Scopes = nil
	t, err := cfg.TokenSource(s.oauthContext(ctx), &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Unix(1, 0)}).Token()
	if err != nil {
		return tokens{}, classify(err)
	}
	out := fromOAuth(t)
	if out.Refresh == refreshToken || out.Refresh == "" {
		// A tool that did not rotate still answered; the token stays the one Rotor holds.
		out.Refresh = refreshToken
	}
	return out, nil
}

func fromOAuth(t *oauth2.Token) tokens {
	scope, _ := t.Extra("scope").(string)
	return tokens{Access: t.AccessToken, Refresh: t.RefreshToken, Scope: scope, Expiry: t.Expiry}
}

// revoke ends a token at the tool (RFC 7009); a tool without a revocation endpoint cannot be told.
func (s *Service) revoke(ctx context.Context, tool tools.Tool, client vault.ToolClient, token, hint string) error {
	if tool.RevocationEndpoint == "" {
		return fmt.Errorf("%s offers no revocation endpoint", tool.Name)
	}
	form := url.Values{"token": {token}, "token_type_hint": {hint}}
	if client.AuthMethod == "client_secret_post" {
		form.Set("client_id", client.ClientID)
		form.Set("client_secret", client.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tool.RevocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if client.AuthMethod != "client_secret_post" {
		// RFC 6749 section 2.3.1: the id and secret are form-encoded before they go into the header.
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, config.MaxToolDocumentBytes))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered the revocation with %d", tool.Name, resp.StatusCode)
	}
	return nil
}
