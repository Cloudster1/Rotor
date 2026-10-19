package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/usage"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Runs starts, lists and cancels runs as the caller; *runs.Service is the real one.
type Runs interface {
	Start(ctx context.Context, req runs.StartRequest) (runs.Run, bool, error)
	Get(ctx context.Context, id uuid.UUID) (runs.Run, error)
	List(ctx context.Context, f runs.Filter) ([]runs.Run, error)
	Cancel(ctx context.Context, id uuid.UUID) (runs.Run, error)
	Follow(ctx context.Context, id uuid.UUID, after int64, to runs.Stream) error
	Graph(ctx context.Context, r runs.Run) (spec.Graph, error)
	LinkHosts(ctx context.Context, r runs.Run) []string
}

// idempotencyHeader names the header a client retries a start with.
const idempotencyHeader = "Idempotency-Key"

var (
	errRunsUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "runs_unavailable",
		Message: "Runs are not set up on this installation. An operator connects Rotor to OpenBao and starts its worker."}
	errRunNotFound   = ErrNotFound("There is no such run, or you may not see it.")
	errNotRunManager = &APIError{Status: http.StatusForbidden, Code: "not_run_manager",
		Message: "Only the person who started the run, its workflow's owner or an administrator may cancel it. To stop it, reject its approval."}
	errRunEnded         = &APIError{Status: http.StatusConflict, Code: "run_ended", Message: "The run has already ended, so there is nothing to cancel."}
	errKeyReused        = ErrValidation(map[string]string{idempotencyHeader: "This key already started a run of another workflow. Send a new key for each run."})
	errNoRunsOfArchived = &APIError{Status: http.StatusConflict, Code: "workflow_archived",
		Message: "This workflow is archived, so it starts no runs. Ask its owner, or save a copy as a workflow of your own."}
)

// runError says what went wrong with a run in the words its caller needs.
func runError(err error) error {
	var refused *runs.InputsRefused
	var needs *runs.NeedsConnections
	var quota *usage.Refused
	switch {
	case errors.As(err, &quota):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: quota.Code, Message: quota.Message}
	case errors.As(err, &refused):
		fields := map[string]string{}
		for _, p := range refused.Problems {
			where := "inputs" + p.Path
			if _, taken := fields[where]; !taken {
				fields[where] = p.Message
			}
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "inputs_invalid", Fields: fields,
			Message: "The inputs do not fit the workflow. " + refused.Problems[0].Message}
	case errors.As(err, &needs):
		// Fields names each tool to connect, with its id; an empty id is a tool this installation does not have.
		fields, names := map[string]string{}, ""
		for i, t := range needs.Tools {
			id := ""
			if t.ID != uuid.Nil {
				id = t.ID.String()
			}
			fields[t.Name] = id
			if i > 0 {
				names += ", "
			}
			names += t.Name
		}
		return &APIError{Status: http.StatusConflict, Code: "connections_needed", Fields: fields,
			Message: fmt.Sprintf("This workflow uses %s as you, so connect it first, then start the run again.", names)}
	case errors.Is(err, runs.ErrNotFound):
		return errRunNotFound
	case errors.Is(err, runs.ErrEnded):
		return errRunEnded
	case errors.Is(err, runs.ErrNotManager):
		return errNotRunManager
	case errors.Is(err, runs.ErrKeyReused):
		return errKeyReused
	case errors.Is(err, runs.ErrArchived):
		return errNoRunsOfArchived
	}
	return workflowError(err)
}

func toRunResponse(r runs.Run) runResponse {
	output := r.Output
	if len(output) == 0 {
		output = json.RawMessage("null")
	}
	var approval *string
	if r.ApprovalID != nil {
		id := r.ApprovalID.String()
		approval = &id
	}
	var trigger *runTrigger
	if r.TriggerID != nil {
		trigger = &runTrigger{ID: r.TriggerID.String(), Name: r.TriggerName}
	}
	return runResponse{ID: r.ID.String(), ApprovalID: approval, Trigger: trigger,
		Usage: runUsage{InputTokens: r.InputTokens, OutputTokens: r.OutputTokens, Cost: r.Cost}, Workflow: personRef{ID: r.WorkflowID.String(), Name: r.WorkflowName}, Version: r.Version,
		Status: runStatus(r.Status), StartedBy: personRef{ID: r.UserID.String(), Name: r.StartedByName}, Reason: r.Reason,
		Inputs: r.Inputs, Output: output, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, Ended: runnerctl.Ended(r.Status)}
}

func (s *Server) runsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Runs == nil {
		respondError(w, r, errRunsUnavailable)
		return false
	}
	return true
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	key := r.Header.Get(idempotencyHeader)
	if len(key) > config.MaxIdempotencyKey {
		respondError(w, r, ErrValidation(map[string]string{idempotencyHeader: fmt.Sprintf("The key is longer than %d characters.", config.MaxIdempotencyKey)}))
		return
	}
	var req startRunRequest
	if err := decodeJSONWithin(w, r, &req, config.MaxRunInputBytes); err != nil {
		respondError(w, r, err)
		return
	}
	if len(req.Inputs) == 0 || string(req.Inputs) == "null" {
		req.Inputs = json.RawMessage("{}")
	}
	if !s.runsReady(w, r) {
		return
	}
	run, created, err := s.Runs.Start(r.Context(), runs.StartRequest{WorkflowID: id, Version: req.Version, Inputs: req.Inputs, IdempotencyKey: key})
	if err != nil {
		respondError(w, r, runError(err))
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	respondJSON(w, r, status, toRunResponse(run))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := runs.Filter{Statuses: q["status"], Mine: q.Get("mine") == "true"}
	if raw := q.Get("workflow"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("That is not a valid workflow id. Check the filter."))
			return
		}
		f.WorkflowID = id
	}
	for _, status := range f.Statuses {
		if !validRunStatus(status) {
			respondError(w, r, ErrBadRequest(fmt.Sprintf("%q is not a run state. Use queued, starting, running, waiting, succeeded, failed, cancelled or expired.", status)))
			return
		}
	}
	if !s.runsReady(w, r) {
		return
	}
	list, err := s.Runs.List(r.Context(), f)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]runResponse, 0, len(list))
	for _, run := range list {
		out = append(out, toRunResponse(run))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"runs": out})
}

func validRunStatus(status string) bool {
	return slices.Contains(m2Enums[reflect.TypeOf(runStatus(""))], status)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "runID", "run")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.runsReady(w, r) {
		return
	}
	run, err := s.Runs.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, runError(err))
		return
	}
	out := toRunResponse(run)
	// A version that no longer reads still shows its run, without the graph.
	if graph, err := s.Runs.Graph(r.Context(), run); err == nil {
		out.Graph = &graph
	}
	out.LinkHosts = s.Runs.LinkHosts(r.Context(), run)
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "runID", "run")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.runsReady(w, r) {
		return
	}
	run, err := s.Runs.Cancel(r.Context(), id)
	if err != nil {
		respondError(w, r, runError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toRunResponse(run))
}
