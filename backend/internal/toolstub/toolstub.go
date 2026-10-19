// Package toolstub stands in for a tool Rotor connects to: an OAuth authorization server and an MCP endpoint that
// behave as Exciter's do, held to the siblings' vendored documents by a contract test (#8).
package toolstub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Paths, and the limits from internal/config under shorter names.
const (
	AccessTTL    = config.ToolStubAccessTTL
	RefreshTTL   = config.ToolStubRefreshTTL
	CodeTTL      = config.ToolStubCodeTTL
	RequestTTL   = config.ToolStubRequestTTL
	MaxTenants   = config.ToolStubMaxTenants
	MaxFormBytes = config.ToolStubMaxFormBytes
	MaxMCPBytes  = config.ToolStubMaxMCPBytes
	// TenantPrefix is where each tenant's tool lives: /t/{tenant}/api/v1/... and its metadata.
	TenantPrefix = "/t"
	apiPrefix    = "/api/v1"
	mcpPath      = apiPrefix + "/mcp"
)

// tenantName is what a tenant may be called: short, lower case, safe in a path.
var tenantName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Options shape one tenant; the zero value of each pointer is Exciter's behaviour.
type Options struct {
	// OAuth false serves /meta and /mcp alone, as a tool that is no authorization server.
	OAuth *bool `json:"oauth,omitempty"`
	// ReuseDetection false refuses a replaced refresh token without ending its grant.
	ReuseDetection *bool `json:"reuseDetection,omitempty"`
	// Product is what /meta names; exciter unless told.
	Product string `json:"product,omitempty"`
	// Capabilities is what /meta lists; mcp@1 unless told, and an empty list is none.
	Capabilities *[]string `json:"capabilities,omitempty"`
	// AccessTokenSeconds overrides AccessTTL, for tests that watch a token expire.
	AccessTokenSeconds int `json:"accessTokenSeconds,omitempty"`
	// LLMKey is the key the tenant's stub model takes; empty takes any.
	LLMKey string `json:"llmKey,omitempty"`
	// LLMScript is what the tenant's stub model answers, one turn per chat call.
	LLMScript []LLMTurn `json:"llmScript,omitempty"`
	// Requirements maps a requirement key to the usernames get_requirement shows it to; anyone else is refused.
	Requirements map[string][]string `json:"requirements,omitempty"`
}

// Stats count what happened at a tenant, for tests to check what Rotor did there.
type Stats struct {
	Registrations  int `json:"registrations"`
	Authorizations int `json:"authorizations"`
	Consents       int `json:"consents"`
	CodesRedeemed  int `json:"codesRedeemed"`
	Refreshes      int `json:"refreshes"`
	ReuseEvents    int `json:"reuseEvents"`
	Revocations    int `json:"revocations"`
	GrantsEnded    int `json:"grantsEnded"`
	MCPCalls       int `json:"mcpCalls"`
	LLMCalls       int `json:"llmCalls"`
	// LiveGrants and LiveAccessTokens are counted when asked, not kept.
	LiveGrants       int `json:"liveGrants"`
	LiveAccessTokens int `json:"liveAccessTokens"`
	Clients          int `json:"clients"`
}

// Stub holds every tenant; one lock covers them all, which also serialises refreshes the way a database row lock does.
type Stub struct {
	mu      sync.Mutex
	tenants map[string]*tenant
	tools   []map[string]any
	now     func() time.Time
}

// New makes a stub serving toolsList, a tools/list result ({"tools": [...]}), at every tenant's MCP endpoint.
func New(toolsList []byte) (*Stub, error) {
	var list struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(toolsList, &list); err != nil || len(list.Tools) == 0 {
		return nil, fmt.Errorf("the tools/list snapshot is not a tools/list result with tools: %v", err)
	}
	return &Stub{tenants: map[string]*tenant{}, tools: list.Tools, now: time.Now}, nil
}

// tenant is one tool: its options, the clients registered with it and what people consented to.
type tenant struct {
	name     string
	opts     Options
	clients  map[string]*client
	requests map[string]*authRequest
	codes    map[string]*code
	grants   map[string]*grant
	tokens   map[string]*token
	stats    Stats
	llmNext  int
}

func (t *tenant) oauth() bool          { return t.opts.OAuth == nil || *t.opts.OAuth }
func (t *tenant) reuseDetection() bool { return t.opts.ReuseDetection == nil || *t.opts.ReuseDetection }

func (t *tenant) product() string {
	if t.opts.Product == "" {
		return "exciter"
	}
	return t.opts.Product
}

func (t *tenant) capabilities() []string {
	if t.opts.Capabilities == nil {
		return []string{"mcp@1"}
	}
	return append([]string{}, *t.opts.Capabilities...)
}

func (t *tenant) accessTTL() time.Duration {
	if t.opts.AccessTokenSeconds > 0 {
		return time.Duration(t.opts.AccessTokenSeconds) * time.Second
	}
	return AccessTTL
}

func newTenant(name string, opts Options) *tenant {
	return &tenant{name: name, opts: opts, clients: map[string]*client{}, requests: map[string]*authRequest{},
		codes: map[string]*code{}, grants: map[string]*grant{}, tokens: map[string]*token{}}
}

