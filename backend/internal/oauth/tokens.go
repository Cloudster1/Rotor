package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrInvalidToken is an access token unknown, expired, or of a grant that ended.
var ErrInvalidToken = errors.New("the access token is not valid")

// ClientAuth is how a client said who it is: in the form, or with HTTP Basic.
type ClientAuth struct {
	ID, Secret string
	Basic      bool
}

// clientFor authenticates the client of a token or revocation request.
func (s *Service) clientFor(ctx context.Context, who ClientAuth) (*client, error) {
	if who.ID == "" {
		return nil, refuse(ErrInvalidClient, "Say which client asks, with client_id or HTTP Basic.")
	}
	c, err := s.clientOf(ctx, who.ID)
	if err != nil {
		return nil, err
	}
	if r := c.authenticate(who.Secret, who.Basic); r != nil {
		return nil, r
	}
	return c, nil
}

var errGrant = refuse(ErrInvalidGrant, "The code or refresh token is not one this client may use: it expired, was used, or was ended. Connect the assistant again.")

// Token trades a code and its verifier, or a refresh token, for a new access and refresh token.
func (s *Service) Token(ctx context.Context, in TokenRequest, who ClientAuth) (*TokenResponse, error) {
	if on, err := s.Enabled(ctx); err != nil || !on {
		return nil, firstErr(err, errSwitchedOffRefusal)
	}
	if in.GrantType != GrantAuthorizationCode && in.GrantType != GrantRefreshToken {
		return nil, refuse(ErrUnsupportedGrantType, "The grants offered are authorization_code and refresh_token.")
	}
	c, err := s.clientFor(ctx, who)
	if err != nil {
		return nil, err
	}
	if !c.grants(in.GrantType) {
		return nil, refuse(ErrUnauthorizedClient, "The client did not register for "+in.GrantType+".")
	}
	if in.Resource != "" && !s.isResource(in.Resource) {
		return nil, refuse(ErrInvalidTarget, "The one resource is the MCP endpoint, "+s.Resource()+".")
	}
	var (
		out     *TokenResponse
		refusal *Refusal
	)
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		redeem := redeemCode
		if in.GrantType == GrantRefreshToken {
			redeem = turnRefresh
		}
		grant, readOnly, err := redeem(ctx, tx, c.ID, in)
		// A refusal still commits: a code or refresh token used twice has just ended its grant.
		if errors.As(err, &refusal) {
			return nil
		}
		if err != nil {
			return err
		}
		out, err = issue(ctx, tx, grant, readOnly)
		return err
	})
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, refusal
	}
	return out, nil
}

// endGrant ends a grant whose code or refresh token came back once used, as a stolen one would, and refuses.
func endGrant(ctx context.Context, tx db.DBTX, grant uuid.UUID, r *Refusal) error {
	if err := deleteGrant(ctx, tx, grant, EndedByReusedToken); err != nil {
		return err
	}
	return r
}

func redeemCode(ctx context.Context, tx db.DBTX, clientID string, in TokenRequest) (uuid.UUID, bool, error) {
	var (
		grant                          uuid.UUID
		owner, redirect, challenge, rs string
		readOnly, expired              bool
		used                           *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT g.id, g.client_id, g.read_only, c.redirect_uri, c.code_challenge, c.resource, c.used_at, c.expires_at <= now()
		FROM oauth_code c JOIN oauth_grant g ON g.id = c.grant_id
		WHERE c.code_hash = $1 FOR UPDATE OF c`, auth.Hash(in.Code)).Scan(&grant, &owner, &readOnly, &redirect, &challenge, &rs, &used, &expired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return grant, false, errGrant
	case err != nil:
		return grant, false, err
	case owner != clientID:
		return grant, false, errGrant
	case used != nil:
		return grant, false, endGrant(ctx, tx, grant, refuse(ErrInvalidGrant, "The code was used already, so the grant it made is ended. Connect the assistant again."))
	case expired:
		return grant, false, errGrant
	case in.RedirectURI != redirect:
		return grant, false, refuse(ErrInvalidGrant, "Send the redirect_uri the code was asked for with.")
	case !VerifyPKCE(challenge, in.CodeVerifier):
		return grant, false, refuse(ErrInvalidGrant, "The code_verifier is not the one the code_challenge was made from.")
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_code SET used_at = now() WHERE code_hash = $1`, auth.Hash(in.Code)); err != nil {
		return grant, false, err
	}
	return grant, readOnly, nil
}

