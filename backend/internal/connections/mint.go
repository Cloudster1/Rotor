package connections

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// MintRequest is what a run needs at a tool, as whom.
type MintRequest struct {
	UserID, ToolID uuid.UUID
	// Scopes are what the run needs, within the person's consent; none asks for all of it.
	Scopes []string
	// RunID names the run in the audit log.
	RunID string
}

// Token is a run's own access token: never stored, never logged, never shared with another run.
type Token struct {
	Value     string
	ExpiresAt time.Time
	// Scopes are what the token carries at the tool.
	Scopes []string
	// NarrowedAtTool is false when the token carries more than the run asked for, which the runner then enforces.
	NarrowedAtTool bool
}

// ScopeError is a run asking for more than the person consented to.
type ScopeError struct{ Missing []string }

func (e *ScopeError) Error() string {
	return "the person did not consent to " + strings.Join(e.Missing, " ")
}

// ErrScopeNotConsented matches a *ScopeError with errors.Is.
var ErrScopeNotConsented = errors.New("scope not consented")

func (e *ScopeError) Is(target error) bool { return target == ErrScopeNotConsented }

// Mint gives a run a fresh access token for a person at a tool. Only one refresh runs per connection at a time, and
// the rotated refresh token is in OpenBao before the access token is handed out, so the tool never sees a token twice.
func (s *Service) Mint(ctx context.Context, req MintRequest) (Token, error) {
	consented, status, err := s.consent(ctx, req.UserID, req.ToolID)
	if err != nil {
		return Token{}, err
	}
	if status == StatusReconnect {
		return Token{}, ErrReconnect
	}
	tool, err := s.tools.Lookup(ctx, req.ToolID)
	if err != nil {
		return Token{}, err
	}
	if tool.Status == tools.StatusNeedsRegistration {
		return Token{}, ErrNeedsRegistration
	}
	asked := req.Scopes
	if len(asked) == 0 {
		asked = consented
	}
	if missing := tool.Profile().Uncovered(consented, asked); len(missing) > 0 {
		return Token{}, &ScopeError{Missing: missing}
	}
	client, err := s.tools.Client(ctx, req.ToolID)
	if err != nil {
		return Token{}, err
	}
	for range config.MintAttempts {
		token, err := s.mintOnce(ctx, req, tool, client, asked, consented)
		if errors.Is(err, vault.ErrConflict) {
			// The person connected again meanwhile; the token just obtained belongs to the grant that ended.
			continue
		}
		return token, err
	}
	return Token{}, fmt.Errorf("the connection to %s kept changing while minting a token; try again", tool.Name)
}