// errTooManyTenants refuses a new tenant once MaxTenants exist.
var errTooManyTenants = fmt.Errorf("the stub holds %d tenants already; forget some through the control API", MaxTenants)

// productSeparator splits a tenant made on first use into the product it answers as and the rest of its name, so a
// browser test that reaches no control API still gets a tool of a name nobody else uses: /t/e2eab12--x is e2eab12.
const productSeparator = "--"

// tenantOf finds a tenant by name, making it with Exciter's behaviour on first use; callers hold the lock.
func (s *Stub) tenantOf(name string) (*tenant, error) {
	if t, ok := s.tenants[name]; ok {
		return t, nil
	}
	if !tenantName.MatchString(name) {
		return nil, fmt.Errorf("a tenant is named by 1 to 63 lower case letters, digits and dashes, not %q", name)
	}
	if len(s.tenants) >= MaxTenants {
		return nil, errTooManyTenants
	}
	opts := Options{}
	if product, _, named := strings.Cut(name, productSeparator); named && product != "" {
		opts.Product = product
	}
	t := newTenant(name, opts)
	s.tenants[name] = t
	return t, nil
}

// Public is what the stack publishes: each tenant's tool, and its metadata where RFC 8414 and RFC 9728 put it for
// an issuer with a path.
func (s *Stub) Public() http.Handler {
	r := chi.NewRouter()
	r.Route(TenantPrefix+"/{tenant}", func(r chi.Router) {
		r.Get("/.well-known/oauth-protected-resource", s.withOAuth(s.handleProtectedResource))
		r.Get("/.well-known/oauth-protected-resource"+mcpPath, s.withOAuth(s.handleProtectedResource))
		r.Get("/.well-known/oauth-authorization-server", s.withOAuth(s.handleServerMetadata))
		r.Get(apiPrefix+"/meta", s.withTenant(s.handleMeta))
		r.Post(mcpPath, s.withTenant(s.handleMCP))
		r.Post(apiPrefix+"/oauth/register", s.withOAuth(s.handleRegister))
		r.Get(apiPrefix+"/oauth/authorize", s.withOAuth(s.handleAuthorize))
		r.Post(apiPrefix+"/oauth/token", s.withOAuth(s.handleToken))
		r.Post(apiPrefix+"/oauth/revoke", s.withOAuth(s.handleRevoke))
		r.Get("/oauth/consent", s.withOAuth(s.handleConsentPage))
		r.Post("/oauth/consent", s.withOAuth(s.handleConsent))
		r.Get(llmPath+"/models", s.withTenant(s.handleLLMModels))
		r.Post(llmPath+"/chat/completions", s.thinking(s.withTenant(s.handleLLMChat)))
	})
	r.Get("/.well-known/oauth-authorization-server"+TenantPrefix+"/{tenant}", s.withOAuth(s.handleServerMetadata))
	r.Get("/.well-known/oauth-protected-resource"+TenantPrefix+"/{tenant}"+mcpPath, s.withOAuth(s.handleProtectedResource))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "No such endpoint. Each tool lives under /t/{tenant}.")
	})
	return r
}

// call is one request to a tenant, with the lock held and the tenant's public names worked out.
type call struct {
	w http.ResponseWriter
	r *http.Request
	t *tenant
	// issuer is the tenant's address as the caller named the stub, which every metadata document is named from.
	issuer string
}

func (c *call) resource() string { return c.issuer + mcpPath }

type handler func(c *call)

// withTenant runs h with the lock held on the tenant the path names.
func (s *Stub) withTenant(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		name := chi.URLParam(r, "tenant")
		t, err := s.tenantOf(name)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", err.Error()+".")
			return
		}
		h(&call{w: w, r: r, t: t, issuer: publicOrigin(r) + TenantPrefix + "/" + name})
	}
}

// withOAuth is withTenant for the authorization server's endpoints, which a tenant without OAuth does not have.
func (s *Stub) withOAuth(h handler) http.HandlerFunc {
	return s.withTenant(func(c *call) {
		if !c.t.oauth() {
			writeError(c.w, http.StatusNotFound, "not_found", "This tool is no OAuth authorization server.")
			return
		}
		h(c)
	})
}

// publicOrigin is the stub's address as the caller named it: the Host a rewrite keeps, or the one a proxy forwards.
func publicOrigin(r *http.Request) string {
	scheme, host := "http", r.Host
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host, _, _ = strings.Cut(forwarded, ",")
		host = strings.TrimSpace(host)
	}
	return scheme + "://" + host
}

// writeJSON answers a body, never kept by a cache since most carry secrets.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// apiError is the error envelope Exciter, Armature and Stator answer outside the OAuth protocol.
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var e apiError
	e.Error.Code, e.Error.Message = code, message
	writeJSON(w, status, e)
}

func (s *Stub) handleMeta(c *call) {
	writeJSON(c.w, http.StatusOK, map[string]any{"product": c.t.product(), "build": map[string]any{"version": "stub"},
		"apiVersion": "1", "capabilities": c.t.capabilities()})
}
