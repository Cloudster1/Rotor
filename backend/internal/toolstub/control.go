package toolstub

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// The control API, which tests use to shape a tenant and watch what Rotor did there. It is served on a listener of
// its own that the stack never publishes, so only what runs on the stack's network can reach it.

// TenantRequest makes or resets a tenant.
type TenantRequest struct {
	Name string `json:"name"`
	Options
}

// Control serves the control API and the health check.
func (s *Stub) Control() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Post("/control/tenants", s.handleMakeTenant)
	r.Get("/control/tenants/{tenant}/stats", s.controlled(s.handleStats))
	r.Get("/control/tenants/{tenant}/records", s.controlled(s.handleRecords))
	r.Delete("/control/tenants/{tenant}/grants", s.controlled(s.handleEndGrants))
	r.Delete("/control/tenants/{tenant}/clients", s.controlled(s.handleForgetClients))
	r.Put("/control/tenants/{tenant}/writes", s.controlled(s.handleSetWrites))
	r.Put("/control/tenants/{tenant}/capabilities", s.controlled(s.handleSetCapabilities))
	r.Post("/control/tenants/{tenant}/clients/approve", s.controlled(s.handleApproveClients))
	r.Delete("/control/tenants/{tenant}", s.controlled(s.handleForgetTenant))
	r.Get("/control/langsmith", s.handleLangSmithCalls)
	r.Delete("/control/langsmith", s.handleForgetLangSmith)
	return r
}

// handleMakeTenant makes a tenant with the options given, forgetting one of the name that existed.
func (s *Stub) handleMakeTenant(w http.ResponseWriter, r *http.Request) {
	var in TenantRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFormBytes)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Send the tenant as one JSON object with a name.")
		return
	}
	if !tenantName.MatchString(in.Name) {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Name the tenant with 1 to 63 lower case letters, digits and dashes.")
		return
	}
	if in.Snapshot != "" && !s.HasSnapshot(in.Snapshot) {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "The stub has no snapshot "+in.Snapshot+
			". Name exciter or armature, or leave it out to serve the product's own.")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tenants[in.Name]; !exists && len(s.tenants) >= MaxTenants {
		writeError(w, http.StatusServiceUnavailable, "too_many_tenants", errTooManyTenants.Error()+".")
		return
	}
	t := newTenant(in.Name, in.Options)
	s.tenants[in.Name] = t
	writeJSON(w, http.StatusCreated, map[string]any{"name": t.name, "oauth": t.oauth(), "reuseDetection": t.reuseDetection(),
		"product": t.product(), "capabilities": t.capabilities(), "accessTokenSeconds": int(t.accessTTL().Seconds()),
		"snapshot": t.snapshot(), "testPlans": t.testPlans()})
}

// testPlans name the tenant's test plans with their ids, which start_test_execution takes.
func (t *tenant) testPlans() []map[string]any {
	out := []map[string]any{}
	for _, plan := range t.opts.TestPlans {
		out = append(out, map[string]any{"id": TestPlanID(t.name, plan), "project": plan.Project, "title": plan.Title})
	}
	return out
}

// handleRecords answers what callers wrote at the tenant, oldest first: comments and remote links by issue key,
// requirements, Armature links by requirement key, reviews, links between requirements and test executions.
func (s *Stub) handleRecords(w http.ResponseWriter, _ *http.Request, t *tenant) {
	view := t.written.view()
	repositories := map[string]any{}
	for key, repo := range t.repos {
		repositories[key] = repo.view()
	}
	view["repositories"] = repositories
	writeJSON(w, http.StatusOK, view)
}

// controlled runs h on an existing tenant with the lock held; the control API never makes one by asking.
func (s *Stub) controlled(h func(w http.ResponseWriter, r *http.Request, t *tenant)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		t, ok := s.tenants[chi.URLParam(r, "tenant")]
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "No such tenant. Make it with POST /control/tenants, or use it once.")
			return
		}
		h(w, r, t)
	}
}

func (s *Stub) handleStats(w http.ResponseWriter, _ *http.Request, t *tenant) {
	out := t.stats
	out.LiveGrants, out.Clients = len(t.grants), len(t.clients)
	for _, tk := range t.tokens {
		if !tk.refresh && s.now().Before(tk.expires) {
			out.LiveAccessTokens++
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleEndGrants ends grants as the tool would on its own, so the next refresh meets invalid_grant; ?username= ends
// one person's alone.
func (s *Stub) handleEndGrants(w http.ResponseWriter, r *http.Request, t *tenant) {
	who := r.URL.Query().Get("username")
	ended := 0
	for id, g := range t.grants {
		if who == "" || g.username == who {
			t.endGrant(id)
			ended++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"ended": ended})
}

// handleForgetClients forgets every client and its grants, as Exciter sweeps an unused one, so the next call meets
// invalid_client.
func (s *Stub) handleForgetClients(w http.ResponseWriter, _ *http.Request, t *tenant) {
	forgotten := len(t.clients)
	for id := range t.grants {
		t.endGrant(id)
	}
	t.clients, t.requests = map[string]*client{}, map[string]*authRequest{}
	writeJSON(w, http.StatusOK, map[string]int{"forgotten": forgotten})
}

func (s *Stub) handleForgetTenant(w http.ResponseWriter, r *http.Request, t *tenant) {
	delete(s.tenants, t.name)
	w.WriteHeader(http.StatusNoContent)
}

// handleSetWrites changes which calls the tenant lists as writing, as a tool that changed a call after a workflow
// was saved against it (#90).
func (s *Stub) handleSetWrites(w http.ResponseWriter, r *http.Request, t *tenant) {
	var in struct {
		Calls []string `json:"calls"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFormBytes)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Send the calls to list as writing, as {\"calls\": [...]}.")
		return
	}
	t.opts.Writes = in.Calls
	writeJSON(w, http.StatusOK, map[string]any{"writes": t.opts.Writes})
}

// handleSetCapabilities changes what the tenant's /meta lists, as a tool updated to another release (#43).
func (s *Stub) handleSetCapabilities(w http.ResponseWriter, r *http.Request, t *tenant) {
	var in struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFormBytes)).Decode(&in); err != nil || in.Capabilities == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Send the capabilities to list, as {\"capabilities\": [...]}.")
		return
	}
	t.opts.Capabilities = &in.Capabilities
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": in.Capabilities})
}
