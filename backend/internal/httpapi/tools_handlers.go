package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/tools"
)

// ToolRegistry is the tools Rotor acts in; *tools.Service is the real one.
type ToolRegistry interface {
	List(ctx context.Context) ([]tools.Tool, error)
	Get(ctx context.Context, id uuid.UUID) (tools.Tool, error)
	Add(ctx context.Context, req tools.AddRequest) (tools.Tool, bool, error)
	Remove(ctx context.Context, id uuid.UUID) error
}

var (
	errNoSuchTool       = ErrNotFound("There is no such tool. Ask an administrator to add it in the settings.")
	errToolsUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "tools_unavailable",
		Message: "Tool connections are not set up on this installation. An operator sets ROTOR_OPENBAO_ADDR to OpenBao."}
	errToolUnreachable = &APIError{Status: http.StatusBadGateway, Code: "tool_unreachable",
		Message: "The tool did not answer. Check that it is running and that Rotor can reach its address, then try again."}
)

// toolError says what went wrong with a tool in the words its caller needs.
func toolError(err error) error {
	var refusal *tools.Refusal
	switch {
	case errors.As(err, &refusal):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "tool_refused", Message: refusal.Message}
	case errors.Is(err, tools.ErrUnreachable):
		return errToolUnreachable
	case errors.Is(err, tools.ErrNotFound):
		return errNoSuchTool
	}
	return err
}

func toToolResponse(t tools.Tool) toolResponse {
	return toolResponse{ID: t.ID.String(), Name: t.Name, BaseURL: t.BaseURL, Resource: t.Resource, Issuer: t.Issuer,
		Capabilities: t.Capabilities, ScopesSupported: t.ScopesSupported, Registration: toolRegistration(t.Registration),
		Status: toolStatus(t.Status), AddedAt: t.AddedAt}
}

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	if s.Tools == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	list, err := s.Tools.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]toolResponse, 0, len(list))
	for _, t := range list {
		out = append(out, toToolResponse(t))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tools": out})
}

func (s *Server) handleGetTool(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Tools == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	t, err := s.Tools.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, toolError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tool": toToolResponse(t)})
}

func (s *Server) handleAddTool(w http.ResponseWriter, r *http.Request) {
	var req addToolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		respondError(w, r, ErrValidation(map[string]string{"baseURL": "Give the address you open the tool at."}))
		return
	}
	if s.Tools == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	t, created, err := s.Tools.Add(r.Context(), tools.AddRequest{BaseURL: req.BaseURL, ClientID: req.ClientID, ClientSecret: req.ClientSecret})
	if err != nil {
		loggerFrom(r.Context()).Warn("tool not added", "base_url", req.BaseURL, "error", err)
		respondError(w, r, toolError(err))
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondJSON(w, r, status, map[string]any{"tool": toToolResponse(t)})
}

func (s *Server) handleRemoveTool(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Tools == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	if err := s.Tools.Remove(r.Context(), id); err != nil {
		respondError(w, r, toolError(err))
		return
	}
	respondNoContent(w)
}
