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
	r.Delete("/control/tenants/{tenant}/grants", s.controlled(s.handleEndGrants))
	r.Delete("/control/tenants/{tenant}/clients", s.controlled(s.handleForgetClients))
	r.Delete("/control/tenants/{tenant}", s.controlled(s.handleForgetTenant))
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tenants[in.Name]; !exists && len(s.tenants) >= MaxTenants {
		writeError(w, http.StatusServiceUnavailable, "too_many_tenants", errTooManyTenants.Error()+".")
		return
	}
	t := newTenant(in.Name, in.Options)
	s.tenants[in.Name] = t
	writeJSON(w, http.StatusCreated, map[string]any{"name": t.name, "oauth": t.oauth(), "reuseDetection": t.reuseDetection(),
		"product": t.product(), "capabilities": t.capabilities(), "accessTokenSeconds": int(t.accessTTL().Seconds())})
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
