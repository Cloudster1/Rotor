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

// Why a catalog cannot be read, each with what fixes it and who does (#266).
var (
	errCatalogNotConnected = &APIError{Status: http.StatusConflict, Code: "not_connected",
		Message: "Connect your account in this tool in Settings, under Tool accounts, first; Rotor lists its tools as your account sees them."}
	errCatalogReconnect = &APIError{Status: http.StatusConflict, Code: "reconnect",
		Message: "This tool ended your connection. Connect your account again in Settings, under Tool accounts, then pick the tool again."}
	errCatalogNeedsRegistration = &APIError{Status: http.StatusConflict, Code: "needs_registration",
		Message: "This tool no longer knows Rotor. Ask an administrator to register it again under Admin, Tools; then everyone connects their account again."}
)

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
	case errors.Is(err, catalog.ErrNeedsRegistration):
		respondError(w, r, errCatalogNeedsRegistration)
		return
	case errors.Is(err, catalog.ErrReconnect):
		respondError(w, r, errCatalogReconnect)
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
