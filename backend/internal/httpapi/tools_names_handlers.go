package httpapi

import (
	"context"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/tools"
)

// binders are the workflows whose current version binds the tool name, for an administrator renaming the tool.
func (s *Server) binders(ctx context.Context, name string) ([]toolBinder, error) {
	out := []toolBinder{}
	if s.Workflows == nil {
		return out, nil
	}
	found, err := s.Workflows.Binding(ctx, name)
	if err != nil {
		return nil, err
	}
	for _, b := range found {
		out = append(out, toolBinder{ID: b.ID.String(), Name: b.Name, OwnerName: b.OwnerName})
	}
	return out, nil
}

// toolFor is the tool the path names, answering the error itself when there is none to act on.
func (s *Server) toolFor(w http.ResponseWriter, r *http.Request) (tools.Tool, bool) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return tools.Tool{}, false
	}
	if s.Tools == nil {
		respondError(w, r, errToolsUnavailable)
		return tools.Tool{}, false
	}
	t, err := s.Tools.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, toolError(err))
		return tools.Tool{}, false
	}
	return t, true
}

func (s *Server) handleToolWorkflows(w http.ResponseWriter, r *http.Request) {
	t, ok := s.toolFor(w, r)
	if !ok {
		return
	}
	found, err := s.binders(r.Context(), t.Name)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, toolWorkflowsResponse{Workflows: found})
}

func (s *Server) handleRenameTool(w http.ResponseWriter, r *http.Request) {
	var req renameToolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	t, ok := s.toolFor(w, r)
	if !ok {
		return
	}
	// Read before renaming, since afterwards nothing tells which workflows meant this tool.
	stranded, err := s.binders(r.Context(), t.Name)
	if err != nil {
		respondError(w, r, err)
		return
	}
	renamed, err := s.Tools.Rename(r.Context(), t.ID, req.Name)
	if err != nil {
		respondError(w, r, toolError(err))
		return
	}
	if renamed.Name == t.Name {
		stranded = []toolBinder{}
	}
	respondJSON(w, r, http.StatusOK, renameToolResponse{Tool: s.toToolResponse(renamed), Workflows: stranded})
}
