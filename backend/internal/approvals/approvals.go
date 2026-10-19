// Package approvals lists approvals and decides them (#31): the run's starter, or the members of the group its node
// names, approve or reject; the run then goes on or ends. docs/api-contract-m5.md says who sees and who decides.
package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
)

// Approval states.
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)

// ErrNotFound is an approval that does not exist or that the caller may not see.
var ErrNotFound = errors.New("no such approval")

// Approval is an approval as the people who may see it see it.
type Approval struct {
	ID            uuid.UUID
	RunID         uuid.UUID
	StarterID     uuid.UUID
	WorkflowID    uuid.UUID
	WorkflowName  string
	StartedByName string
	Node          string
	Message       string
	Status        string
	// Group decides when set; the starter decides otherwise.
	Group       *string
	Preview     json.RawMessage
	FormSchema  json.RawMessage
	FormValues  json.RawMessage
	CanDecide   bool
	RequestedAt time.Time
	ExpiresAt   time.Time
	DecidedAt   *time.Time
	DecidedBy   *uuid.UUID
	// DecidedByName, Comment and ApprovedValues are set once a person decided it.
	DecidedByName  string
	Comment        string
	ApprovedValues json.RawMessage
}

// Publisher tells the api replicas that something changed for a run; *cache.Cache is the real one.
type Publisher interface {
	Publish(ctx context.Context, channel string, message []byte)
}

// Service reads and decides approvals as the person on the context.
type Service struct {
	db      *db.Cluster
	keys    *envelope.Keys
	signals Publisher
}

// NewService returns the approvals; signals may be nil, and a run's followers then see a decision on their next beat.
func NewService(cluster *db.Cluster, keys *envelope.Keys, signals Publisher) *Service {
	return &Service{db: cluster, keys: keys, signals: signals}
}

// mayDecide says, in SQL, whether the caller decides a.
const mayDecide = `(a.status = 'pending' AND a.expires_at > now() AND CASE WHEN a.approver_group IS NULL
	THEN a.starter_id = current_actor_id() ELSE a.approver_group = ANY (current_actor_groups()) END)`

const columns = `a.id, a.run_id, a.starter_id, a.workflow_id, a.workflow_name, a.started_by_name, a.node, a.message, a.status,
	a.approver_group, a.preview, a.form_schema, a.form_values, ` + mayDecide + `, a.requested_at, a.expires_at, a.decided_at,
	a.decided_by, a.decided_by_name, a.comment, a.approved_values`

func scan(row pgx.Row) (Approval, error) {
	var a Approval
	err := row.Scan(&a.ID, &a.RunID, &a.StarterID, &a.WorkflowID, &a.WorkflowName, &a.StartedByName, &a.Node, &a.Message, &a.Status,
		&a.Group, &a.Preview, &a.FormSchema, &a.FormValues, &a.CanDecide, &a.RequestedAt, &a.ExpiresAt, &a.DecidedAt,
		&a.DecidedBy, &a.DecidedByName, &a.Comment, &a.ApprovedValues)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Get is an approval the caller may see: of a run they may see, or one their group decides.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Approval, error) {
	var a Approval
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		a, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM approval a WHERE a.id = $1`, id))
		return err
	})
	return a, err
}

// Filter narrows a list: Status is pending, decided or all; WaitingOnMe keeps those the caller may decide now.
type Filter struct {
	Status      string
	WaitingOnMe bool
}

// List is the approvals the caller may see, newest first, at most config.ApprovalListLimit.
func (s *Service) List(ctx context.Context, f Filter) ([]Approval, error) {
	out := []Approval{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+columns+` FROM approval a
			WHERE ($1 = 'all' OR ($1 = 'decided') = (a.status <> 'pending'))
			  AND (NOT $2 OR `+mayDecide+`)
			ORDER BY a.requested_at DESC, a.id DESC LIMIT $3`, statusOr(f.Status), f.WaitingOnMe, config.ApprovalListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// Count is how many approvals wait for the caller to decide them.
func (s *Service) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM approval a WHERE `+mayDecide).Scan(&n)
	})
	return n, err
}

func statusOr(status string) string {
	switch status {
	case "decided", "all":
		return status
	}
	return StatusPending
}
