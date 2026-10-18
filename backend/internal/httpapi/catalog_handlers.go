package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/catalog"
	"github.com/cloudster1/rotor/backend/internal/tools"
)

// Catalogs reads a tool's MCP tools as the caller's connection sees them; *catalog.Service is the real one.
type Catalogs interface {
	Get(ctx context.Context, toolID uuid.UUID) (catalog.Catalog, error)
}

var errCatalogNotConnected = &APIError{Status: http.StatusConflict, Code: "not_connected",
	Message: "Connect your account in this tool on the Connections page first; Rotor lists its tools as your account sees them."}

func (s *Server) handleToolCatalog(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Catalogs == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	c, err := s.Catalogs.Get(r.Context(), id)
	switch {
	case errors.Is(err, tools.ErrNotFound):
		respondError(w, r, ErrNotFound("There is no such tool. Ask an administrator to add it in the settings."))
		return
	case errors.Is(err, catalog.ErrNotConnected):
		respondError(w, r, errCatalogNotConnected)
		return
	case errors.Is(err, catalog.ErrUnreachable):
		loggerFrom(r.Context()).Warn("catalog not read", "tool", id, "error", err)
		respondError(w, r, errToolUnreachable)
		return
	case err != nil:
		respondError(w, r, err)
		return
	}
	out := catalogResponse{Tool: connectionTool{ID: c.ToolID.String(), Name: c.ToolName, BaseURL: c.ToolBaseURL},
		Build: c.Build, Tools: make([]catalogTool, 0, len(c.Tools))}
	for _, t := range c.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = []byte("{}")
		}
		out.Tools = append(out.Tools, catalogTool{Name: t.Name, Description: t.Description, InputSchema: schema,
			ReadOnly: t.ReadOnly, Destructive: t.Destructive})
	}
	respondJSON(w, r, http.StatusOK, out)
}