func (s *Service) consent(ctx context.Context, userID, toolID uuid.UUID) ([]string, string, error) {
	var scopes []string
	var status string
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT scopes, status FROM connection WHERE user_id = $1 AND tool_id = $2`, userID, toolID).Scan(&scopes, &status)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotConnected
	}
	return scopes, status, err
}

// mintOnce refreshes under the connection's lock. A failure that ends the connection is recorded in the same
// transaction, which therefore commits; the error goes to the caller beside it.
func (s *Service) mintOnce(ctx context.Context, req MintRequest, tool tools.Tool, client vault.ToolClient, asked, consented []string) (Token, error) {
	var token Token
	var outcome error
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			"connection:"+req.UserID.String()+":"+req.ToolID.String()); err != nil {
			return err
		}
		secret, version, err := s.vault.Connection(ctx, req.UserID, req.ToolID)
		if errors.Is(err, vault.ErrNotFound) {
			outcome = ErrReconnect
			return s.markReconnect(ctx, tx, req.UserID, tool)
		}
		if err != nil {
			return err
		}
		granted, err := s.refresh(ctx, tool, client, secret.RefreshToken)
		var refused *oauthError
		switch {
		case errors.As(err, &refused) && refused.Code == codeInvalidGrant:
			outcome = ErrReconnect
			return s.markReconnect(ctx, tx, req.UserID, tool)
		case errors.As(err, &refused) && refused.Code == codeInvalidClient:
			outcome = ErrNeedsRegistration
			_, err := tx.Exec(ctx, `UPDATE tool SET status = $2 WHERE id = $1`, tool.ID, tools.StatusNeedsRegistration)
			return err
		case err != nil:
			return err
		}
		secret.RefreshToken, secret.ObtainedAt = granted.Refresh, time.Now().UTC()
		if _, err := s.vault.PutConnection(ctx, req.UserID, req.ToolID, secret, version); err != nil {
			return err
		}
		carried := consented
		if fields := strings.Fields(granted.Scope); len(fields) > 0 {
			carried = fields
		}
		if tool.OffersExchange() {
			// The grant's own token stays here: a refresh would replace it under another run, and revoking it ends the grant.
			wanted := tool.Profile().WithImplied(asked)
			exchanged, err := s.exchangeToken(ctx, tool, client, granted.Access, wanted)
			var refused *oauthError
			switch {
			case errors.As(err, &refused) && refused.Code == codeInvalidScope:
				outcome = &ScopeError{Missing: wanted}
				return nil
			case errors.As(err, &refused) && refused.Code == codeUnauthorizedClient:
				outcome = ErrNeedsRegistration
				_, err := tx.Exec(ctx, `UPDATE tool SET status = $2 WHERE id = $1`, tool.ID, tools.StatusNeedsRegistration)
				return err
			case err != nil:
				return err
			}
			granted.Access, granted.Expiry, carried, asked = exchanged.Access, exchanged.Expiry, wanted, wanted
			if fields := strings.Fields(exchanged.Scope); len(fields) > 0 {
				carried = fields
			}
		}
		token = Token{Value: granted.Access, ExpiresAt: granted.Expiry, Scopes: carried, NarrowedAtTool: sameScopes(asked, carried)}
		if _, err := tx.Exec(ctx, `UPDATE connection SET last_used_at = now(), last_refreshed_at = now() WHERE user_id = $1 AND tool_id = $2`,
			req.UserID, req.ToolID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTokenMinted, TargetType: "tool", TargetID: tool.ID.String(), Actor: req.UserID,
			Data: map[string]any{"tool": tool.Name, "scopes_asked": asked, "scopes_carried": carried, "run": req.RunID,
				"expires_at": granted.Expiry, "narrowed_at_tool": token.NarrowedAtTool}})
	})
	if err != nil {
		return Token{}, err
	}
	if outcome != nil {
		return Token{}, outcome
	}
	return token, nil
}

// sameScopes says whether the token carries exactly what the run asked for, so the runner has nothing to narrow.
func sameScopes(asked, carried []string) bool {
	a, c := slices.Clone(asked), slices.Clone(carried)
	slices.Sort(a)
	slices.Sort(c)
	return slices.Equal(slices.Compact(a), slices.Compact(c))
}

// markReconnect records, in the minting transaction, that the tool ended the grant; the person connects again.
func (s *Service) markReconnect(ctx context.Context, tx db.DBTX, userID uuid.UUID, tool tools.Tool) error {
	if err := s.vault.DeleteConnection(context.WithoutCancel(ctx), userID, tool.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE connection SET status = 'reconnect' WHERE user_id = $1 AND tool_id = $2`, userID, tool.ID); err != nil {
		return err
	}
	return audit.Write(ctx, tx, ended(uuid.Nil, userID, tool, EndedByTool, false))
}

// Revoke ends one run's access token at the tool, leaving the connection and every other run's token alone.
func (s *Service) Revoke(ctx context.Context, toolID uuid.UUID, token string) error {
	tool, err := s.tools.Lookup(ctx, toolID)
	if err != nil {
		return err
	}
	client, err := s.tools.Client(ctx, toolID)
	if err != nil {
		return err
	}
	return s.revoke(ctx, tool, client, token, "access_token")
}
