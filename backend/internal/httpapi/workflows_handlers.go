package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// Workflows is the workflows store; *workflows.Service is the real one.
type Workflows interface {
	List(ctx context.Context, archived bool) ([]workflows.Workflow, error)
	Get(ctx context.Context, id uuid.UUID) (workflows.Workflow, workflows.Version, error)
	Versions(ctx context.Context, id uuid.UUID) ([]workflows.Version, error)
	Version(ctx context.Context, id uuid.UUID, number int) (workflows.Version, error)
	Diff(ctx context.Context, id uuid.UUID, from, to int) (workflows.Diff, error)
	Validate(ctx context.Context, source string) []spec.Problem
	Create(ctx context.Context, source, comment string) (uuid.UUID, error)
	Save(ctx context.Context, id uuid.UUID, source, comment string, base int) error
	Share(ctx context.Context, id uuid.UUID, sharing string, groups []string) error
	Archive(ctx context.Context, id uuid.UUID) error
}

var (
	errWorkflowsUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "workflows_unavailable",
		Message: "Workflows are not set up on this installation. An operator connects Rotor to its database."}
	errNotOwner = ErrForbidden("Only the workflow's owner may change it. Ask them, or save a copy as a new workflow of your own.")
	errStale    = &APIError{Status: http.StatusConflict, Code: "workflow_changed",
		Message: "Someone saved this workflow since you opened it. Reload it, then make your change again."}
	errArchived = &APIError{Status: http.StatusConflict, Code: "workflow_archived",
		Message: "This workflow is archived. Save a copy as a new workflow to change it."}
)

// workflowError says what went wrong with a workflow in the words its caller needs.
func workflowError(err error) error {
	var invalid *workflows.Invalid
	switch {
	case errors.As(err, &invalid):
		fields := map[string]string{}
		for _, p := range invalid.Problems {
			where := p.Path
			if where == "" {
				where = "/"
			}
			if _, taken := fields[where]; !taken {
				fields[where] = p.Message
			}
		}
		first := "the workflow has problems"
		if len(invalid.Problems) > 0 {
			first = invalid.Problems[0].Message
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "workflow_invalid", Fields: fields,
			Message: fmt.Sprintf("The workflow has %d problem(s) to fix before it is saved. The first: %s", len(invalid.Problems), first)}
	case errors.Is(err, workflows.ErrNotFound):
		return ErrNotFound("There is no such workflow, or it is not shared with you.")
	case errors.Is(err, workflows.ErrNotOwner):
		return errNotOwner
	case errors.Is(err, workflows.ErrStale):
		return errStale
	case errors.Is(err, workflows.ErrArchived):
		return errArchived
	}
	return err
}

func toWorkflowSummary(ctx context.Context, w workflows.Workflow) workflowSummary {
	p := auth.PrincipalFrom(ctx)
	return workflowSummary{ID: w.ID.String(), Name: w.Name, Description: w.Description,
		Owner: personRef{ID: w.OwnerID.String(), Name: w.OwnerName}, Sharing: workflowSharing(w.Sharing),
		SharedGroups: append([]string{}, w.SharedGroups...), CurrentVersion: w.CurrentVersion, UpdatedAt: w.UpdatedAt,
		Archived: w.ArchivedAt != nil, CanEdit: p != nil && p.UserID == w.OwnerID}
}

func toVersionSummary(v workflows.Version) versionSummary {
	return versionSummary{Number: v.Number, Comment: v.Comment, Hash: v.Hash, CreatedAt: v.CreatedAt,
		CreatedBy: personRef{ID: v.CreatedByID.String(), Name: v.CreatedByName}}
}

// toVersionResponse is a version with what a run of it asks for; a version that no longer parses asks for nothing.
func toVersionResponse(v workflows.Version) versionResponse {
	out := versionResponse{versionSummary: toVersionSummary(v), SpecYAML: v.SpecYAML, Inputs: json.RawMessage("null"), Bindings: []bindingRef{}}
	parsed, _ := spec.Parse([]byte(v.SpecYAML))
	if parsed == nil {
		return out
	}
	if len(parsed.Inputs) > 0 {
		out.Inputs = parsed.Inputs
	}
	for name, b := range parsed.Tools {
		access := b.Access
		if access == "" {
			access = spec.AccessRead
		}
		out.Bindings = append(out.Bindings, bindingRef{Name: name, Tool: b.Tool, Scopes: append([]string{}, b.Scopes...), Access: access})
	}
	sort.Slice(out.Bindings, func(i, j int) bool { return out.Bindings[i].Name < out.Bindings[j].Name })
	return out
}

