// Package tools keeps the tools Rotor acts in: what each offers, and Rotor's OAuth client there. The client
// secret goes to OpenBao; the rest is a row everyone signed in may read and administrators alone may change.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/tools/profiles"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Errors a caller tells apart; a *Refusal says in a sentence why a tool cannot be added.
var (
	ErrNotFound    = errors.New("no such tool")
	ErrUnreachable = errors.New("the tool did not answer")
)

// Refusal is a tool Rotor will not add, with the sentence an administrator reads.
type Refusal struct {
	Message string
	// cause is what the refusal rests on, such as a *Missing capability.
	cause error
}

func (r *Refusal) Error() string { return r.Message }
func (r *Refusal) Unwrap() error { return r.cause }

func refuse(format string, args ...any) error { return &Refusal{Message: fmt.Sprintf(format, args...)} }

// title is a product's name as a sentence writes it: exciter is Exciter.
func title(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// Statuses and registrations, as the table and the API name them.
const (
	StatusReady             = "ready"
	StatusNeedsRegistration = "needs_registration"
	// StatusAwaitingApproval is a client the tool holds pending until its administrator allows Rotor (#45).
	StatusAwaitingApproval = "awaiting_approval"
	RegistrationDynamic    = "dynamic"
	RegistrationManual     = "manual"
)

// Tool is a tool Rotor knows.
type Tool struct {
	ID       uuid.UUID
	Name     string
	BaseURL  string
	Resource string
	// MCPURL is where Rotor calls MCP: the resource itself at Exciter, under it at Armature.
	MCPURL                string
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RevocationEndpoint    string
	RegistrationEndpoint  string
	ScopesSupported       []string
	Capabilities          []string
	// GrantTypes are what the tool's authorization server offers (RFC 8414).
	GrantTypes       []string
	ClientID         string
	ClientAuthMethod string
	Registration     string
	Status           string
	AddedAt          time.Time
}

// LinkHosts are the hosts a person reaches the tool at, from its address and its MCP resource: the only ones a run's
// agent may write links to (#40).
func (t Tool) LinkHosts() []string {
	var out []string
	for _, address := range []string{t.BaseURL, t.MCPURL} {
		u, err := url.Parse(address)
		if err != nil || u.Hostname() == "" {
			continue
		}
		if host := strings.ToLower(u.Hostname()); !slices.Contains(out, host) {
			out = append(out, host)
		}
	}
	return out
}

// Profile is how the tool names its scopes and hands out tokens, by the product it is.
func (t Tool) Profile() profiles.Profile { return profiles.For(t.Name) }

// OffersExchange says whether a run's token is exchanged from the grant's (RFC 8693); only a tool with one token per
// grant does that, since Exciter's exchange trades the identity provider's tokens for partner apps instead (#139).
func (t Tool) OffersExchange() bool {
	return exchangesGrantTokens(t.Name, t.GrantTypes)
}

// exchangesGrantTokens says whether the product's exchange, where its server offers one, trades its own grant's tokens.
func exchangesGrantTokens(product string, grantTypes []string) bool {
	return profiles.For(product).SharedGrantToken && slices.Contains(grantTypes, GrantTokenExchange)
}

// Secrets is what the registry keeps in OpenBao; *vault.Client is the real one.
type Secrets interface {
	ToolClient(ctx context.Context, toolID uuid.UUID) (vault.ToolClient, int, error)
	PutToolClient(ctx context.Context, toolID uuid.UUID, c vault.ToolClient, version int) (int, error)
	DeleteToolClient(ctx context.Context, toolID uuid.UUID) error
	// DeleteWebhookSecret forgets the secret the tool signs its webhook deliveries with (#35).
	DeleteWebhookSecret(ctx context.Context, toolID uuid.UUID) error
}

// ConnectionEnder ends every connection to a tool before it goes, revoking them at the tool.
type ConnectionEnder interface {
	EndToolConnections(ctx context.Context, tool Tool) (int, error)
}

// Service is the registry.
type Service struct {
	db    *db.Cluster
	vault Secrets
	http  *http.Client
	// redirectURL is the one callback Rotor registers at every tool.
	redirectURL string
	// allowHTTP accepts plain http beyond the loopback, in development only.
	allowHTTP bool
	// Connections is set once connections exist, so removing a tool ends them first.
	Connections ConnectionEnder
}

// NewService returns the registry; client reaches the tools, rewriting their public addresses where needed.
func NewService(cluster *db.Cluster, secrets Secrets, client *http.Client, redirectURL string, allowHTTP bool) *Service {
	return &Service{db: cluster, vault: secrets, http: client, redirectURL: redirectURL, allowHTTP: allowHTTP}
}

const toolColumns = `id, name, base_url, resource, mcp_url, issuer, authorization_endpoint, token_endpoint,
	COALESCE(revocation_endpoint, ''), COALESCE(registration_endpoint, ''), scopes_supported, capabilities, grant_types,
	client_id, client_auth_method, registration, status, created_at`

func scanTool(row pgx.Row) (Tool, error) {
	var t Tool
	err := row.Scan(&t.ID, &t.Name, &t.BaseURL, &t.Resource, &t.MCPURL, &t.Issuer, &t.AuthorizationEndpoint, &t.TokenEndpoint,
		&t.RevocationEndpoint, &t.RegistrationEndpoint, &t.ScopesSupported, &t.Capabilities, &t.GrantTypes,
		&t.ClientID, &t.ClientAuthMethod, &t.Registration, &t.Status, &t.AddedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// List is every tool, by name, as the person on the context may see them.
func (s *Service) List(ctx context.Context) ([]Tool, error) {
	out := []Tool{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+toolColumns+` FROM tool ORDER BY name, base_url`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTool(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Get is one tool, as the person on the context may see it.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Tool, error) {
	var t Tool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		t, err = scanTool(tx.QueryRow(ctx, `SELECT `+toolColumns+` FROM tool WHERE id = $1`, id))
		return err
	})
	return t, err
}

// Lookup is one tool for the control plane, outside any person's view: for connecting and minting.
func (s *Service) Lookup(ctx context.Context, id uuid.UUID) (Tool, error) {
	var t Tool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		t, err = scanTool(tx.QueryRow(ctx, `SELECT `+toolColumns+` FROM tool WHERE id = $1`, id))
		return err
	})
	return t, err
}

// ByName is the tool a workflow's binding names, for the control plane; ErrNotFound when none or several have it.
func (s *Service) ByName(ctx context.Context, name string) (Tool, error) {
	var found []Tool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+toolColumns+` FROM tool WHERE name = $1`, name)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTool(rows)
			if err != nil {
				return err
			}
			found = append(found, t)
		}
		return rows.Err()
	})
	if err != nil {
		return Tool{}, err
	}
	if len(found) != 1 {
		return Tool{}, fmt.Errorf("%w: %d tools are called %s", ErrNotFound, len(found), name)
	}
	return found[0], nil
}

// Client is Rotor's client at a tool, secret included, for the token endpoint.
func (s *Service) Client(ctx context.Context, id uuid.UUID) (vault.ToolClient, error) {
	c, _, err := s.vault.ToolClient(ctx, id)
	return c, err
}

// MarkNeedsRegistration records that the tool no longer knows Rotor's client, so an administrator registers again.
func (s *Service) MarkNeedsRegistration(ctx context.Context, id uuid.UUID) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE tool SET status = $2 WHERE id = $1`, id, StatusNeedsRegistration)
		return err
	})
}
