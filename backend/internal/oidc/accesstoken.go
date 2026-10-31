package oidc

// How a service account of the realm is let in at the few routes that take its access token (#168): signed with the
// realm's published keys, by the configured issuer, for Rotor, unexpired, and in the group that administers Rotor.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
)

const (
	// accessTokenType is what Keycloak writes in the typ claim of an access token; an id token says ID.
	accessTokenType = "Bearer"
	// serviceAccountDomain makes a service account an address; the top-level domain .invalid never reaches anyone.
	serviceAccountDomain = "service-account.invalid"
)

// What a realm's access token can be refused for; a Refusal carries one of them with its sentence.
var (
	ErrTokenUnverified = errors.New("the token is not signed with the realm's published keys")
	ErrTokenIssuer     = errors.New("the token was issued by another identity provider")
	ErrTokenAudience   = errors.New("the token is not meant for Rotor")
	ErrTokenExpired    = errors.New("the token has expired or carries no expiry")
	ErrTokenNotAccess  = errors.New("the token is not an access token")
	ErrTokenNoAccount  = errors.New("the token names no account")
	ErrTokenNotAdmin   = errors.New("the token's account is not in " + AdminGroup)
)

// Refusal is a realm's access token Rotor does not take; Message says what is missing and what to do.
type Refusal struct {
	Reason  error
	Message string
}

func (r *Refusal) Error() string { return r.Message }
func (r *Refusal) Unwrap() error { return r.Reason }

func refuse(reason error, format string, args ...any) *Refusal {
	return &Refusal{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// accessToken is what the checks read of a token whose signature has been verified.
type accessToken struct {
	Issuer   string
	Audience []string
	Subject  string
	Expiry   time.Time
	Claims   map[string]any
}

// VerifyAccessToken checks an access token of the realm against now and says who it names.
func (s *Service) VerifyAccessToken(ctx context.Context, raw string, now time.Time) (*Identity, error) {
	provider, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}
	// The library checks the signature against the realm's key set and every claim is checked below, so that each
	// refusal names what is wrong.
	verifier := provider.Verifier(&coreoidc.Config{SkipClientIDCheck: true, SkipExpiryCheck: true, SkipIssuerCheck: true})
	token, err := verifier.Verify(s.providerContext(ctx), raw)
	if err != nil {
		return nil, refuse(fmt.Errorf("%w: %w", ErrTokenUnverified, err),
			"The token is not signed with the keys the realm at %s publishes. Take a fresh access token from that realm with the client's credentials and send it again.", s.cfg.Issuer)
	}
	var claims map[string]any
	if err := token.Claims(&claims); err != nil {
		return nil, refuse(fmt.Errorf("%w: read its claims: %w", ErrTokenUnverified, err),
			"The token's claims cannot be read. Take a fresh access token from the realm at %s and send it again.", s.cfg.Issuer)
	}
	return checkAccessToken(accessToken{Issuer: token.Issuer, Audience: token.Audience, Subject: token.Subject,
		Expiry: token.Expiry, Claims: claims}, s.cfg.Issuer, s.cfg.ClientID, now)
}

// checkAccessToken holds a verified token to the issuer, Rotor's audience, its expiry and the administrators' group.
func checkAccessToken(token accessToken, issuer, audience string, now time.Time) (*Identity, error) {
	switch {
	case token.Issuer != issuer:
		return nil, refuse(ErrTokenIssuer, "The token was issued by %q, and Rotor takes tokens of %s alone. Take the token from that realm.",
			token.Issuer, issuer)
	case audience == "" || !slices.Contains(token.Audience, audience):
		return nil, refuse(ErrTokenAudience, "The token is not meant for Rotor: its audience does not hold %s. "+
			"Give the client it is issued to an audience mapper for %s, then take a fresh token.", audience, audience)
	case token.Expiry.IsZero() || !now.Before(token.Expiry):
		return nil, refuse(ErrTokenExpired, "The token has expired or carries no expiry. Take a fresh access token from the realm and send it again.")
	case asString(token.Claims["typ"]) != accessTokenType:
		return nil, refuse(ErrTokenNotAccess, "That is not an access token. Send the access token the realm issued, not an id or refresh token.")
	case token.Subject == "":
		return nil, refuse(ErrTokenNoAccount, "The token names no account. Take it with the client credentials of a client that has a service account.")
	}
	identity, err := tokenIdentity(token)
	if err != nil {
		return nil, err
	}
	if RoleFor(identity.Groups) != auth.RoleAdmin {
		return nil, refuse(ErrTokenNotAdmin, "The token's account is not in the group %s, so it cannot administer Rotor. "+
			"Add the client's service account to %s in the realm, then take a fresh token.", AdminGroup, AdminGroup)
	}
	return identity, nil
}

// tokenIdentity is a person when the token carries an address, and otherwise the service account of the client it
// was issued to, known by that client's name.
func tokenIdentity(token accessToken) (*Identity, error) {
	if strings.TrimSpace(asString(token.Claims["email"])) != "" {
		identity, err := identityFromClaims(token.Issuer, token.Subject, token.Claims)
		if err != nil {
			return nil, refuse(err, "The token's account has an email address the realm has not verified. Verify it in the realm, then take a fresh token.")
		}
		return identity, nil
	}
	client := strings.ToLower(strings.TrimSpace(asString(token.Claims["azp"])))
	if client == "" {
		return nil, refuse(ErrTokenNoAccount, "The token names no client it was issued to. Take it with the client credentials of a client that has a service account.")
	}
	return &Identity{Issuer: token.Issuer, Subject: token.Subject, Email: client + "@" + serviceAccountDomain, Name: client,
		Groups: normalizeGroups(asStrings(token.Claims[groupsClaim]))}, nil
}

// Authenticate lets in the account a realm's access token names: it is made or updated as a sign-in would, its role
// copied from the realm, without a session.
func (s *Service) Authenticate(ctx context.Context, raw string) (*auth.Principal, error) {
	identity, err := s.VerifyAccessToken(ctx, raw, time.Now())
	if err != nil {
		return nil, err
	}
	var p *auth.Principal
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		user, role, err := adopt(ctx, tx, identity)
		if err != nil {
			return err
		}
		p = &auth.Principal{UserID: user.id, Email: identity.Email, Name: identity.Name, Role: role, Groups: identity.Groups}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}
