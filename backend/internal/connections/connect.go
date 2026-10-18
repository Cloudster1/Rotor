package connections

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Codes the callback names on its redirect, for the Connections page to put in words.
const (
	CodeDenied      = "denied"
	CodeExpired     = "expired"
	CodeMismatch    = "mismatch"
	CodeTool        = "tool"
	CodeUnavailable = "unavailable"
)

// CallbackError is a connection that was not made, with the code the page shows.
type CallbackError struct {
	Code  string
	cause error
}

func (e *CallbackError) Error() string { return e.Code + ": " + e.cause.Error() }
func (e *CallbackError) Unwrap() error { return e.cause }

func failed(code string, err error) error { return &CallbackError{Code: code, cause: err} }

// Callback is what a tool sends the browser back with.
type Callback struct {
	State, Code, Issuer, Error, ErrorDescription string
}

func randomState() (string, error) {
	b := make([]byte, config.StateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Start begins connecting the signed-in person to a tool and returns where the browser goes to consent.
func (s *Service) Start(ctx context.Context, toolID uuid.UUID) (string, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return "", ErrNoSession
	}
	tool, err := s.tools.Lookup(ctx, toolID)
	if err != nil {
		return "", err
	}
	if tool.Status == tools.StatusNeedsRegistration {
		return "", ErrNeedsRegistration
	}
	client, err := s.tools.Client(ctx, toolID)
	if err != nil {
		return "", fmt.Errorf("read Rotor's client at %s: %w", tool.Name, err)
	}
	state, err := randomState()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM connection_login WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO connection_login (state_hash, session_id, user_id, tool_id, code_verifier, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)`, auth.Hash(state), p.SessionID, p.UserID, toolID, verifier, time.Now().Add(config.ConnectionLoginTTL))
		return err
	})
	if err != nil {
		return "", err
	}
	return oauthConfig(tool, client, s.redirectURL).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", tool.Resource)), nil
}

// Finish completes a connection from the tool's answer: the refresh token goes to OpenBao, the access token is
// dropped. It returns the tool, so the page can say which one is connected.
func (s *Service) Finish(ctx context.Context, cb Callback) (uuid.UUID, error) {
	p := auth.PrincipalFrom(ctx)
	if cb.State == "" {
		return uuid.Nil, failed(CodeExpired, errors.New("the tool sent no state"))
	}
	// The state is used up whatever happens next, so an answer can never be replayed.
	var sessionID, userID, toolID uuid.UUID
	var verifier string
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `DELETE FROM connection_login WHERE state_hash = $1 AND expires_at > now()
			RETURNING session_id, user_id, tool_id, code_verifier`, auth.Hash(cb.State)).Scan(&sessionID, &userID, &toolID, &verifier)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, failed(CodeExpired, errors.New("unknown, used or expired state"))
	}
	if err != nil {
		return uuid.Nil, err
	}
	if p == nil || p.SessionID != sessionID {
		return toolID, failed(CodeMismatch, errors.New("the state was started by another session"))
	}
	if cb.Error != "" {
		code := CodeTool
		if cb.Error == codeAccessDenied {
			code = CodeDenied
		}
		return toolID, failed(code, &oauthError{Code: cb.Error, Description: cb.ErrorDescription})
	}
	tool, err := s.tools.Lookup(ctx, toolID)
	if err != nil {
		return toolID, failed(CodeUnavailable, err)
	}
	if cb.Issuer != "" && cb.Issuer != tool.Issuer {
		return toolID, failed(CodeMismatch, fmt.Errorf("the answer names issuer %q, not %q", cb.Issuer, tool.Issuer))
	}
	client, err := s.tools.Client(ctx, toolID)
	if err != nil {
		return toolID, failed(CodeUnavailable, err)
	}
	granted, err := s.exchange(ctx, tool, client, cb.Code, verifier)
	if err != nil {
		var refused *oauthError
		if errors.As(err, &refused) {
			if refused.Code == codeInvalidClient {
				_ = s.tools.MarkNeedsRegistration(ctx, toolID)
			}
			return toolID, failed(CodeTool, err)
		}
		return toolID, failed(CodeUnavailable, err)
	}
	if granted.Refresh == "" {
		return toolID, failed(CodeTool, fmt.Errorf("%s granted no refresh token", tool.Name))
	}
	if err := s.keep(ctx, userID, tool, client, granted); err != nil {
		return toolID, failed(CodeUnavailable, err)
	}
	return toolID, nil
}

// keep writes the refresh token over any earlier one and records the connection and its consent.
func (s *Service) keep(ctx context.Context, userID uuid.UUID, tool tools.Tool, client vault.ToolClient, granted tokens) error {
	_, version, err := s.vault.Connection(ctx, userID, tool.ID)
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		return err
	}
	secret := vault.ConnectionSecret{RefreshToken: granted.Refresh, Scope: granted.Scope, ClientID: client.ClientID, ObtainedAt: time.Now().UTC()}
	if _, err := s.vault.PutConnection(ctx, userID, tool.ID, secret, version); err != nil {
		return err
	}
	scopes := strings.Fields(granted.Scope)
	if len(scopes) == 0 {
		// RFC 6749 section 5.1: a token answer without scope grants what was asked.
		scopes = tools.RequestedScopes(tool.ScopesSupported)
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO connection (user_id, tool_id, scopes) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, tool_id) DO UPDATE SET scopes = EXCLUDED.scopes, status = 'connected', connected_at = now(),
				last_refreshed_at = NULL`, userID, tool.ID, scopes); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionConnectionGranted, TargetType: "tool", TargetID: tool.ID.String(),
			Actor: userID, Data: map[string]any{"tool": tool.Name, "scopes": scopes}})
	})
}
