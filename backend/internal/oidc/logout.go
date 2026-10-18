package oidc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// BackchannelLogout verifies a logout token and ends the sessions it names, once per token.
func (s *Service) BackchannelLogout(ctx context.Context, raw string) error {
	logout, err := s.VerifyLogoutToken(ctx, raw, time.Now())
	if err != nil {
		return err
	}
	return s.EndSessions(ctx, logout)
}

// EndSessions ends the provider's session the logout names, or every session of the person when it names no session.
func (s *Service) EndSessions(ctx context.Context, logout *Logout) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_logout_jti WHERE expires_at < now()`); err != nil {
			return err
		}
		seen, err := tx.Exec(ctx, `INSERT INTO oidc_logout_jti (jti, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			logout.JTI, logout.Expires)
		if err != nil {
			return err
		}
		if seen.RowsAffected() == 0 {
			return fmt.Errorf("%w: it was already used", ErrBadLogoutToken)
		}
		// A named session ends alone, so leaving one browser keeps the others; only a token naming nobody
		// but the person ends all of theirs, as section 2.7 of the specification says.
		rows, err := tx.Query(ctx, `
			DELETE FROM user_session
			WHERE CASE WHEN $1 <> '' THEN oidc_sid = $1
			      ELSE user_id IN (SELECT user_id FROM user_identity WHERE issuer = $2 AND subject = $3) END
			RETURNING user_id::text`, logout.SID, logout.Issuer, logout.Subject)
		if err != nil {
			return err
		}
		ended := map[string]int{}
		for rows.Next() {
			var user string
			if err := rows.Scan(&user); err != nil {
				rows.Close()
				return err
			}
			ended[user]++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		slog.InfoContext(ctx, "the identity provider ended sessions", "sid", logout.SID, "by_person", logout.SID == "", "people", len(ended))
		for user, n := range ended {
			if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSessionEndedByProvider, TargetType: "user", TargetID: user,
				Data: map[string]any{"sessions": n, "sid": logout.SID}}); err != nil {
				return err
			}
		}
		return nil
	})
}
