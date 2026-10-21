package oauth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// The reasons the consent page names when the browser cannot go back to the client.
const (
	PageInvalidClient   = "invalid_client"
	PageInvalidRedirect = "invalid_redirect"
	PageSwitchedOff     = "switched_off"
)

var (
	// ErrNoRequest is a request expired, decided already, or never made.
	ErrNoRequest = errors.New("no such authorization request")
	// ErrWiderThanAsked is a grant of more than the client asked for.
	ErrWiderThanAsked = errors.New("the assistant asked only to read, so it can only be granted reading")
)

// Authorize checks a client's request in the contract's order and answers where the browser goes: the consent page
// with the request kept, the page saying why the client is refused, or the client with the error.
func (s *Service) Authorize(ctx context.Context, q url.Values) (string, error) {
	page := func(reason string) string { return s.ConsentPage() + "?error=" + reason }
	c, err := s.clientOf(ctx, q.Get("client_id"))
	var refused *Refusal
	switch {
	case errors.As(err, &refused):
		return page(PageInvalidClient), nil
	case err != nil:
		return "", err
	}
	redirect := q.Get("redirect_uri")
	if !RedirectMatches(c.RedirectURIs, redirect) {
		return page(PageInvalidRedirect), nil
	}
	if on, err := s.Enabled(ctx); err != nil {
		return "", err
	} else if !on {
		return page(PageSwitchedOff), nil
	}
	state := q.Get("state")
	back := func(code, description string) string {
		return withQuery(redirect, url.Values{"error": {code}, "error_description": {description}, "state": {state}, "iss": {s.issuer}})
	}
	if q.Get("response_type") != ResponseCode {
		return back(ErrUnsupportedResponseType, "The one response type is code."), nil
	}
	if state == "" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != ChallengeS256 {
		return back(ErrInvalidRequest, "Send state, and a PKCE code_challenge with code_challenge_method S256."), nil
	}
	if !challengeShape.MatchString(q.Get("code_challenge")) {
		return back(ErrInvalidRequest, "The code_challenge is not an S256 challenge."), nil
	}
	if !s.isResource(q.Get("resource")) {
		return back(ErrInvalidTarget, "Name the MCP endpoint, "+s.Resource()+", as the resource."), nil
	}
	readOnly, ok := scopeReadOnly(q.Get("scope"))
	if !ok {
		return back(ErrInvalidScope, "Ask for mcp, or mcp:read to only read."), nil
	}
	if !c.grants(GrantAuthorizationCode) {
		return back(ErrUnauthorizedClient, "The client did not register for authorization_code."), nil
	}
	var id uuid.UUID
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			INSERT INTO oauth_request (client_id, redirect_uri, state, code_challenge, read_only, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7)) RETURNING id`,
			c.ID, redirect, state, q.Get("code_challenge"), readOnly, s.Resource(), config.OAuthRequestTTL.Seconds()).Scan(&id)
	})
	if err != nil {
		return "", err
	}
	return s.ConsentPage() + "?request=" + id.String(), nil
}

// isResource says whether a client named the MCP endpoint, written with or without a closing slash.
func (s *Service) isResource(asked string) bool {
	return strings.TrimRight(asked, "/") == s.Resource()
}

func withQuery(address string, q url.Values) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	values := u.Query()
	for k, v := range q {
		values[k] = v
	}
	u.RawQuery = values.Encode()
	return u.String()
}

type pending struct {
	AuthorizationRequest
	redirect string
	state    string
}

// Request is a request as its consent page shows it; anybody signed in who was handed its id may read it.
func (s *Service) Request(ctx context.Context, id uuid.UUID) (*AuthorizationRequest, error) {
	p, err := s.pending(ctx, id)
	if err != nil {
		return nil, err
	}
	return &p.AuthorizationRequest, nil
}

func (s *Service) pending(ctx context.Context, id uuid.UUID) (*pending, error) {
	var p pending
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT r.id, r.redirect_uri, r.state, r.read_only, r.expires_at, c.id, c.name, c.uri
			FROM oauth_request r JOIN oauth_client c ON c.id = r.client_id
			WHERE r.id = $1 AND r.expires_at > now()`, id).Scan(&p.ID, &p.redirect, &p.state, &p.ReadOnly, &p.ExpiresAt,
			&p.Client.ID, &p.Client.Name, &p.Client.URI)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoRequest
	}
	if err != nil {
		return nil, err
	}
	p.RedirectHost = RedirectHost(p.redirect)
	return &p, nil
}

// Decide answers a request as the person who consents and says where the browser goes back to. Allowing makes the
// grant, ending an earlier one of the same client, and hands the client a code to trade for its tokens.
func (s *Service) Decide(ctx context.Context, person, id uuid.UUID, in ConsentInput) (string, error) {
	p, err := s.pending(ctx, id)
	if err != nil {
		return "", err
	}
	back := func(q url.Values) string {
		q.Set("state", p.state)
		q.Set("iss", s.issuer)
		return withQuery(p.redirect, q)
	}
	if !in.Allow {
		err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM oauth_request WHERE id = $1`, id)
			return err
		})
		return back(url.Values{"error": {ErrAccessDenied}, "error_description": {"The person declined to connect the assistant."}}), err
	}
	if p.ReadOnly && !in.ReadOnly {
		return "", ErrWiderThanAsked
	}
	readOnly := in.ReadOnly || p.ReadOnly
	code, digest, err := secret(codePrefix)
	if err != nil {
		return "", err
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var r struct {
			client, redirect, challenge, resource string
		}
		err := tx.QueryRow(ctx, `SELECT client_id, redirect_uri, code_challenge, resource FROM oauth_request_take($1)`, id).
			Scan(&r.client, &r.redirect, &r.challenge, &r.resource)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoRequest
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_grant WHERE client_id = $1 AND user_id = $2`, r.client, person); err != nil {
			return err
		}
		var grant uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO oauth_grant (user_id, client_id, redirect_uri, read_only) VALUES ($1, $2, $3, $4)
			RETURNING id`, person, r.client, r.redirect, readOnly).Scan(&grant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO oauth_code (code_hash, grant_id, redirect_uri, code_challenge, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => $6))`, digest, grant, r.redirect, r.challenge, r.resource,
			config.OAuthCodeTTL.Seconds()); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthGranted, TargetType: "oauth_grant", TargetID: grant.String(),
			Actor: person, Data: map[string]any{"client": p.Client.Name, "clientId": p.Client.ID, "readOnly": readOnly}})
	})
	if err != nil {
		return "", err
	}
	return back(url.Values{"code": {code}}), nil
}

// expiresIn is how long a token lasts, as the answer counts it.
func expiresIn(ttl time.Duration) int { return int(ttl / time.Second) }
