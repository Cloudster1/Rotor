package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// Errors a caller tells apart; *InputsRefused and *NeedsConnections say what to fix.
var (
	ErrNotFound = errors.New("no such run")
	// ErrKeyReused is an idempotency key the caller already used to start a run of another workflow.
	ErrKeyReused = errors.New("the idempotency key started a run of another workflow")
	// ErrArchived is a workflow that no longer starts runs.
	ErrArchived = errors.New("the workflow is archived")
)

// InputsRefused is inputs the workflow's inputs schema refuses.
type InputsRefused struct{ Problems []spec.Problem }

func (e *InputsRefused) Error() string {
	return fmt.Sprintf("the inputs have %d problems, the first: %s", len(e.Problems), e.Problems[0].Message)
}

// MissingTool is a tool a run needs a connection to; ID is nil for a tool Rotor does not have.
type MissingTool struct {
	ID   uuid.UUID
	Name string
}

// NeedsConnections is a run the caller cannot start before connecting the tools it binds.
type NeedsConnections struct{ Tools []MissingTool }

func (e *NeedsConnections) Error() string {
	names := make([]string, len(e.Tools))
	for i, t := range e.Tools {
		names[i] = t.Name
	}
	return "connect " + strings.Join(names, ", ") + " first"
}

// Run is a run as its viewers see it.
type Run struct {
	ID            uuid.UUID
	WorkflowID    uuid.UUID
	WorkflowName  string
	Version       int
	UserID        uuid.UUID
	StartedByName string
	Status        string
	Reason        string
	Inputs        json.RawMessage
	Output        json.RawMessage
	// dataKey and the sealed inputs and output are the run as stored (#41); open fills Inputs and Output from them.
	dataKey             string
	sealedIn, sealedOut []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
	StartedAt           *time.Time
	FinishedAt          *time.Time
	// ApprovalID is the pending approval the run waits for, if any.
	ApprovalID *uuid.UUID
	// TriggerID and TriggerName name the trigger that started the run, if one did; the name only for its owner.
	TriggerID   *uuid.UUID
	TriggerName *string
	// InputTokens, OutputTokens and Cost are what the run's model calls used so far (#38).
	InputTokens, OutputTokens int64
	Cost                      float64
	// Manages says the caller started the run, owns its workflow or administers Rotor, and so may cancel it; a group
	// approver sees the run without managing it.
	Manages bool
}

// Workflows is what starting a run reads of the workflow, as the caller sees it; *workflows.Service is the real one.
type Workflows interface {
	Get(ctx context.Context, id uuid.UUID) (workflows.Workflow, workflows.Version, error)
	Version(ctx context.Context, id uuid.UUID, number int) (workflows.Version, error)
}

// ToolFinder finds a binding's tool by name; *tools.Service is the real one.
type ToolFinder interface {
	ByName(ctx context.Context, name string) (tools.Tool, error)
}

// Connections is the caller's own connections; *connections.Service is the real one.
type Connections interface {
	List(ctx context.Context) ([]connections.Connection, error)
}

// Publisher tells the api replicas that something changed for a run; *cache.Cache is the real one.
type Publisher interface {
	Publish(ctx context.Context, channel string, message []byte)
}

// Signals is pub/sub across api replicas, published to and listened on; *cache.Cache is the real one.
type Signals interface {
	Publisher
	Subscribe(ctx context.Context, channel string) <-chan []byte
}

// Service starts, lists, follows and cancels runs as the person on the context; the worker carries them.
type Service struct {
	db          *db.Cluster
	keys        *envelope.Keys
	workflows   Workflows
	tools       ToolFinder
	connections Connections
	signals     Signals
	// Usage refuses a run its person's quota or its model's prices do not allow (#38); nil allows every run.
	Usage StartCheck
	// Catalogs reads the tools as the run's person sees them now, to check its writes again (#90); nil skips that.
	Catalogs Catalogs
}

// Catalogs reads what a spec's tools offer, from the tools themselves; *catalog.Service is the real one.
type Catalogs interface {
	Fresh(ctx context.Context, sp *spec.Spec) (spec.Catalog, error)
}

// ToolsChanged is a run whose workflow no longer holds against its tools as they answer now, such as a call that
// became a write behind no approval since the workflow was saved.
type ToolsChanged struct{ Problems []spec.Problem }