func turnRefresh(ctx context.Context, tx db.DBTX, clientID string, in TokenRequest) (uuid.UUID, bool, error) {
	var (
		grant             uuid.UUID
		owner             string
		readOnly, expired bool
		replaced          *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT g.id, g.client_id, t.read_only, t.replaced_at, t.expires_at <= now()
		FROM oauth_token t JOIN oauth_grant g ON g.id = t.grant_id
		WHERE t.token_hash = $1 AND t.kind = 'refresh' FOR UPDATE OF t`, auth.Hash(in.RefreshToken)).Scan(&grant, &owner, &readOnly, &replaced, &expired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return grant, false, errGrant
	case err != nil:
		return grant, false, err
	case owner != clientID:
		return grant, false, errGrant
	case replaced != nil:
		return grant, false, endGrant(ctx, tx, grant,
			refuse(ErrInvalidGrant, "The refresh token was used already, so the grant is ended, as a stolen one would be. Connect the assistant again."))
	case expired:
		return grant, false, errGrant
	}
	asked, ok := scopeReadOnly(in.Scope)
	if !ok || (readOnly && in.Scope != "" && !asked) {
		return grant, false, refuse(ErrInvalidScope, "A refresh may only narrow the scope, to mcp:read.")
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_token SET replaced_at = now() WHERE token_hash = $1`, auth.Hash(in.RefreshToken)); err != nil {
		return grant, false, err
	}
	return grant, readOnly || (in.Scope != "" && asked), nil
}

// issue hands a grant a new access and refresh token; only their digests are kept.
func issue(ctx context.Context, tx db.DBTX, grant uuid.UUID, readOnly bool) (*TokenResponse, error) {
	access, accessDigest, err := secret(AccessTokenPrefix)
	if err != nil {
		return nil, err
	}
	refresh, refreshDigest, err := secret(RefreshTokenPrefix)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO oauth_token (token_hash, grant_id, kind, read_only, expires_at) VALUES
			($1, $3, 'access', $4, now() + make_interval(secs => $5)),
			($2, $3, 'refresh', $4, now() + make_interval(secs => $6))`,
		accessDigest, refreshDigest, grant, readOnly, config.OAuthAccessTTL.Seconds(), config.OAuthRefreshTTL.Seconds()); err != nil {
		return nil, err
	}
	return &TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: expiresIn(config.OAuthAccessTTL), RefreshToken: refresh,
		Scope: scopeOf(readOnly)}, nil
}

// Revoke ends an access token, or a refresh token and with it the grant (RFC 7009); a token unknown, ended
// already or another client's is no refusal, so the answer says nothing of it.
func (s *Service) Revoke(ctx context.Context, in RevokeRequest, who ClientAuth) error {
	c, err := s.clientFor(ctx, who)
	if err != nil {
		return err
	}
	if in.Token == "" {
		return refuse(ErrInvalidRequest, "Send the token to end.")
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			kind  string
			grant uuid.UUID
		)
		err := tx.QueryRow(ctx, `SELECT t.kind, t.grant_id FROM oauth_token t JOIN oauth_grant g ON g.id = t.grant_id
			WHERE t.token_hash = $1 AND g.client_id = $2`, auth.Hash(in.Token), c.ID).Scan(&kind, &grant)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return err
		case kind == "refresh":
			return deleteGrant(ctx, tx, grant, EndedByClient)
		}
		_, err = tx.Exec(ctx, `DELETE FROM oauth_token WHERE token_hash = $1`, auth.Hash(in.Token))
		return err
	})
}

// Authenticate reads the person an access token acts for, with the grant it acts through. A person switched off
// is refused, as a session of theirs is.
func (s *Service) Authenticate(ctx context.Context, token string) (*auth.Principal, error) {
	if !IsAccessToken(token) {
		return nil, ErrInvalidToken
	}
	var (
		p        auth.Principal
		a        auth.Assistant
		lastUsed *time.Time
		active   bool
		on       bool
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT g.id, c.name, g.read_only OR t.read_only, g.last_used_at,
			       u.id, u.email, u.name, u.role, u.groups, u.is_active, s.connections_on
			FROM oauth_token t
			JOIN oauth_grant g ON g.id = t.grant_id
			JOIN oauth_client c ON c.id = g.client_id
			JOIN app_user u ON u.id = g.user_id
			CROSS JOIN assistant_setting s
			WHERE t.token_hash = $1 AND t.kind = 'access' AND t.expires_at > now()`, auth.Hash(token)).Scan(
			&a.GrantID, &a.Client, &a.ReadOnly, &lastUsed, &p.UserID, &p.Email, &p.Name, &p.Role, &p.Groups, &active, &on)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrInvalidToken
	case err != nil:
		return nil, err
	case !on:
		return nil, ErrSwitchedOff
	case !active:
		return nil, auth.ErrSwitchedOff
	}
	p.Assistant = &a
	if lastUsed == nil || s.now().Sub(*lastUsed) > config.OAuthLastUsedEvery {
		_ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE oauth_grant SET last_used_at = now() WHERE id = $1`, a.GrantID)
			return err
		})
	}
	return &p, nil
}
