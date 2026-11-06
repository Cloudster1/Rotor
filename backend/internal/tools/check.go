package tools

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Check reads a tool's meta and metadata again and keeps what changed, without registering again: a new client
// would leave every person's connection behind. It also proves the client works, as minting would (#266).
func (s *Service) Check(ctx context.Context, id uuid.UUID) (Tool, error) {
	tool, err := s.Get(ctx, id)
	if err != nil {
		return Tool{}, err
	}
	d, err := s.discover(ctx, tool.BaseURL)
	var lacking *Missing
	switch {
	case errors.As(err, &lacking):
		tool.Name, tool.Capabilities = d.Name, d.Capabilities
	case err != nil:
		return Tool{}, err
	default:
		tool.Name, tool.Resource, tool.MCPURL, tool.Issuer, tool.Capabilities = d.Name, d.Resource, d.MCPURL, d.Issuer, d.Capabilities
		tool.GrantTypes = d.GrantTypes
		tool.AuthorizationEndpoint, tool.TokenEndpoint = d.AuthorizationEndpoint, d.TokenEndpoint
		tool.RevocationEndpoint, tool.RegistrationEndpoint, tool.ScopesSupported = d.RevocationEndpoint, d.RegistrationEndpoint, d.ScopesSupported
	}
	if tool.Status, err = s.clientStatus(ctx, tool); err != nil {
		return Tool{}, err
	}
	actor := actorOf(ctx)
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE tool SET name = $2, resource = $3, issuer = $4, authorization_endpoint = $5, token_endpoint = $6,
				revocation_endpoint = NULLIF($7, ''), registration_endpoint = NULLIF($8, ''), scopes_supported = $9, capabilities = $10,
				mcp_url = $11, grant_types = $12, status = $13
			WHERE id = $1`,
			tool.ID, tool.Name, tool.Resource, tool.Issuer, tool.AuthorizationEndpoint, tool.TokenEndpoint,
			tool.RevocationEndpoint, tool.RegistrationEndpoint, tool.ScopesSupported, tool.Capabilities, tool.MCPURL, tool.GrantTypes,
			tool.Status)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		missing := []string{}
		for _, m := range tool.Missing() {
			missing = append(missing, m.Capability)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionToolChecked, TargetType: "tool", TargetID: tool.ID.String(), Actor: actor,
			Data: map[string]any{"name": tool.Name, "capabilities": tool.Capabilities, "missing": missing, "status": tool.Status}})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Tool{}, ErrNotFound
	}
	return tool, err
}

// clientStatus is the tool's status once its token endpoint has said whether it still knows Rotor's client. Only a
// tool marked ready changes: one awaiting approval is refused the same way, and registering again alone mends the rest.
func (s *Service) clientStatus(ctx context.Context, tool Tool) (string, error) {
	if tool.Status != StatusReady {
		return tool.Status, nil
	}
	client, _, err := s.vault.ToolClient(ctx, tool.ID)
	if errors.Is(err, vault.ErrNotFound) {
		return StatusNeedsRegistration, nil
	}
	if err != nil {
		return "", err
	}
	known, err := s.clientKnown(ctx, tool, client)
	if err != nil || known {
		return tool.Status, err
	}
	return StatusNeedsRegistration, nil
}
