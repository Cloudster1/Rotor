package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// AddRequest names a tool by its address; a client registered by hand comes with its secret.
type AddRequest struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

// Add discovers a tool, registers Rotor as its client and keeps it. A tool Rotor knows is registered again,
// for one that forgot the client; created says which happened.
func (s *Service) Add(ctx context.Context, req AddRequest) (tool Tool, created bool, err error) {
	base, err := normalizeBaseURL(req.BaseURL, s.allowHTTP)
	if err != nil {
		return Tool{}, false, err
	}
	if (req.ClientID == "") != (req.ClientSecret == "") {
		return Tool{}, false, refuse("Give both the client id and its secret, or neither to let Rotor register itself.")
	}
	d, err := s.discover(ctx, base)
	if err != nil {
		return Tool{}, false, err
	}
	method, err := d.authMethod()
	if err != nil {
		return Tool{}, false, err
	}

	known, err := s.byBaseURL(ctx, base)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Tool{}, false, err
	}
	created = errors.Is(err, ErrNotFound)
	id := known.ID
	if created {
		id = uuid.Must(uuid.NewV7())
	}

	client, registration := vault.ToolClient{ClientID: req.ClientID, ClientSecret: req.ClientSecret, AuthMethod: method}, RegistrationManual
	if req.ClientID == "" {
		if client, err = s.register(ctx, d, method); err != nil {
			return Tool{}, false, err
		}
		registration = RegistrationDynamic
	}
	if err := s.keepClient(ctx, id, client, created); err != nil {
		return Tool{}, false, err
	}

	tool = Tool{ID: id, Name: d.Name, BaseURL: base, Resource: d.Resource, Issuer: d.Issuer,
		AuthorizationEndpoint: d.AuthorizationEndpoint, TokenEndpoint: d.TokenEndpoint, RevocationEndpoint: d.RevocationEndpoint,
		RegistrationEndpoint: d.RegistrationEndpoint, ScopesSupported: d.ScopesSupported, Capabilities: d.Capabilities,
		ClientID: client.ClientID, ClientAuthMethod: client.AuthMethod, Registration: registration, Status: StatusReady}
	if err := s.save(ctx, &tool, created); err != nil {
		if created {
			// The row was refused, so the secret has nothing to belong to.
			_ = s.vault.DeleteToolClient(context.WithoutCancel(ctx), id)
		}
		return Tool{}, false, err
	}
	return tool, created, nil
}

// keepClient writes the client secret to OpenBao, over the one there for a tool registered again.
func (s *Service) keepClient(ctx context.Context, id uuid.UUID, client vault.ToolClient, created bool) error {
	version := 0
	if !created {
		_, current, err := s.vault.ToolClient(ctx, id)
		if err != nil && !errors.Is(err, vault.ErrNotFound) {
			return err
		}
		version = current
	}
	if _, err := s.vault.PutToolClient(ctx, id, client, version); err != nil {
		return fmt.Errorf("keep the client secret of %s: %w", id, err)
	}
	return nil
}

// save writes the row as the person on the context, so the database refuses anyone but an administrator.
func (s *Service) save(ctx context.Context, t *Tool, created bool) error {
	actor := actorOf(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if created {
			err = tx.QueryRow(ctx, `
				INSERT INTO tool (id, name, base_url, resource, issuer, authorization_endpoint, token_endpoint,
					revocation_endpoint, registration_endpoint, scopes_supported, capabilities, client_id,
					client_auth_method, registration, status, added_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, $11, $12, $13, $14, $15, $16)
				RETURNING created_at`,
				t.ID, t.Name, t.BaseURL, t.Resource, t.Issuer, t.AuthorizationEndpoint, t.TokenEndpoint,
				t.RevocationEndpoint, t.RegistrationEndpoint, t.ScopesSupported, t.Capabilities, t.ClientID,
				t.ClientAuthMethod, t.Registration, t.Status, nullable(actor)).Scan(&t.AddedAt)
		} else {
			err = tx.QueryRow(ctx, `
				UPDATE tool SET name = $2, resource = $3, issuer = $4, authorization_endpoint = $5, token_endpoint = $6,
					revocation_endpoint = NULLIF($7, ''), registration_endpoint = NULLIF($8, ''), scopes_supported = $9,
					capabilities = $10, client_id = $11, client_auth_method = $12, registration = $13, status = $14
				WHERE id = $1 RETURNING created_at`,
				t.ID, t.Name, t.Resource, t.Issuer, t.AuthorizationEndpoint, t.TokenEndpoint, t.RevocationEndpoint,
				t.RegistrationEndpoint, t.ScopesSupported, t.Capabilities, t.ClientID, t.ClientAuthMethod,
				t.Registration, t.Status).Scan(&t.AddedAt)
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionToolAdded, TargetType: "tool", TargetID: t.ID.String(), Actor: actor,
			Data: map[string]any{"base_url": t.BaseURL, "name": t.Name, "registration": t.Registration, "again": !created}})
	})
}

func (s *Service) byBaseURL(ctx context.Context, base string) (Tool, error) {
	var t Tool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		t, err = scanTool(tx.QueryRow(ctx, `SELECT `+toolColumns+` FROM tool WHERE base_url = $1`, base))
		return err
	})
	return t, err
}

// Remove ends every connection to the tool, then forgets its client secret and the tool.
func (s *Service) Remove(ctx context.Context, id uuid.UUID) error {
	tool, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	ended := 0
	if s.Connections != nil {
		if ended, err = s.Connections.EndToolConnections(ctx, tool); err != nil {
			return err
		}
	}
	actor := actorOf(ctx)
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM tool WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionToolRemoved, TargetType: "tool", TargetID: id.String(), Actor: actor,
			Data: map[string]any{"base_url": tool.BaseURL, "name": tool.Name, "connections_ended": ended}})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// The row is gone, so the caller is told when its secret could not follow it.
	if err := s.vault.DeleteToolClient(context.WithoutCancel(ctx), id); err != nil {
		return fmt.Errorf("forget the client secret of %s: %w", tool.Name, err)
	}
	return nil
}

func actorOf(ctx context.Context) uuid.UUID {
	if p := auth.PrincipalFrom(ctx); p != nil {
		return p.UserID
	}
	return uuid.Nil
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
