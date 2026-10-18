// Package connections keeps people's connections to the tools: the authorization code flow that makes one, ending
// one, and minting a run's access token from one. Refresh tokens live in OpenBao alone; access tokens nowhere.
package connections

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Errors a caller tells apart.
var (
	// ErrNotConnected is a person without a connection to the tool.
	ErrNotConnected = errors.New("not connected to the tool")
	// ErrReconnect is a connection the tool ended, which the person must make again.
	ErrReconnect = errors.New("the connection must be made again")
	// ErrNeedsRegistration is a tool that forgot Rotor's client, until an administrator registers again.
	ErrNeedsRegistration = errors.New("the tool must be registered again")
	// ErrUnreachable is a tool that did not answer.
	ErrUnreachable = errors.New("the tool did not answer")
	// ErrNoSession is a request without the person and session the flow is bound to.
	ErrNoSession = errors.New("no signed-in session")
)

// Statuses of a connection, as the table and the API name them.
const (
	StatusConnected = "connected"
	StatusReconnect = "reconnect"
)

// How a connection ended, for the audit log.
const (
	EndedBySelf        = "self"
	EndedByTool        = "tool"
	EndedByToolRemoved = "tool_removed"
	EndedBySwitchOff   = "switched_off"
)

// Connection is one of a person's connections, as they see it.
type Connection struct {
	ToolID      uuid.UUID
	ToolName    string
	ToolBaseURL string
	Scopes      []string
	Status      string
	ConnectedAt time.Time
	LastUsedAt  *time.Time
}

// Secrets is what connections keep in OpenBao; *vault.Client is the real one.
type Secrets interface {
	Connection(ctx context.Context, userID, toolID uuid.UUID) (vault.ConnectionSecret, int, error)
	PutConnection(ctx context.Context, userID, toolID uuid.UUID, s vault.ConnectionSecret, version int) (int, error)
	DeleteConnection(ctx context.Context, userID, toolID uuid.UUID) error
}

// Tools is what connections ask of the registry; *tools.Service is the real one.
type Tools interface {
	Lookup(ctx context.Context, id uuid.UUID) (tools.Tool, error)
	Client(ctx context.Context, id uuid.UUID) (vault.ToolClient, error)
	MarkNeedsRegistration(ctx context.Context, id uuid.UUID) error
}

// Service is people's connections.
type Service struct {
	db    *db.Cluster
	vault Secrets
	tools Tools
	http  *http.Client
	// redirectURL is the callback Rotor registered at every tool.
	redirectURL string
	log         *slog.Logger
}

// NewService returns the connections; client reaches the tools as the registry does.
func NewService(cluster *db.Cluster, secrets Secrets, registry Tools, client *http.Client, redirectURL string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{db: cluster, vault: secrets, tools: registry, http: client, redirectURL: redirectURL, log: log}
}

// List is the person's own connections, by tool name; the database shows nobody anyone else's.
func (s *Service) List(ctx context.Context) ([]Connection, error) {
	out := []Connection{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT c.tool_id, t.name, t.base_url, c.scopes, c.status, c.connected_at, c.last_used_at
			FROM connection c JOIN tool t ON t.id = c.tool_id
			WHERE c.user_id = current_actor_id() ORDER BY t.name, t.base_url`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Connection
			if err := rows.Scan(&c.ToolID, &c.ToolName, &c.ToolBaseURL, &c.Scopes, &c.Status, &c.ConnectedAt, &c.LastUsedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}
