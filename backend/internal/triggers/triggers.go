package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// Kinds of trigger.
const (
	KindSchedule = "schedule"
	KindEvent    = "event"
)

// Firing outcomes.
const (
	OutcomeStarted = "started"
	OutcomeSkipped = "skipped"
	OutcomeRefused = "refused"
	OutcomeFailed  = "failed"
)

// ErrNotFound is a trigger that does not exist or that the caller may not see.
var ErrNotFound = errors.New("no such trigger")

// Trigger is a trigger as its owner sees it.
type Trigger struct {
	ID             uuid.UUID
	WorkflowID     uuid.UUID
	WorkflowName   string
	OwnerID        uuid.UUID
	OwnerName      string
	Name           string
	Kind           string
	Enabled        bool
	DisabledReason string
	DisabledDetail string
	DisabledAt     *time.Time
	Cron           string
	TimeZone       string
	NoOverlap      bool
	NextRunAt      *time.Time
	ToolID         *uuid.UUID
	Topics         []string
	Projects       []string
	Conditions     json.RawMessage
	Inputs         json.RawMessage
	LastFiredAt    *time.Time
	LastOutcome    string
	LastReason     string
	LastRunID      *uuid.UUID
	LastRunStatus  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Input makes or changes a trigger; the fields of the other kind are ignored.
type Input struct {
	Name       string
	Kind       string
	Cron       string
	TimeZone   string
	NoOverlap  bool
	ToolID     uuid.UUID
	Topics     []string
	Projects   []string
	Conditions json.RawMessage
	Inputs     json.RawMessage
	// Firing says the trigger is checked as it fires, not as a person makes or turns it on.
	Firing bool
}

// Workflows is the workflow as the person on the context sees it; *workflows.Service is the real one.
type Workflows interface {
	Get(ctx context.Context, id uuid.UUID) (workflows.Workflow, workflows.Version, error)
}

// Catalogs reads what a spec's tools say of their calls, as the person on the context sees them.
type Catalogs interface {
	For(ctx context.Context, s *spec.Spec) (spec.Catalog, error)
}

// Runs starts a trigger's runs and tells whether its owner has every connection; *runs.Service is the real one.
type Runs interface {
	Start(ctx context.Context, req runs.StartRequest) (runs.Run, bool, error)
	Connected(ctx context.Context, parsed *spec.Spec) error
}

// Service keeps triggers as the person on the context; the worker's passes start their runs.
type Service struct {
	db        *db.Cluster
	workflows Workflows
	catalogs  Catalogs
	runs      Runs
	// CheckEvent checks an event trigger's tool and filters (#35); nil refuses event triggers.
	CheckEvent func(ctx context.Context, in Input) []Problem
	// Now is the clock next runs are reckoned from; nil is the real one.
	Now func() time.Time
}

// NewService returns the triggers.
func NewService(cluster *db.Cluster, wf Workflows, catalogs Catalogs, starter Runs) *Service {
	return &Service{db: cluster, workflows: wf, catalogs: catalogs, runs: starter}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

const columns = `t.id, t.workflow_id, COALESCE((SELECT w.name FROM workflow w WHERE w.id = t.workflow_id), ''), t.owner_id,
	COALESCE((SELECT u.name FROM app_user u WHERE u.id = t.owner_id), ''), t.name, t.kind, t.enabled, t.disabled_reason,
	t.disabled_detail, t.disabled_at, COALESCE(t.cron, ''), COALESCE(t.time_zone, ''), t.no_overlap, t.next_run_at, t.tool_id,
	t.topics, t.projects, t.conditions, t.inputs, t.last_fired_at, t.last_outcome, t.last_reason, t.last_run_id,
	(SELECT r.status FROM run r WHERE r.id = t.last_run_id), t.created_at, t.updated_at`

func scan(row pgx.Row) (Trigger, error) {
	var t Trigger
	err := row.Scan(&t.ID, &t.WorkflowID, &t.WorkflowName, &t.OwnerID, &t.OwnerName, &t.Name, &t.Kind, &t.Enabled, &t.DisabledReason,
		&t.DisabledDetail, &t.DisabledAt, &t.Cron, &t.TimeZone, &t.NoOverlap, &t.NextRunAt, &t.ToolID,
		&t.Topics, &t.Projects, &t.Conditions, &t.Inputs, &t.LastFiredAt, &t.LastOutcome, &t.LastReason, &t.LastRunID,
		&t.LastRunStatus, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// Get is one of the caller's triggers, or any for an administrator.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Trigger, error) {
	var t Trigger
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		t, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM trigger t WHERE t.id = $1`, id))
		return err
	})
	return t, err
}

// List is the caller's triggers, newest first; on one workflow, an administrator's list has everyone's.
func (s *Service) List(ctx context.Context, workflowID uuid.UUID) ([]Trigger, error) {
	out := []Trigger{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+columns+` FROM trigger t
			WHERE CASE WHEN $1::uuid IS NULL THEN t.owner_id = current_actor_id() ELSE t.workflow_id = $1 END
			ORDER BY t.created_at DESC, t.id DESC LIMIT $2`, nullUUID(workflowID), config.TriggerListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

func nullUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
