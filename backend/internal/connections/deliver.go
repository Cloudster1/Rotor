package connections

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Errors a run's token delivery says to its runner.
var (
	// ErrRunNotLive is a run that ended or waits, which gets no token.
	ErrRunNotLive = errors.New("the run is not running, so it gets no token")
	// ErrSwitchedOff is a person whose account was switched off, whose runs act nowhere any more.
	ErrSwitchedOff = errors.New("the person who started the run was switched off, so the run gets no token")
)

// RunSecrets is what delivery keeps in OpenBao; *vault.Client is the real one.
type RunSecrets interface {
	PutRunToken(ctx context.Context, runID, tokenID uuid.UUID, t vault.RunToken) error
	RunToken(ctx context.Context, runID, tokenID uuid.UUID) (vault.RunToken, error)
	DeleteRunToken(ctx context.Context, runID, tokenID uuid.UUID) error
	Wrap(ctx context.Context, data map[string]any, ttl time.Duration) (string, error)
}

// Delivery is a run's token, wrapped so that only the sandbox that unwraps it first can read it.
type Delivery struct {
	WrappingToken string
	ExpiresAt     time.Time
}

// Deliver mints a token for a live run as its person, keeps it in OpenBao until the run ends and answers it
// wrapped for config.WrapTTL. The token is revoked at its tool by RevokeRun.
func (s *Service) Deliver(ctx context.Context, req MintRequest) (Delivery, error) {
	runID, err := uuid.Parse(req.RunID)
	if err != nil {
		return Delivery{}, fmt.Errorf("deliver a token: %w", err)
	}
	if err := s.live(ctx, runID, req.UserID); err != nil {
		return Delivery{}, err
	}
	token, err := s.Mint(ctx, req)
	if err != nil {
		return Delivery{}, err
	}
	tokenID := uuid.Must(uuid.NewV7())
	// The row comes first: a crash before the token reaches OpenBao leaves a row RevokeRun settles, never a
	// token nobody revokes.
	if err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO run_token (id, run_id, tool_id, expires_at) VALUES ($1, $2, $3, $4)`,
			tokenID, runID, req.ToolID, token.ExpiresAt)
		return err
	}); err != nil {
		return Delivery{}, err
	}
	if err := s.Runs.PutRunToken(ctx, runID, tokenID, vault.RunToken{Token: token.Value, ToolID: req.ToolID}); err != nil {
		return Delivery{}, err
	}
	wrapped, err := s.Runs.Wrap(ctx, map[string]any{"access_token": token.Value, "expires_at": token.ExpiresAt.UTC().Format(time.RFC3339)}, config.WrapTTL)
	if err != nil {
		return Delivery{}, err
	}
	return Delivery{WrappingToken: wrapped, ExpiresAt: token.ExpiresAt}, nil
}

// live refuses a token to a run that is not starting or running, and to a person who was switched off.
func (s *Service) live(ctx context.Context, runID, userID uuid.UUID) error {
	var status string
	var active bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT r.status, u.is_active FROM run r JOIN app_user u ON u.id = r.user_id
			WHERE r.id = $1 AND r.user_id = $2`, runID, userID).Scan(&status, &active)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrRunNotLive
	case err != nil:
		return err
	case !active:
		return ErrSwitchedOff
	case status != "starting" && status != "running":
		return ErrRunNotLive
	}
	return nil
}

// RevokeRun revokes at their tools every token the run was handed and not yet revoked, and forgets them. A tool
// that does not answer is tried again on a later call, until the token would have expired anyway.
func (s *Service) RevokeRun(ctx context.Context, runID uuid.UUID) error {
	return s.revokeTokens(ctx, `SELECT id, run_id, tool_id, expires_at FROM run_token WHERE run_id = $1 AND revoked_at IS NULL`, runID)
}

// RevokeFor revokes the tokens a person's runs hold at one tool, when their connection there or their account ends.
func (s *Service) RevokeFor(ctx context.Context, userID, toolID uuid.UUID) error {
	return s.revokeTokens(ctx, `SELECT t.id, t.run_id, t.tool_id, t.expires_at FROM run_token t JOIN run r ON r.id = t.run_id
		WHERE r.user_id = $1 AND t.tool_id = $2 AND t.revoked_at IS NULL`, userID, toolID)
}

type heldToken struct {
	id, runID, toolID uuid.UUID
	expiresAt         time.Time
}

func (s *Service) revokeTokens(ctx context.Context, query string, args ...any) error {
	if s.Runs == nil {
		return nil
	}
	var held []heldToken
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h heldToken
			if err := rows.Scan(&h.id, &h.runID, &h.toolID, &h.expiresAt); err != nil {
				return err
			}
			held = append(held, h)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, h := range held {
		errs = append(errs, s.revokeOne(ctx, h))
	}
	return errors.Join(errs...)
}

// revokeOne revokes one token at its tool and forgets it; one OpenBao no longer holds was never handed out.
func (s *Service) revokeOne(ctx context.Context, h heldToken) error {
	confirmed := false
	t, err := s.Runs.RunToken(ctx, h.runID, h.id)
	switch {
	case errors.Is(err, vault.ErrNotFound):
	case err != nil:
		return err
	default:
		if err := s.Revoke(ctx, h.toolID, t.Token); err != nil {
			if time.Now().Before(h.expiresAt) {
				return fmt.Errorf("revoke a token of run %s: %w", h.runID, err)
			}
			s.log.Warn("the tool did not confirm a revocation; the token has expired anyway", "run", h.runID, "error", err)
		} else {
			confirmed = true
		}
		if err := s.Runs.DeleteRunToken(ctx, h.runID, h.id); err != nil {
			return err
		}
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE run_token SET revoked_at = now() WHERE id = $1`, h.id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTokenRevoked, TargetType: "run", TargetID: h.runID.String(),
			Data: map[string]any{"tool": h.toolID.String(), "confirmed": confirmed}})
	})
}
