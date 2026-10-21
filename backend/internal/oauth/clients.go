package oauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// client is a client as the server knows it.
type client struct {
	ID, Name, URI string
	Kind          ClientKind
	RedirectURIs  []string
	AuthMethod    string
	SecretHash    []byte
	GrantTypes    []string
}

func (c *client) ref() ClientRef { return ClientRef{ID: c.ID, Name: c.Name, URI: c.URI, Kind: c.Kind} }

// grants says whether the client registered for a grant.
func (c *client) grants(grant string) bool { return slices.Contains(c.GrantTypes, grant) }

// authenticate holds what a client sent to the method it registered with; a client that keeps no secret sends none.
func (c *client) authenticate(secret string, basic bool) *Refusal {
	wrong := refuse(ErrInvalidClient, "The client could not be authenticated. Send its secret as it registered, or register it again.")
	switch c.AuthMethod {
	case AuthNone:
		if secret != "" {
			return wrong
		}
		return nil
	case AuthSecretBasic:
		if !basic {
			return wrong
		}
	case AuthSecretPost:
		if basic {
			return wrong
		}
	}
	if secret == "" || subtle.ConstantTimeCompare(auth.Hash(secret), c.SecretHash) != 1 {
		return wrong
	}
	return nil
}

var errUnknownClient = refuse(ErrInvalidClient, "Rotor does not know this client. Register it, or give the address of its metadata document as its client_id.")

// clientOf finds a client, fetching its metadata document again when it is one and the kept copy went stale.
func (s *Service) clientOf(ctx context.Context, id string) (*client, error) {
	var (
		c     client
		fresh *time.Time
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id, name, uri, kind, redirect_uris, auth_method, secret_hash, grant_types, fresh_until
			FROM oauth_client WHERE id = $1`, id).Scan(&c.ID, &c.Name, &c.URI, &c.Kind, &c.RedirectURIs, &c.AuthMethod, &c.SecretHash,
			&c.GrantTypes, &fresh)
	})
	switch {
	case err == nil && (c.Kind == ClientRegistered || fresh.After(s.now())):
		return &c, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	case !IsMetadataClientID(id):
		return nil, errUnknownClient
	}
	return s.fetchClient(ctx, id)
}

var errSwitchedOffRefusal = refuse(ErrTemporarilyUnavailable, "Assistants are switched off on this Rotor. Ask its administrators to switch them on.")

// Register keeps a client that registered itself (RFC 7591), at most config.OAuthMaxRegistrationsPerHour from one
// address.
func (s *Service) Register(ctx context.Context, in ClientMetadataInput, from string) (*RegisteredClient, error) {
	if on, err := s.Enabled(ctx); err != nil || !on {
		return nil, firstErr(err, errSwitchedOffRefusal)
	}
	if r := checkMetadata(&in); r != nil {
		return nil, r
	}
	id, _, err := secret("")
	if err != nil {
		return nil, err
	}
	out := RegisteredClient{ClientMetadataInput: in, ClientID: id, ClientIDIssuedAt: s.now().Unix()}
	var digest []byte
	if in.TokenEndpointAuthMethod != AuthNone {
		if out.ClientSecret, digest, err = secret(clientSecretPrefix); err != nil {
			return nil, err
		}
	}
	tooMany := false
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM oauth_client WHERE kind = 'registered' AND registered_from = $1
			AND created_at > now() - interval '1 hour'`, from).Scan(&recent); err != nil {
			return err
		}
		if tooMany = recent >= config.OAuthMaxRegistrationsPerHour; tooMany {
			return nil
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oauth_client (id, name, uri, redirect_uris, auth_method, secret_hash, grant_types, registered_from)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, in.ClientName, in.ClientURI, in.RedirectURIs, in.TokenEndpointAuthMethod, digest, in.GrantTypes, from)
		return err
	})
	if err != nil {
		return nil, err
	}
	if tooMany {
		r := refuse(ErrTemporarilyUnavailable, "Too many clients registered from this address in the last hour. Try again later.")
		r.Status = http.StatusTooManyRequests
		return nil, r
	}
	return &out, nil
}

// firstErr is err when there is one, otherwise the refusal.
func firstErr(err error, r *Refusal) error {
	if err != nil {
		return err
	}
	return r
}
