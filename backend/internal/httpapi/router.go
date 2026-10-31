package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"

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
	// Providers is nil where OpenBao is not configured.
	Providers Providers
	Catalogs  Catalogs
	// Workflows is nil only in tests that exercise none.
	Workflows Workflows
	// Runs is nil where OpenBao is not configured; its routes then answer 503.
	Runs Runs
	// NoSandboxes is set where the installation runs no sandboxes; /meta then says runs are off (#178).
	NoSandboxes bool
	// Approvals is nil where runs are not set up; its routes then answer 503.
	Approvals Approvals
	// Triggers is nil where runs are not set up; its routes then answer 503.
	Triggers Triggers
	// Audit reads the audit log for administrators (#36); nil answers 503.
	Audit AuditLog
	// Usage is nil where runs are not set up; its routes then answer 503.
	Usage Usage
	// Hooks is nil where OpenBao is not configured; its routes then answer 503.
	Hooks Hooks
	// LangSmith is nil where OpenBao is not configured; its routes then answer 503 (#37).
	LangSmith LangSmith
	// LangSmithEndpoint is where runs' spans go while LangSmith is on, shown to administrators.
	LangSmithEndpoint string
	// Retention is how long a finished run's data is kept (#41); nil answers 503.
	Retention Retention
	// Workspaces is what workspaces may use (#136); nil answers 503.
	Workspaces Workspaces
	// OAuth is Rotor as the authorization server of its MCP endpoint (#47); nil answers 503 and refuses every token.
	OAuth OAuthServer
	// TrustedProxies are the peers whose X-Real-IP header names the caller.
	TrustedProxies []netip.Prefix
	// Sessions and SignIn are nil only in tests that exercise neither.
	Sessions Sessions
	SignIn   SignIn
	// ServiceAccounts checks a realm's access token at the routes that take one (#168); nil refuses every such token.
	ServiceAccounts ServiceAccounts
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

	// handler is the whole router, which MCP sends the calls of its tools through; Routes sets it.
	handler http.Handler
}

// Routes builds the HTTP surface. The order matters: an id first so every later
// line can quote it, recovery inside the logger so a panic still logs a line.
func (s *Server) Routes(allowedOrigins []string) http.Handler {
	timeout := s.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultRequestTimeout
	}

	r := chi.NewRouter()
	r.Use(traced)
	r.Use(requestID)
	r.Use(logging(s.Log))
	r.Use(recovery)
	r.Use(securityHeaders)
	r.Use(cors(allowedOrigins))
	r.Use(s.sameSite(allowedOrigins))
	r.Use(timeoutExceptStreams(timeout))
	r.Use(s.authenticate)
	r.Use(readOnlyGrant)

	// Liveness and readiness are deliberately outside the API and authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)
	s.mountOAuthDiscovery(r)

	r.Route(APIPrefix, func(r chi.Router) {
		r.Get("/openapi.json", s.handleOpenAPI)
		r.Get("/meta", s.handleMeta)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Post("/auth/oidc/backchannel-logout", s.handleBackchannelLogout)
		r.Get("/connections/callback", s.handleConnectionCallback)
		r.Post("/hooks/{toolID}", s.handleReceiveHook)
		s.mountOAuth(r)
		r.With(s.requireMCPAuth).Post("/mcp", s.handleMCP)
		mountPending(r, true, false)
		// A service account of the realm adds tools and hands Rotor their hook secrets (#168); nothing else takes its
		// token. The table marks these rows serviceAccount, and a test holds the two to each other.
		r.Group(func(r chi.Router) {
			r.Use(s.serviceAccount)
			r.Use(requireAuth)
			r.Get("/tools", s.handleListTools)
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				r.Use(requireSession)
				r.Post("/tools", s.handleAddTool)
				r.Get("/tools/{toolID}/hook", s.handleGetHook)
				r.Put("/tools/{toolID}/hook", s.handleSetHookSecret)
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Get("/auth/me", s.handleMe)
			r.With(requireSession).Post("/auth/logout", s.handleLogout)
			r.Get("/tools/{toolID}", s.handleGetTool)
			r.Get("/connections", s.handleListConnections)
			r.Get("/models", s.handleListModels)
			r.With(requireSession).Post("/tools/{toolID}/connection", s.handleConnect)
			r.With(requireSession).Delete("/tools/{toolID}/connection", s.handleDisconnect)
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
			r.Post("/workflows/{workflowID}/runs", s.handleStartRun)
			r.Get("/runs", s.handleListRuns)
			r.Get("/runs/{runID}", s.handleGetRun)
			r.Post("/runs/{runID}/cancel", s.handleCancelRun)
			r.Get("/runs/{runID}/events", s.handleRunEvents)
			r.Get("/approvals", s.handleListApprovals)
			r.Get("/approvals/count", s.handleCountApprovals)
			r.Get("/approvals/{approvalID}", s.handleGetApproval)
			r.With(requireSession).Post("/approvals/{approvalID}/decision", s.handleDecideApproval)
			r.Get("/triggers", s.handleListTriggers)
			r.Post("/triggers/schedule", s.handlePreviewSchedule)
			r.Get("/triggers/{triggerID}", s.handleGetTrigger)
			r.Put("/triggers/{triggerID}", s.handleChangeTrigger)
			r.Delete("/triggers/{triggerID}", s.handleRemoveTrigger)
			r.Post("/triggers/{triggerID}/enable", s.handleEnableTrigger)
			r.Post("/triggers/{triggerID}/disable", s.handleDisableTrigger)
			r.Get("/workflows/{workflowID}/triggers", s.handleListWorkflowTriggers)
			r.Post("/workflows/{workflowID}/triggers", s.handleCreateTrigger)
			r.Get("/usage", s.handleMyUsage)
			mountPending(r, false, false)
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				// An assistant never administers Rotor, whoever it acts for.
				r.Use(requireSession)
				r.Get("/providers", s.handleListProviders)
				r.Post("/providers", s.handleAddProvider)
				r.Put("/providers/{providerID}", s.handleChangeProvider)
				r.Delete("/providers/{providerID}", s.handleRemoveProvider)
				r.Post("/providers/{providerID}/test", s.handleTestProvider)
				r.Delete("/tools/{toolID}", s.handleRemoveTool)
				r.Post("/tools/{toolID}/check", s.handleCheckTool)
				r.Get("/audit", s.handleListAudit)
				r.Get("/audit/facets", s.handleAuditFacets)
				r.Get("/audit/export", s.handleExportAudit)
				r.Delete("/tools/{toolID}/hook", s.handleRemoveHookSecret)
				r.Get("/usage/people", s.handlePeopleUsage)
				r.Get("/settings/limits", s.handleGetLimits)
				r.Put("/settings/limits", s.handleSetLimits)
				r.Get("/providers/{providerID}/prices", s.handleGetPrices)
				r.Put("/providers/{providerID}/prices", s.handleSetPrices)
				r.Get("/settings/langsmith", s.handleGetLangSmith)
				r.Put("/settings/langsmith", s.handleSetLangSmith)
				r.Delete("/settings/langsmith/key", s.handleRemoveLangSmithKey)
				r.Get("/settings/retention", s.handleGetRetention)
				r.Put("/settings/retention", s.handleSetRetention)
				r.Get("/settings/unattended-writes", s.handleGetUnattendedWrites)
				r.Put("/settings/unattended-writes", s.handleSetUnattendedWrites)
				r.Get("/settings/workspaces", s.handleGetWorkspaces)
				r.Put("/settings/workspaces", s.handleSetWorkspaces)
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
	s.handler = r
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
