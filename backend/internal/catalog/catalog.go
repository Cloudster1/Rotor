// Package catalog reads a tool's MCP tool list as a person's connection sees it, for the workflow editor and for
// checking a workflow against its tools. Lists are cached per tool build and person.
package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/cache"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/tools"
)

// Errors a caller tells apart.
var (
	// ErrNotConnected is a person without a usable connection to the tool.
	ErrNotConnected = errors.New("not connected to the tool")
	// ErrUnreachable is a tool that did not answer.
	ErrUnreachable = errors.New("the tool did not answer")
)

// Tool is one MCP tool, with the hints it gives about itself.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	ReadOnly    bool            `json:"readOnly"`
	Destructive bool            `json:"destructive"`
}

// Catalog is a tool's MCP tools as one person's connection sees them.
type Catalog struct {
	ToolID      uuid.UUID `json:"toolID"`
	ToolName    string    `json:"toolName"`
	ToolBaseURL string    `json:"toolBaseURL"`
	Build       string    `json:"build"`
	Tools       []Tool    `json:"tools"`
}

// Registry is what the catalog asks of the tools; *tools.Service is the real one.
type Registry interface {
	List(ctx context.Context) ([]tools.Tool, error)
	Lookup(ctx context.Context, id uuid.UUID) (tools.Tool, error)
}

// Minter mints and revokes a person's token; *connections.Service is the real one.
type Minter interface {
	Mint(ctx context.Context, req connections.MintRequest) (connections.Token, error)
	Revoke(ctx context.Context, toolID uuid.UUID, token string) error
}

// Service reads catalogs.
type Service struct {
	tools  Registry
	minter Minter
	http   *http.Client
	cache  *cache.Cache
	log    *slog.Logger
}

// NewService returns the catalogs; client reaches the tools as the registry does, and cache may be nil.
func NewService(registry Registry, minter Minter, client *http.Client, c *cache.Cache, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{tools: registry, minter: minter, http: client, cache: c, log: log}
}

// Get is a tool's catalog for the person on the context, from the cache while the tool's build is the same.
func (s *Service) Get(ctx context.Context, toolID uuid.UUID) (Catalog, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Catalog{}, ErrNotConnected
	}
	tool, err := s.tools.Lookup(ctx, toolID)
	if err != nil {
		return Catalog{}, err
	}
	build, err := s.build(ctx, tool)
	if err != nil {
		return Catalog{}, err
	}
	key := fmt.Sprintf("catalog:%s:%s:%s", tool.ID, build, p.UserID)
	if cached, ok := s.cache.Get(ctx, key); ok {
		var c Catalog
		if json.Unmarshal(cached, &c) == nil {
			return c, nil
		}
	}
	token, err := s.minter.Mint(ctx, connections.MintRequest{UserID: p.UserID, ToolID: tool.ID, RunID: "catalog"})
	switch {
	case errors.Is(err, connections.ErrNotConnected), errors.Is(err, connections.ErrReconnect), errors.Is(err, connections.ErrNeedsRegistration):
		return Catalog{}, fmt.Errorf("%w: %w", ErrNotConnected, err)
	case errors.Is(err, connections.ErrUnreachable):
		return Catalog{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	case err != nil:
		return Catalog{}, err
	}
	// The token was only for reading the list; it should not outlive this request.
	defer func() {
		if err := s.minter.Revoke(context.WithoutCancel(ctx), tool.ID, token.Value); err != nil {
			s.log.Warn("cannot revoke the catalog's token", "tool", tool.Name, "error", err)
		}
	}()
	list, err := s.list(ctx, tool, token.Value)
	if err != nil {
		return Catalog{}, err
	}
	c := Catalog{ToolID: tool.ID, ToolName: tool.Name, ToolBaseURL: tool.BaseURL, Build: build, Tools: list}
	if encoded, err := json.Marshal(c); err == nil {
		s.cache.Set(ctx, key, encoded, config.CatalogTTL)
	}
	return c, nil
}

// build is the tool's build as its meta names it, so a new release is read afresh.
func (s *Service) build(ctx context.Context, tool tools.Tool) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tool.BaseURL+"/api/v1/meta", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	var meta struct {
		Build struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		} `json:"build"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, config.MaxToolDocumentBytes)).Decode(&meta) != nil {
		return "", fmt.Errorf("%w: %s answered its meta with %d", ErrUnreachable, tool.Name, resp.StatusCode)
	}
	return strings.Trim(meta.Build.Version+"+"+meta.Build.Commit, "+"), nil
}

// list calls tools/list at the tool's MCP endpoint, following its pages.
func (s *Service) list(ctx context.Context, tool tools.Tool, token string) ([]Tool, error) {
	out := []Tool{}
	cursor := ""
	for page := 0; page < config.MaxCatalogPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
				Annotations struct {
					ReadOnly    bool `json:"readOnlyHint"`
					Destructive bool `json:"destructiveHint"`
				} `json:"annotations"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := s.call(ctx, tool, token, "tools/list", params, &result); err != nil {
			return nil, err
		}
		for _, t := range result.Tools {
			out = append(out, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
				ReadOnly: t.Annotations.ReadOnly, Destructive: t.Annotations.Destructive})
		}
		if result.NextCursor == "" {
			return out, nil
		}
		cursor = result.NextCursor
	}
	return out, nil
}

// call is one JSON-RPC request to the tool's MCP endpoint, answered as JSON; no Origin, as a server would send.
func (s *Service) call(ctx context.Context, tool tools.Tool, token, method string, params any, into any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tool.Resource, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", config.MCPProtocolVersion)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s refused the token with %d", ErrNotConnected, tool.Name, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %s with %d", ErrUnreachable, tool.Name, method, resp.StatusCode)
	}
	var answer struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, config.MaxCatalogBytes)).Decode(&answer); err != nil {
		return fmt.Errorf("%w: %s answered %s with something other than JSON", ErrUnreachable, tool.Name, method)
	}
	if answer.Error != nil {
		return fmt.Errorf("%w: %s refused %s: %s", ErrUnreachable, tool.Name, method, answer.Error.Message)
	}
	return json.Unmarshal(answer.Result, into)
}