func (s *Server) workflowsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Workflows == nil {
		respondError(w, r, errWorkflowsUnavailable)
		return false
	}
	return true
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if !s.workflowsReady(w, r) {
		return
	}
	list, err := s.Workflows.List(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]workflowSummary, 0, len(list))
	for _, wf := range list {
		out = append(out, toWorkflowSummary(r.Context(), wf))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"workflows": out})
}

// respondWorkflow answers a workflow with its current version.
func (s *Server) respondWorkflow(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	wf, v, err := s.Workflows.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	respondJSON(w, r, status, workflowResponse{Workflow: toWorkflowSummary(r.Context(), wf),
		Version: toVersionResponse(v)})
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req saveWorkflowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	id, err := s.Workflows.Create(r.Context(), req.SpecYAML, req.Comment)
	if err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	s.respondWorkflow(w, r, id, http.StatusCreated)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.workflowsReady(w, r) {
		s.respondWorkflow(w, r, id, http.StatusOK)
	}
}

func (s *Server) handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req saveWorkflowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	if err := s.Workflows.Save(r.Context(), id, req.SpecYAML, req.Comment, req.BaseVersion); err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	s.respondWorkflow(w, r, id, http.StatusOK)
}

func (s *Server) handleArchiveWorkflow(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	if err := s.Workflows.Archive(r.Context(), id); err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListWorkflowVersions(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	versions, err := s.Workflows.Versions(r.Context(), id)
	if err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	out := make([]versionSummary, 0, len(versions))
	for _, v := range versions {
		out = append(out, toVersionSummary(v))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"versions": out})
}

func (s *Server) handleGetWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	number, err := strconv.Atoi(chi.URLParam(r, "versionNumber"))
	if err != nil || number < 1 {
		respondError(w, r, ErrBadRequest("That is not a version number. Versions count from 1."))
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	v, err := s.Workflows.Version(r.Context(), id, number)
	if err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toVersionResponse(v))
}

func (s *Server) handleDiffWorkflowVersions(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	from, errFrom := strconv.Atoi(r.URL.Query().Get("from"))
	to, errTo := strconv.Atoi(r.URL.Query().Get("to"))
	if errFrom != nil || errTo != nil || from < 1 || to < 1 {
		respondError(w, r, ErrValidation(map[string]string{"from": "Give the older version's number.", "to": "Give the newer version's number."}))
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	d, err := s.Workflows.Diff(r.Context(), id, from, to)
	if err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	out := diffResponse{From: d.From, To: d.To, Hunks: make([]diffHunk, 0, len(d.Hunks))}
	for _, h := range d.Hunks {
		hunk := diffHunk{FromLine: h.FromLine, ToLine: h.ToLine, Lines: make([]diffLine, 0, len(h.Lines))}
		for _, l := range h.Lines {
			hunk.Lines = append(hunk.Lines, diffLine{Op: diffOp(l.Op), Text: l.Text})
		}
		out.Hunks = append(out.Hunks, hunk)
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleShareWorkflow(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req sharingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	if err := s.Workflows.Share(r.Context(), id, string(req.Sharing), req.Groups); err != nil {
		respondError(w, r, workflowError(err))
		return
	}
	s.respondWorkflow(w, r, id, http.StatusOK)
}

func (s *Server) handleValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req validateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.workflowsReady(w, r) {
		return
	}
	problems := s.Workflows.Validate(r.Context(), req.SpecYAML)
	out := validateResponse{Valid: len(problems) == 0, Problems: make([]problem, 0, len(problems))}
	for _, p := range problems {
		out.Problems = append(out.Problems, problem{Path: p.Path, Line: p.Line, Column: p.Column, Code: p.Code, Message: p.Message})
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleWorkflowSchema(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, json.RawMessage(spec.Schema()))
}