func (e *ToolsChanged) Error() string {
	return fmt.Sprintf("the tools changed under the workflow: %s", e.Problems[0].Message)
}

// StartCheck refuses a run its person may not start for usage; *usage.Service is the real one.
type StartCheck interface {
	CheckStart(ctx context.Context, userID uuid.UUID, sp *spec.Spec) error
}

// NewService returns the runs; signals may be nil, and streams then find a cancel or an event on their next beat.
func NewService(cluster *db.Cluster, keys *envelope.Keys, wf Workflows, finder ToolFinder, conns Connections, signals Signals) *Service {
	return &Service{db: cluster, keys: keys, workflows: wf, tools: finder, connections: conns, signals: signals}
}

const runColumns = `r.id, r.workflow_id, r.workflow_name, r.version, r.user_id, r.started_by_name, r.status, r.reason,
	r.data_key, r.sealed_inputs, r.sealed_output, r.created_at, r.updated_at, r.started_at, r.finished_at,
	(SELECT a.id FROM approval a WHERE a.run_id = r.id AND a.status = 'pending'), run_visible(r.user_id, r.workflow_id),
	r.trigger_id, (SELECT t.name FROM trigger t WHERE t.id = r.trigger_id),
	COALESCE((SELECT u.input_tokens FROM run_usage u WHERE u.run_id = r.id), 0),
	COALESCE((SELECT u.output_tokens FROM run_usage u WHERE u.run_id = r.id), 0),
	COALESCE((SELECT u.cost FROM run_usage u WHERE u.run_id = r.id), 0)::float8`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.WorkflowID, &r.WorkflowName, &r.Version, &r.UserID, &r.StartedByName, &r.Status, &r.Reason,
		&r.dataKey, &r.sealedIn, &r.sealedOut, &r.CreatedAt, &r.UpdatedAt, &r.StartedAt, &r.FinishedAt, &r.ApprovalID, &r.Manages, &r.TriggerID, &r.TriggerName, &r.InputTokens, &r.OutputTokens, &r.Cost)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// open fills in the runs' inputs and output from what is sealed of them, unwrapping the keys not in memory at once.
func (s *Service) open(ctx context.Context, runs []Run) error {
	wanted := make(map[uuid.UUID]string, len(runs))
	for _, r := range runs {
		wanted[r.ID] = r.dataKey
	}
	if len(wanted) == 0 {
		return nil
	}
	boxes, err := s.keys.ForMany(ctx, wanted)
	if err != nil {
		return err
	}
	for i := range runs {
		box := boxes[runs[i].ID]
		if runs[i].Inputs, err = box.Open(envelope.ColumnInputs, "", runs[i].sealedIn); err != nil {
			return err
		}
		if runs[i].Output, err = box.OpenOr(envelope.ColumnOutput, "", runs[i].sealedOut); err != nil {
			return err
		}
	}
	return nil
}

// Get is a run the caller may see: one they started, one of a workflow they own, one whose approval their group
// decides, or any for an administrator.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Run, error) {
	var r Run
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		r, err = scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM run r WHERE r.id = $1`, id))
		return err
	})
	if err != nil {
		return r, err
	}
	one := []Run{r}
	err = s.open(ctx, one)
	return one[0], err
}

// Filter narrows a list of runs; zero values do not narrow.
type Filter struct {
	WorkflowID uuid.UUID
	Statuses   []string
	// Mine keeps the runs the caller started, leaving out those of workflows they own.
	Mine bool
}

// List is the runs the caller manages, newest first, at most config.RunListLimit; runs seen only as an approver are
// in the approvals inbox instead.
func (s *Service) List(ctx context.Context, f Filter) ([]Run, error) {
	out, statuses := []Run{}, append([]string{}, f.Statuses...)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+runColumns+` FROM run r
			WHERE ($1::uuid IS NULL OR r.workflow_id = $1)
			  AND (cardinality($2::text[]) = 0 OR r.status = ANY($2))
			  AND (NOT $3 OR r.user_id = current_actor_id())
			  AND run_visible(r.user_id, r.workflow_id)
			ORDER BY r.created_at DESC, r.id DESC LIMIT $4`, nullUUID(f.WorkflowID), statuses, f.Mine, config.RunListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRun(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	return out, s.open(ctx, out)
}

func nullUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
