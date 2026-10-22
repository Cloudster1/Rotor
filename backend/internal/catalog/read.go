package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/connections"
)

// Read calls one reading MCP tool as the person on the context, with a token minted for the call and revoked right
// after. It answers false when the tool refuses or does not show what was asked, and an error when it did not answer.
func (s *Service) Read(ctx context.Context, toolID uuid.UUID, name string, arguments map[string]any) (bool, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return false, ErrNotConnected
	}
	tool, err := s.tools.Lookup(ctx, toolID)
	if err != nil {
		return false, err
	}
	token, err := s.minter.Mint(ctx, connections.MintRequest{UserID: p.UserID, ToolID: tool.ID, Scopes: tool.Profile().ReadScopes, RunID: "probe"})
	switch {
	case errors.Is(err, connections.ErrNotConnected), errors.Is(err, connections.ErrReconnect), errors.Is(err, connections.ErrNeedsRegistration),
		errors.Is(err, connections.ErrScopeNotConsented):
		return false, nil
	case err != nil:
		return false, err
	}
	defer func() {
		if err := s.minter.Revoke(context.WithoutCancel(ctx), tool.ID, token.Value); err != nil {
			s.log.Warn("cannot revoke the probe's token", "tool", tool.Name, "error", err)
		}
	}()
	var result struct {
		IsError bool `json:"isError"`
	}
	err = s.call(ctx, tool, token.Value, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result)
	switch {
	case errors.Is(err, ErrNotConnected), errors.Is(err, ErrRefusedCall):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("ask %s for %s: %w", tool.Name, name, err)
	}
	return !result.IsError, nil
}
