package tracedest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// What a workflow keeps of its runs' text (#259): as the installation says, or none whatever it says.
const (
	ContentInherit = "inherit"
	ContentNone    = "none"
)

var (
	// ErrNoWorkflow is a workflow the caller does not read, or none at all.
	ErrNoWorkflow = errors.New("no such workflow")
	// ErrNotAdministrator is a change by someone who reads the workflow but does not administer it.
	ErrNotAdministrator = errors.New("changing a workflow's tracing takes administering it")
)

// Sentences the person choosing reads beside the field.
var (
	contentSentence      = "Choose inherit, to keep text as the installation does, or none, to keep no text of this workflow's runs."
	ratioSentence        = "Send a share of runs from 0 to 1, such as 0.25 for one run in four."
	destinationsSentence = fmt.Sprintf("Choose among the trace destinations listed, each once, at most %d.", config.MaxTraceDestinations)
)

// WorkflowTracing is what a workflow chose of its runs' traces; ChangedBy and UpdatedAt are nil while it has the defaults.
type WorkflowTracing struct {
	Content string
	// Destinations names the only ones its runs go to; nil sends to every enabled one, ones added later too.
	Destinations *[]uuid.UUID
	SampleRatio  float64
	ChangedBy    *string
	UpdatedAt    *time.Time
}

// Offered is a destination a workflow may choose, by name alone: its endpoint and headers are an administrator's.
type Offered struct {
	ID               uuid.UUID
	Name             string
	Enabled, Content bool
}

// WorkflowView is a workflow's choice and what it is made against, so the person choosing knows what inherit means.
type WorkflowView struct {
	Tracing      WorkflowTracing
	KeepContent  bool
	Destinations []Offered
	CanChange    bool
}

// Workflow is a workflow's tracing for whoever reads the workflow.
func (s *Service) Workflow(ctx context.Context, workflowID uuid.UUID) (WorkflowView, error) {
	canChange, err := s.administers(ctx, workflowID)
	if err != nil {
		return WorkflowView{}, err
	}
	view := WorkflowView{CanChange: canChange, Tracing: WorkflowTracing{Content: ContentInherit, SampleRatio: 1}}
	// The installation's setting, the destinations and who changed the row are read as rotor_admin, since a workflow's
	// administrator need not be the installation's; what is answered of them is named in the contract.
	err = s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT keep_content FROM trace_setting`).Scan(&view.KeepContent); err != nil {
			return err
		}
		if view.Destinations, err = offered(ctx, tx); err != nil {
			return err
		}
		var chosen bool
		var ids []uuid.UUID
		t := &view.Tracing
		err := tx.QueryRow(ctx, `SELECT t.content, t.destinations IS NOT NULL, coalesce(t.destinations, '{}'), t.sample_ratio,
			(SELECT u.name FROM app_user u WHERE u.id = t.changed_by), t.updated_at FROM workflow_tracing t WHERE t.workflow_id = $1`,
			workflowID).Scan(&t.Content, &chosen, &ids, &t.SampleRatio, &t.ChangedBy, &t.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || !chosen {
			return err
		}
		// A destination removed since drops out of the choice.
		kept := slices.DeleteFunc(ids, func(id uuid.UUID) bool {
			return !slices.ContainsFunc(view.Destinations, func(o Offered) bool { return o.ID == id })
		})
		t.Destinations = &kept
		return nil
	})
	return view, err
}

// SetWorkflow changes a workflow's tracing as one of its administrators, audited; the database refuses anyone else.
func (s *Service) SetWorkflow(ctx context.Context, workflowID uuid.UUID, in WorkflowTracing) (WorkflowView, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return WorkflowView{}, auth.ErrNoSession
	}
	canChange, err := s.administers(ctx, workflowID)
	if err != nil {
		return WorkflowView{}, err
	}
	if !canChange {
		return WorkflowView{}, ErrNotAdministrator
	}
	var all []Offered
	if err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		all, err = offered(ctx, tx)
		return err
	}); err != nil {
		return WorkflowView{}, err
	}
	names, err := checkWorkflow(in, all)
	if err != nil {
		return WorkflowView{}, err
	}
	var ids any
	if in.Destinations != nil {
		ids = *in.Destinations
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO workflow_tracing (workflow_id, content, destinations, sample_ratio, changed_by)
			VALUES ($1, $2, $3::uuid[], $4, $5) ON CONFLICT (workflow_id) DO UPDATE SET content = EXCLUDED.content,
			destinations = EXCLUDED.destinations, sample_ratio = EXCLUDED.sample_ratio, changed_by = EXCLUDED.changed_by`,
			workflowID, in.Content, ids, in.SampleRatio, p.UserID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionWorkflowTracingChanged, TargetType: "workflow",
			TargetID: workflowID.String(), Actor: p.UserID,
			Data: map[string]any{"content": in.Content, "destinations": names, "sampleRatio": in.SampleRatio}})
	})
	if err != nil {
		return WorkflowView{}, err
	}
	return s.Workflow(ctx, workflowID)
}

// administers says whether the caller administers a workflow they read; one they do not read is ErrNoWorkflow.
func (s *Service) administers(ctx context.Context, workflowID uuid.UUID) (bool, error) {
	var may bool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT actor_can('workflow.administer', w.id) FROM workflow w WHERE w.id = $1`, workflowID).Scan(&may)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNoWorkflow
	}
	return may, err
}

// offered is every destination by name, as rotor_admin reads them.
func offered(ctx context.Context, tx db.DBTX) ([]Offered, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, enabled, content FROM trace_destination ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Offered{}
	for rows.Next() {
		var o Offered
		if err := rows.Scan(&o.ID, &o.Name, &o.Enabled, &o.Content); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// checkWorkflow refuses a choice the contract does not allow, and answers the chosen destinations' names for the
// audit log, nil for all of them.
func checkWorkflow(in WorkflowTracing, all []Offered) ([]string, error) {
	fields := map[string]string{}
	if in.Content != ContentInherit && in.Content != ContentNone {
		fields["content"] = contentSentence
	}
	if math.IsNaN(in.SampleRatio) || in.SampleRatio < 0 || in.SampleRatio > 1 {
		fields["sampleRatio"] = ratioSentence
	}
	var names []string
	if in.Destinations != nil {
		names = []string{}
		chosen := *in.Destinations
		for i, id := range chosen {
			at := slices.IndexFunc(all, func(o Offered) bool { return o.ID == id })
			if at < 0 || slices.Index(chosen, id) != i || len(chosen) > config.MaxTraceDestinations {
				fields["destinations"] = destinationsSentence
				break
			}
			names = append(names, all[at].Name)
		}
	}
	if len(fields) > 0 {
		return nil, &Invalid{Fields: fields}
	}
	return names, nil
}
