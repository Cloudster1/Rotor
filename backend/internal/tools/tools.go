// Package tools keeps the tools Rotor acts in: what each offers, and Rotor's OAuth client there. The client
// secret goes to OpenBao; the rest is a row everyone signed in may read and administrators alone may change.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Errors a caller tells apart; a *Refusal says in a sentence why a tool cannot be added.
var (
	ErrNotFound    = errors.New("no such tool")
	ErrUnreachable = errors.New("the tool did not answer")
)

// Refusal is a tool Rotor will not add, with the sentence an administrator reads.
type Refusal struct{ Message string }

func (r *Refusal) Error() string { return r.Message }

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
	RegistrationDynamic     = "dynamic"
	RegistrationManual      = "manual"
)

// Tool is a tool Rotor knows.
type Tool struct {
	ID                    uuid.UUID
	Name                  string
	BaseURL               string
	Resource              string
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RevocationEndpoint    string
	RegistrationEndpoint  string
	ScopesSupported       []string
	Capabilities          []string
	ClientID              string
	ClientAuthMethod      string
	Registration          string
	Status                string
	AddedAt               time.Time
}

// Secrets is what the registry keeps in OpenBao; *vault.Client is the real one.
type Secrets interface {
	ToolClient(ctx context.Context, toolID uuid.UUID) (vault.ToolClient, int, error)
	PutToolClient(ctx context.Context, toolID uuid.UUID, c vault.ToolClient, version int) (int, error)
	DeleteToolClient(ctx context.Context, toolID uuid.UUID) error
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

const toolColumns = `id, name, base_url, resource, issuer, authorization_endpoint, token_endpoint,
	COALESCE(revocation_endpoint, ''), COALESCE(registration_endpoint, ''), scopes_supported, capabilities,
	client_id, client_auth_method, registration, status, created_at`

func scanTool(row pgx.Row) (Tool, error) {
	var t Tool
	err := row.Scan(&t.ID, &t.Name, &t.BaseURL, &t.Resource, &t.Issuer, &t.AuthorizationEndpoint, &t.TokenEndpoint,
		&t.RevocationEndpoint, &t.RegistrationEndpoint, &t.ScopesSupported, &t.Capabilities,
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
