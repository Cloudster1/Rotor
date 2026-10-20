package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeTool is a tool under a path, as the test double's tenants are, whose answers a test bends one at a time.
type fakeTool struct {
	*httptest.Server
	base, issuer string
	meta         map[string]any
	resource     map[string]any
	server       map[string]any
	registered   map[string]any
	register     func(w http.ResponseWriter)
}

func newFakeTool(t *testing.T) *fakeTool {
	t.Helper()
	f := &fakeTool{}
	mux := http.NewServeMux()
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	f.base = f.URL + "/t/demo"
	f.issuer = f.base
	f.meta = map[string]any{"product": "exciter", "capabilities": []any{map[string]any{"name": "mcp@1"}, "webhooks@2"}}
	f.resource = map[string]any{"resource": f.base + "/api/v1/mcp", "authorization_servers": []string{f.issuer}, "scopes_supported": []string{"mcp", "mcp:read", "evidence"}}
	f.server = map[string]any{"issuer": f.issuer, "authorization_endpoint": f.issuer + "/api/v1/oauth/authorize",
		"token_endpoint": f.issuer + "/api/v1/oauth/token", "revocation_endpoint": f.issuer + "/api/v1/oauth/revoke",
		"registration_endpoint": f.issuer + "/api/v1/oauth/register", "code_challenge_methods_supported": []string{"S256"},
		"response_types_supported": []string{"code"}, "token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"}}
	serve := func(get func() map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			body := get()
			if body == nil {
				http.NotFound(w, nil)
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		}
	}
	mux.HandleFunc("GET /t/demo/api/v1/meta", serve(func() map[string]any { return f.meta }))
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/t/demo/api/v1/mcp", serve(func() map[string]any { return f.resource }))
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/t/demo", serve(func() map[string]any { return f.server }))
	mux.HandleFunc("POST /t/demo/api/v1/oauth/register", func(w http.ResponseWriter, r *http.Request) {
		if f.register != nil {
			f.register(w)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&f.registered)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "c-1", "client_secret": "exciter_ocs_s", "token_endpoint_auth_method": "client_secret_basic"})
	})
	return f
}

func (f *fakeTool) service() *Service {
	return &Service{http: f.Client(), redirectURL: "http://localhost:3/api/v1/connections/callback"}
}

func TestDiscoveryFollowsTheRFCsUnderAPath(t *testing.T) {
	f := newFakeTool(t)
	d, err := f.service().discover(context.Background(), f.base)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "exciter" || d.Issuer != f.issuer || d.Resource != f.base+"/api/v1/mcp" || d.TokenEndpoint != f.issuer+"/api/v1/oauth/token" {
		t.Errorf("discovered %+v", d)
	}
	if strings.Join(d.Capabilities, ",") != "mcp@1,webhooks@2" || strings.Join(d.ScopesSupported, ",") != "mcp,mcp:read,evidence" {
		t.Errorf("capabilities %v, scopes %v", d.Capabilities, d.ScopesSupported)
	}
	if method, err := d.authMethod(); err != nil || method != authBasic {
		t.Errorf("auth method = %q, %v", method, err)
	}
}

func TestDiscoveryRefusesWhatIsMissingInASentence(t *testing.T) {
	for name, tc := range map[string]struct {
		bend func(f *fakeTool)
		says string
	}{
		"no meta":                 {func(f *fakeTool) { f.meta = nil }, "does not answer /api/v1/meta"},
		"no mcp":                  {func(f *fakeTool) { f.meta["capabilities"] = []any{"webhooks@2"} }, "does not offer mcp@1"},
		"no resource metadata":    {func(f *fakeTool) { f.resource = nil }, "protected resource metadata"},
		"another resource":        {func(f *fakeTool) { f.resource["resource"] = "https://elsewhere.test/api/v1/mcp" }, "does not hold its MCP endpoint"},
		"no authorization server": {func(f *fakeTool) { f.resource["authorization_servers"] = []string{} }, "names no authorization server"},
		"no server metadata":      {func(f *fakeTool) { f.server = nil }, "authorization server metadata"},
		"another issuer":          {func(f *fakeTool) { f.server["issuer"] = "https://evil.test" }, "calls itself"},
		"no S256":                 {func(f *fakeTool) { f.server["code_challenge_methods_supported"] = []string{"plain"} }, "S256"},
	} {
		f := newFakeTool(t)
		tc.bend(f)
		_, err := f.service().discover(context.Background(), f.base)
		var refusal *Refusal
		if !errors.As(err, &refusal) || !strings.Contains(refusal.Message, tc.says) || !strings.HasSuffix(refusal.Message, ".") {
			t.Errorf("%s: %v, want a sentence saying %q", name, err, tc.says)
		}
	}
}

func TestAToolThatDoesNotAnswerIsUnreachableNotRefused(t *testing.T) {
	f := newFakeTool(t)
	s := f.service()
	f.Close()
	if _, err := s.discover(context.Background(), f.base); !errors.Is(err, ErrUnreachable) {
		t.Errorf("a closed tool = %v, want ErrUnreachable", err)
	}
}

func TestRegistrationAsksForAConfidentialClientWithoutUnusedScopes(t *testing.T) {
	f := newFakeTool(t)
	s := f.service()
	d, err := s.discover(context.Background(), f.base)
	if err != nil {
		t.Fatal(err)
	}
	client, err := s.register(context.Background(), d, authBasic)
	if err != nil || client.ClientID != "c-1" || client.ClientSecret != "exciter_ocs_s" || client.AuthMethod != authBasic {
		t.Fatalf("registered %+v, %v", client, err)
	}
	if f.registered["client_name"] != "Rotor" || f.registered["token_endpoint_auth_method"] != authBasic ||
		f.registered["scope"] != "mcp mcp:read" || f.registered["redirect_uris"].([]any)[0] != s.redirectURL {
		t.Errorf("asked to register %v", f.registered)
	}

	f.register = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri","error_description":"Redirect URIs must use https."}`))
	}
	var refusal *Refusal
	if _, err := s.register(context.Background(), d, authBasic); !errors.As(err, &refusal) || !strings.Contains(refusal.Message, "must use https") {
		t.Errorf("a refused registration = %v", err)
	}
	d.RegistrationEndpoint = ""
	if _, err := s.register(context.Background(), d, authBasic); !errors.As(err, &refusal) || !strings.Contains(refusal.Message, s.redirectURL) {
		t.Errorf("a tool without registration = %v, want it to name the redirect URI to register by hand", err)
	}
}

func TestOnlyAToolsAddressIsAccepted(t *testing.T) {
	for in, want := range map[string]string{
		"https://exciter.test/":        "https://exciter.test",
		"http://localhost:8000/t/a/":   "http://localhost:8000/t/a",
		"http://127.0.0.1:9/":          "http://127.0.0.1:9",
		"http://exciter.test":          "",
		"ftp://exciter.test":           "",
		"https://exciter.test/?x=1":    "",
		"https://user:pw@exciter.test": "",
		"exciter":                      "",
	} {
		got, err := normalizeBaseURL(in, false)
		if want == "" && err == nil || want != "" && got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got, err := normalizeBaseURL("http://exciter:8080", true); err != nil || got != "http://exciter:8080" {
		t.Errorf("development accepts plain http: %q, %v", got, err)
	}
}
