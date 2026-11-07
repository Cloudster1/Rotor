package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/tracedest"
)

// WorkflowTracing is what a workflow chooses of its runs' traces (#259); *tracedest.Service is the real one.
type WorkflowTracing interface {
	Workflow(ctx context.Context, workflowID uuid.UUID) (tracedest.WorkflowView, error)
	SetWorkflow(ctx context.Context, workflowID uuid.UUID, in tracedest.WorkflowTracing) (tracedest.WorkflowView, error)
}

var errNotTracingAdministrator = ErrForbidden("Only an administrator of this workflow or of Rotor may change how its runs are traced. " +
	"Ask one of them to do it.")

// tracingContent is what a workflow keeps of its runs' text: inherit, as the installation does, or none.
type tracingContent string

// workflowTracing is what a workflow chose; changedBy and updatedAt are null while it has the defaults.
type workflowTracing struct {
	// Content is inherit, keeping text as the installation does, or none, keeping no text of the workflow's runs.
	Content tracingContent `json:"content"`
	// Destinations names the only destinations its runs go to; null sends to every enabled one.
	Destinations *[]string `json:"destinations"`
	// SampleRatio is the share of its runs sent to destinations, from 0 to 1.
	SampleRatio float64    `json:"sampleRatio"`
	ChangedBy   *string    `json:"changedBy"`
	UpdatedAt   *time.Time `json:"updatedAt"`
}

// tracingDestination is a destination a workflow may choose, by name alone.
type tracingDestination struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Content says the destination takes text, which a workflow that inherits sends it.
	Content bool `json:"content"`
}

// installationTracing is what the installation keeps, which a workflow that inherits follows.
type installationTracing struct {
	KeepContent bool `json:"keepContent"`
}

// workflowTracingResponse is a workflow's tracing and what it is chosen against.
type workflowTracingResponse struct {
	Tracing      workflowTracing      `json:"tracing"`
	Installation installationTracing  `json:"installation"`
	Destinations []tracingDestination `json:"destinations"`
	// CanChange says the caller administers the workflow, so may change its tracing.
	CanChange bool `json:"canChange"`
}

// workflowTracingRequest changes what a workflow chooses of its runs' traces.
type workflowTracingRequest struct {
	Content      tracingContent `json:"content"`
	Destinations *[]string      `json:"destinations"`
	SampleRatio  float64        `json:"sampleRatio"`
}

func (s *Server) workflowTracingReady(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return uuid.Nil, false
	}
	if s.WorkflowTracing == nil {
		respondError(w, r, errTracesUnavailable)
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleGetWorkflowTracing(w http.ResponseWriter, r *http.Request) {
	id, ok := s.workflowTracingReady(w, r)
	if !ok {
		return
	}
	view, err := s.WorkflowTracing.Workflow(r.Context(), id)
	answerWorkflowTracing(w, r, view, err)
}

func (s *Server) handleSetWorkflowTracing(w http.ResponseWriter, r *http.Request) {
	var req workflowTracingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	id, ok := s.workflowTracingReady(w, r)
	if !ok {
		return
	}
	in := tracedest.WorkflowTracing{Content: string(req.Content), SampleRatio: req.SampleRatio}
	if req.Destinations != nil {
		ids := make([]uuid.UUID, 0, len(*req.Destinations))
		for _, raw := range *req.Destinations {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				// No destination has it, so the service refuses it as one that is none.
				parsed = uuid.Nil
			}
			ids = append(ids, parsed)
		}
		in.Destinations = &ids
	}
	view, err := s.WorkflowTracing.SetWorkflow(r.Context(), id, in)
	answerWorkflowTracing(w, r, view, err)
}

func answerWorkflowTracing(w http.ResponseWriter, r *http.Request, view tracedest.WorkflowView, err error) {
	var invalid *tracedest.Invalid
	switch {
	case errors.As(err, &invalid):
		err = ErrValidation(invalid.Fields)
	case errors.Is(err, tracedest.ErrNoWorkflow):
		err = errNoSuchWorkflow
	case errors.Is(err, tracedest.ErrNotAdministrator):
		err = errNotTracingAdministrator
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	t := view.Tracing
	out := workflowTracingResponse{Tracing: workflowTracing{Content: tracingContent(t.Content), SampleRatio: t.SampleRatio, ChangedBy: t.ChangedBy,
		UpdatedAt: t.UpdatedAt}, Installation: installationTracing{KeepContent: view.KeepContent},
		Destinations: make([]tracingDestination, 0, len(view.Destinations)), CanChange: view.CanChange}
	if t.Destinations != nil {
		ids := make([]string, 0, len(*t.Destinations))
		for _, id := range *t.Destinations {
			ids = append(ids, id.String())
		}
		out.Tracing.Destinations = &ids
	}
	for _, d := range view.Destinations {
		out.Destinations = append(out.Destinations, tracingDestination{ID: d.ID.String(), Name: d.Name, Enabled: d.Enabled, Content: d.Content})
	}
	respondJSON(w, r, http.StatusOK, out)
}
