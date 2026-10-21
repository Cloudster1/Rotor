package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/tools"
)

// connectionsPath is the web client's page the callback lands on.
const connectionsPath = "/connections"

// Connections is people's connections to the tools; *connections.Service is the real one.
type Connections interface {
	List(ctx context.Context) ([]connections.Connection, error)
	Start(ctx context.Context, toolID uuid.UUID, next string) (string, error)
	Finish(ctx context.Context, cb connections.Callback) (uuid.UUID, string, error)
	Disconnect(ctx context.Context, toolID uuid.UUID) error
}

var (
	errNeedsRegistration = ErrConflict("This tool no longer knows Rotor. Ask an administrator of Rotor to register it again in the settings.")
	errNotConnected      = ErrNotFound("You are not connected to this tool. Connect it on the Connections page.")
)

func connectionError(err error) error {
	switch {
	case errors.Is(err, connections.ErrNeedsRegistration):
		return errNeedsRegistration
	case errors.Is(err, connections.ErrNotConnected):
		return errNotConnected
	case errors.Is(err, connections.ErrUnreachable):
		return errToolUnreachable
	case errors.Is(err, connections.ErrNoSession):
		return errSessionOnly
	}
	return toolError(err)
}

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	if s.Connections == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	list, err := s.Connections.List(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]connectionResponse, 0, len(list))
	for _, c := range list {
		out = append(out, connectionResponse{
			Tool:   connectionTool{ID: c.ToolID.String(), Name: c.ToolName, BaseURL: c.ToolBaseURL},
			Scopes: c.Scopes, Status: connectionStatus(c.Status), ConnectedAt: c.ConnectedAt, LastUsedAt: c.LastUsedAt,
		})
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connections": out})
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Connections == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	next := r.URL.Query().Get("next")
	if next != "" {
		next = safeNext(next)
	}
	authorize, err := s.Connections.Start(r.Context(), id, next)
	if err != nil {
		respondError(w, r, connectionError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, connectResponse{AuthorizeURL: authorize})
}

// handleConnectionCallback lands the browser on the page the connection was started from, or the Connections page,
// saying what came of it; a failure always lands on the Connections page, which explains it.
func (s *Server) handleConnectionCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if s.Connections == nil {
		redirect(w, connectionsPath+"?error="+connections.CodeUnavailable)
		return
	}
	toolID, next, err := s.Connections.Finish(r.Context(), connections.Callback{State: q.Get("state"), Code: q.Get("code"),
		Issuer: q.Get("iss"), Error: q.Get("error"), ErrorDescription: q.Get("error_description")})
	if err != nil {
		code := connections.CodeUnavailable
		var failed *connections.CallbackError
		if errors.As(err, &failed) {
			code = failed.Code
		}
		loggerFrom(r.Context()).Warn("connection not made", "code", code, "error", err)
		target := connectionsPath + "?error=" + url.QueryEscape(code)
		if toolID != uuid.Nil {
			target += "&tool=" + toolID.String()
		}
		redirect(w, target)
		return
	}
	target := connectionsPath
	if next != "" && next != "/" {
		target = next
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	redirect(w, target+separator+"connected="+toolID.String())
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "toolID", "tool")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Connections == nil {
		respondError(w, r, errToolsUnavailable)
		return
	}
	if err := s.Connections.Disconnect(r.Context(), id); err != nil {
		if errors.Is(err, tools.ErrNotFound) {
			err = errNotConnected
		}
		respondError(w, r, connectionError(err))
		return
	}
	respondNoContent(w)
}
