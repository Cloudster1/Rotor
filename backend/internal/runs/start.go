package runs

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// StartRequest starts a run; Version 0 pins the workflow's current version, and IdempotencyKey may be empty.
type StartRequest struct {
	WorkflowID     uuid.UUID
	Version        int
	Inputs         []byte
	IdempotencyKey string
	// TriggerID names the trigger of the caller's that starts the run (#34); the database refuses anyone else's.
	TriggerID uuid.UUID
}

// Start queues a run of a workflow the caller may see, as the caller. A key the caller used before answers that
// run and created false, so a retried request starts nothing twice.
func (s *Service) Start(ctx context.Context, req StartRequest) (Run, bool, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Run{}, false, connections.ErrNoSession
	}
	if req.IdempotencyKey != "" {
		if earlier, err := s.byKey(ctx, req); !errors.Is(err, ErrNotFound) {
			return earlier, false, err
		}
	}
	w, version, err := s.pinned(ctx, req)
	if err != nil {
		return Run{}, false, err
	}
	parsed, problems := spec.Parse([]byte(version.SpecYAML))
	if parsed == nil || len(problems) > 0 {
		return Run{}, false, &InputsRefused{Problems: []spec.Problem{{Code: spec.CodeInputs,
			Message: "This version of the workflow no longer reads; its owner must save a version that does."}}}
	}
	if problems := parsed.CheckInputs(req.Inputs); len(problems) > 0 {
		return Run{}, false, &InputsRefused{Problems: problems}
	}
	if err := s.Connected(ctx, parsed); err != nil {
		return Run{}, false, err
	}
	if s.Usage != nil {
		if err := s.Usage.CheckStart(ctx, p.UserID, parsed); err != nil {
			return Run{}, false, err
		}
	}
	var r Run
	created := true
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		r, err = scanRun(tx.QueryRow(ctx, `WITH started AS (
				INSERT INTO run (workflow_id, version, user_id, inputs, idempotency_key, workflow_name, started_by_name, trigger_id)
				VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8)
				ON CONFLICT (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
				RETURNING *)
			SELECT `+runColumns+` FROM started r`,
			w.ID, version.Number, p.UserID, req.Inputs, req.IdempotencyKey, w.Name, p.Name, nullUUID(req.TriggerID)))
		if errors.Is(err, ErrNotFound) {
			// Another request with the same key won the race; it answers instead.
			created = false
			return nil
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRunStarted, TargetType: "run", TargetID: r.ID.String(), Actor: p.UserID,
			Data: startedData(w, version.Number, req.TriggerID)})
	})
	if err != nil || created {
		return r, created, err
	}
	r, err = s.byKey(ctx, req)
	return r, false, err
}

// byKey is the run the caller started with the request's key; one of another workflow is refused.
func (s *Service) byKey(ctx context.Context, req StartRequest) (Run, error) {
	var r Run
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		r, err = scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM run r
			WHERE r.user_id = current_actor_id() AND r.idempotency_key = $1`, req.IdempotencyKey))
		return err
	})
	if err == nil && r.WorkflowID != req.WorkflowID {
		return Run{}, ErrKeyReused
	}
	return r, err
}

// pinned is the workflow as the caller sees it and the version the run executes.
func (s *Service) pinned(ctx context.Context, req StartRequest) (workflows.Workflow, workflows.Version, error) {
	w, current, err := s.workflows.Get(ctx, req.WorkflowID)
	if err != nil {
		return w, current, err
	}
	if w.ArchivedAt != nil {
		return w, current, ErrArchived
	}
	if req.Version == 0 || req.Version == current.Number {
		return w, current, nil
	}
	v, err := s.workflows.Version(ctx, req.WorkflowID, req.Version)
	return w, v, err
}

// Connected refuses a run whose bindings name a tool the caller has no working connection to, with every scope the
// binding asks for; the run would otherwise fail at its first call.
func (s *Service) Connected(ctx context.Context, parsed *spec.Spec) error {
	mine, err := s.connections.List(ctx)
	if err != nil {
		return err
	}
	consent := map[uuid.UUID][]string{}
	for _, c := range mine {
		if c.Status == connections.StatusConnected {
			consent[c.ToolID] = c.Scopes
		}
	}
	missing := map[string]MissingTool{}
	for _, b := range parsed.Tools {
		tool, err := s.tools.ByName(ctx, b.Tool)
		switch {
		case errors.Is(err, tools.ErrNotFound):
			missing[b.Tool] = MissingTool{Name: b.Tool}
		case err != nil:
			return err
		default:
			scopes, ok := consent[tool.ID]
			if !ok || !connections.Covers(scopes, b.Scopes) {
				missing[b.Tool] = MissingTool{ID: tool.ID, Name: tool.Name}
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	out := make([]MissingTool, 0, len(missing))
	for _, t := range missing {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return &NeedsConnections{Tools: out}
}

// startedData is what run.started records: the workflow, its version and, for a triggered run, the trigger.
func startedData(w workflows.Workflow, version int, trigger uuid.UUID) map[string]any {
	data := map[string]any{"workflow": w.ID.String(), "name": w.Name, "version": version}
	if trigger != uuid.Nil {
		data["trigger"] = trigger.String()
	}
	return data
}
