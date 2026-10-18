package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// logoutClaims are the claims of OpenID Connect Back-Channel Logout 1.0 a logout token carries.
type logoutClaims struct {
	IssuedAt int64                      `json:"iat"`
	Expires  int64                      `json:"exp"`
	JTI      string                     `json:"jti"`
	SID      string                     `json:"sid"`
	Events   map[string]json.RawMessage `json:"events"`
	Nonce    *json.RawMessage           `json:"nonce"`
}

// VerifyLogoutToken checks a logout token the way section 2.6 of the specification asks, against now,
// and says which sessions it ends. Replay is caught when the sessions are ended (logout.go).
func (s *Service) VerifyLogoutToken(ctx context.Context, raw string, now time.Time) (*Logout, error) {
	provider, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}
	// Signed by the issuer's published keys, for this client. Expiry is checked below, where a missing exp is allowed.
	verifier := provider.Verifier(&coreoidc.Config{ClientID: s.cfg.ClientID, SkipExpiryCheck: true})
	token, err := verifier.Verify(s.providerContext(ctx), raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadLogoutToken, err)
	}
	var claims logoutClaims
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: read its claims: %w", ErrBadLogoutToken, err)
	}
	issued := time.Unix(claims.IssuedAt, 0)
	switch {
	case claims.IssuedAt == 0 || issued.After(now.Add(config.LogoutTokenLeeway)) || issued.Before(now.Add(-config.LogoutTokenLeeway)):
		return nil, fmt.Errorf("%w: it was not issued just now", ErrBadLogoutToken)
	case claims.Expires != 0 && time.Unix(claims.Expires, 0).Before(now):
		return nil, fmt.Errorf("%w: it has expired", ErrBadLogoutToken)
	case claims.Events[backchannelLogoutEvent] == nil:
		return nil, fmt.Errorf("%w: it is not a logout event", ErrBadLogoutToken)
	case claims.Nonce != nil:
		return nil, fmt.Errorf("%w: it carries a nonce, as an id token would", ErrBadLogoutToken)
	case claims.SID == "" && token.Subject == "":
		return nil, fmt.Errorf("%w: it names neither a session nor a person", ErrBadLogoutToken)
	case claims.JTI == "":
		return nil, fmt.Errorf("%w: it has no jti to tell a replay by", ErrBadLogoutToken)
	}
	expires := issued.Add(config.LogoutTokenLeeway)
	if claims.Expires != 0 {
		expires = time.Unix(claims.Expires, 0)
	}
	return &Logout{Issuer: token.Issuer, Subject: token.Subject, SID: claims.SID, JTI: claims.JTI, Expires: expires}, nil
}
