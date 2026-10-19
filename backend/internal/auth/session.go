package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// tokenBytes makes a session token unguessable; the cookie carries it in base64url.
const tokenBytes = 32

// Service opens, checks and ends sessions, always as rotor_admin: sessions are nobody's to read through the app role.
type Service struct {
	db  *db.Cluster
	ttl time.Duration
	// OnSwitchOff ends what else a switched-off person holds, such as their connections to the tools.
	OnSwitchOff func(ctx context.Context, userID uuid.UUID) error
}

// NewService returns the session service; ttl is how long a session lasts from sign-in.
func NewService(cluster *db.Cluster, ttl time.Duration) *Service {
	return &Service{db: cluster, ttl: ttl}
}

// Hash is what the database keeps of a token.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// SessionInput is what a sign-in knows about the browser it opens a session for.
type SessionInput struct {
	UserID    uuid.UUID
	OIDCSID   string
	UserAgent string
	IP        string
}

// Open makes a session inside the sign-in's transaction and returns the token for the cookie.
func (s *Service) Open(ctx context.Context, tx db.DBTX, in SessionInput) (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	_, err := tx.Exec(ctx, `
		INSERT INTO user_session (user_id, token_hash, oidc_sid, user_agent, ip, expires_at)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, '')::inet, now() + make_interval(secs => $6))`,
		in.UserID, Hash(token), in.OIDCSID, in.UserAgent, in.IP, s.ttl.Seconds())
	if err != nil {
		return "", fmt.Errorf("open the session: %w", err)
	}
	return token, nil
}

// Authenticate finds the person a token belongs to. A switched-off person is refused, not made anonymous.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, ErrNoSession
	}
	var (
		p      Principal
		active bool
	)
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			UPDATE user_session s SET last_seen_at = now()
			FROM app_user u
			WHERE s.token_hash = $1 AND s.expires_at > now() AND u.id = s.user_id
			RETURNING s.id, u.id, u.email, u.name, u.role, u.groups, u.is_active`, Hash(token),
		).Scan(&p.SessionID, &p.UserID, &p.Email, &p.Name, &p.Role, &p.Groups, &active)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSession
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrSwitchedOff
	}
	return &p, nil
}

// End closes the session a token belongs to; an unknown token is already ended.
func (s *Service) End(ctx context.Context, token string) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var user uuid.UUID
		err := tx.QueryRow(ctx, `DELETE FROM user_session WHERE token_hash = $1 RETURNING user_id`, Hash(token)).Scan(&user)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSignedOut, TargetType: "user", TargetID: user.String(), Actor: user})
	})
}

// SwitchOff stops a person signing in and ends every session they have; actor is who did it, nil for the system.
// OnSwitchOff then ends their connections to the tools; the worker's sweep turns their triggers off (#34).
func (s *Service) SwitchOff(ctx context.Context, actor, userID uuid.UUID) error {
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("switch off %s: no such person", userID)
		}
		ended, err := tx.Exec(ctx, `DELETE FROM user_session WHERE user_id = $1`, userID)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSwitchedOff, TargetType: "user", TargetID: userID.String(),
			Actor: actor, Data: map[string]any{"sessionsEnded": ended.RowsAffected()}})
	})
	if err != nil || s.OnSwitchOff == nil {
		return err
	}
	return s.OnSwitchOff(ctx, userID)
}
