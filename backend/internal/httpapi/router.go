package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// APIPrefix is where every endpoint but the probes lives.
const APIPrefix = "/api/v1"

// Database is what the handlers ask of the cluster; *db.Cluster is the real one.
type Database interface {
	Ping(ctx context.Context) error
}

// Secrets is OpenBao as readiness sees it; *vault.Client is the real one.
type Secrets interface {
	Ready(ctx context.Context) error
}

// Server holds everything the handlers need.
type Server struct {
	DB  Database
	Log *slog.Logger
	// Vault is nil where OpenBao is not configured, in development outside the stack.
	Vault Secrets
	// Tools and Connections are nil where OpenBao is not configured; their routes then answer 503.
	Tools       ToolRegistry
	Connections Connections
	Catalogs    Catalogs
	// Workflows is nil only in tests that exercise none.
	Workflows Workflows
	// Sessions and SignIn are nil only in tests that exercise neither.
	Sessions Sessions
	SignIn   SignIn
	// SessionTTL is how long the cookie a sign-in sets lives, matching the session behind it.
	SessionTTL time.Duration

	// CookieName is the session cookie the same-site check guards.
	CookieName string
	Secure     bool
	// AppBaseURL is the web client's origin, which the same-site check trusts.
	AppBaseURL string
	// CheckOrigin also refuses a cookie-carried write from a foreign origin;
	// off in development, where the dev server proxies from an origin of its own.
	CheckOrigin bool
	// RequestTimeout bounds each handler; zero means the configured default.
	RequestTimeout time.Duration
}

// Routes builds the HTTP surface. The order matters: an id first so every later
// line can quote it, recovery inside the logger so a panic still logs a line.
func (s *Server) Routes(allowedOrigins []string) http.Handler {
	timeout := s.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultRequestTimeout
	}

	r := chi.NewRouter()
	r.Use(requestID)
	r.Use(logging(s.Log))
	r.Use(recovery)
	r.Use(securityHeaders)
	r.Use(cors(allowedOrigins))
	r.Use(s.sameSite(allowedOrigins))
	r.Use(middleware.Timeout(timeout))
	r.Use(s.authenticate)

	// Liveness and readiness are deliberately outside the API and authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)

	r.Route(APIPrefix, func(r chi.Router) {
		r.Get("/openapi.json", s.handleOpenAPI)
		r.Get("/meta", s.handleMeta)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Post("/auth/oidc/backchannel-logout", s.handleBackchannelLogout)
		r.Get("/connections/callback", s.handleConnectionCallback)
		mountPending(r, true, false)
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/tools", s.handleListTools)
			r.Get("/tools/{toolID}", s.handleGetTool)
			r.Get("/connections", s.handleListConnections)
			r.Post("/tools/{toolID}/connection", s.handleConnect)
			r.Delete("/tools/{toolID}/connection", s.handleDisconnect)
			r.Get("/tools/{toolID}/catalog", s.handleToolCatalog)
			r.Get("/workflows", s.handleListWorkflows)
			r.Post("/workflows", s.handleCreateWorkflow)
			r.Post("/workflows/validate", s.handleValidateWorkflow)
			r.Get("/workflows/{workflowID}", s.handleGetWorkflow)
			r.Put("/workflows/{workflowID}", s.handleSaveWorkflow)
			r.Delete("/workflows/{workflowID}", s.handleArchiveWorkflow)
			r.Get("/workflows/{workflowID}/versions", s.handleListWorkflowVersions)
			r.Get("/workflows/{workflowID}/versions/{versionNumber}", s.handleGetWorkflowVersion)
			r.Get("/workflows/{workflowID}/diff", s.handleDiffWorkflowVersions)
			r.Put("/workflows/{workflowID}/sharing", s.handleShareWorkflow)
			r.Get("/schemas/workflow.v1.json", s.handleWorkflowSchema)
			mountPending(r, false, false)
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				r.Post("/tools", s.handleAddTool)
				r.Delete("/tools/{toolID}", s.handleRemoveTool)
				mountPending(r, false, true)
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, ErrNotFound("No such endpoint. The API lives under "+APIPrefix+"."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, &APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "That method is not allowed here.",
		})
	})
	return r
}

// mountPending routes each pending operation to a 501 so clients can be written against it; the ones
// that are not public sit behind the sign-in, and the admin ones behind the role too.
func mountPending(r chi.Router, public, admin bool) {
	for _, op := range operations {
		if op.pending && op.public == public && op.admin == admin && !op.root {
			r.Method(op.method, op.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respondError(w, r, errNotImplemented)
			}))
		}
	}
}

// statusResponse is what the probes answer.
type statusResponse struct {
	Status string `json:"status"`
}

// handleLiveness touches nothing external on purpose: a database outage must
// not get the container killed and restarted, which would only make it worse.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, statusResponse{Status: "ok"})
}

// handleReadiness answers whether the process can serve traffic: the database and OpenBao answer. Nobody has
// signed in to read it, so a failure says whether, not why.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), config.ReadinessTimeout)
	defer cancel()

	if s.DB == nil {
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	if err := s.DB.Ping(ctx); err != nil {
		loggerFrom(r.Context()).Error("not ready", "error", err)
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	// Without OpenBao no connection can be made or used, so a sealed one takes the api out of rotation.
	if s.Vault != nil {
		if err := s.Vault.Ready(ctx); err != nil {
			loggerFrom(r.Context()).Error("not ready", "error", err)
			respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
			return
		}
	}
	respondJSON(w, r, http.StatusOK, statusResponse{Status: "ok"})
}
