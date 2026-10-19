package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/tools"
)

type fakeTools struct {
	list    []tools.Tool
	addErr  error
	known   bool
	added   tools.AddRequest
	removed uuid.UUID
	checked []string
}

func (f *fakeTools) List(context.Context) ([]tools.Tool, error) { return f.list, nil }

func (f *fakeTools) Get(_ context.Context, id uuid.UUID) (tools.Tool, error) {
	for _, t := range f.list {
		if t.ID == id {
			return t, nil
		}
	}
	return tools.Tool{}, tools.ErrNotFound
}

func (f *fakeTools) Add(_ context.Context, req tools.AddRequest) (tools.Tool, bool, error) {
	f.added = req
	return tools.Tool{ID: uuid.New(), Name: "exciter", BaseURL: req.BaseURL, Status: tools.StatusReady, AddedAt: time.Now()}, !f.known, f.addErr
}

func (f *fakeTools) Remove(_ context.Context, id uuid.UUID) error {
	if _, err := f.Get(context.Background(), id); err != nil {
		return err
	}
	f.removed = id
	return nil
}

func (f *fakeTools) Check(_ context.Context, id uuid.UUID) (tools.Tool, error) {
	for i, t := range f.list {
		if t.ID == id {
			f.list[i].Capabilities = f.checked
			return f.list[i], nil
		}
	}
	return tools.Tool{}, tools.ErrNotFound
}

func toolsServer(t *testing.T) (*Server, *fakeTools) {
	t.Helper()
	s, sessions, _ := signedInServer(t)
	sessions.people["bob"] = &auth.Principal{UserID: uuid.New(), Name: "Bob Builder", Role: auth.RoleMember}
	registry := &fakeTools{list: []tools.Tool{{ID: uuid.New(), Name: "exciter", BaseURL: "https://exciter.test",
		Capabilities: []string{"mcp@1"}, ScopesSupported: []string{"mcp", "mcp:read"}, Registration: tools.RegistrationDynamic, Status: tools.StatusReady}}}
	s.Tools = registry
	return s, registry
}

func TestEveryoneSignedInSeesTheTools(t *testing.T) {
	s, registry := toolsServer(t)
	resp, body := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/tools", nil), "bob"))
	listed, _ := body["tools"].([]any)
	if resp.StatusCode != http.StatusOK || len(listed) != 1 || listed[0].(map[string]any)["status"] != "ready" {
		t.Fatalf("GET /tools as a member = %d %v", resp.StatusCode, body)
	}
	resp, _ = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/tools/"+registry.list[0].ID.String(), nil), "bob"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET one tool = %d", resp.StatusCode)
	}
	resp, body = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/tools/"+uuid.NewString(), nil), "bob"))
	if resp.StatusCode != http.StatusNotFound || errorOf(t, body)["code"] != "not_found" {
		t.Errorf("GET an unknown tool = %d %v, want 404 in a sentence", resp.StatusCode, body)
	}
}

func TestAddingAToolAnswersCreatedOrRegisteredAgain(t *testing.T) {
	s, registry := toolsServer(t)
	add := func(who, body string) (*http.Response, map[string]any) {
		return serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPost, APIPrefix+"/tools", body), who))
	}
	if resp, body := add("bob", `{"baseURL":"https://exciter.test"}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member adding a tool = %d %v", resp.StatusCode, body)
	}
	if resp, _ := add("alice", `{"baseURL":"https://exciter.test","clientID":"c","clientSecret":"s"}`); resp.StatusCode != http.StatusCreated ||
		registry.added.ClientSecret != "s" {
		t.Errorf("adding a tool = %d, asked %+v", resp.StatusCode, registry.added)
	}
	registry.known = true
	if resp, _ := add("alice", `{"baseURL":"https://exciter.test"}`); resp.StatusCode != http.StatusOK {
		t.Errorf("registering a known tool again = %d, want 200", resp.StatusCode)
	}
	if resp, body := add("alice", `{"baseURL":" "}`); resp.StatusCode != http.StatusUnprocessableEntity || errorOf(t, body)["fields"] == nil {
		t.Errorf("an empty address = %d %v", resp.StatusCode, body)
	}

	for err, want := range map[error]struct {
		status int
		code   string
	}{
		&tools.Refusal{Message: "Exciter does not offer mcp@1."}: {http.StatusUnprocessableEntity, "tool_refused"},
		fmt.Errorf("%w: dial", tools.ErrUnreachable):             {http.StatusBadGateway, "tool_unreachable"},
	} {
		registry.addErr = err
		resp, body := add("alice", `{"baseURL":"https://exciter.test"}`)
		if resp.StatusCode != want.status || errorOf(t, body)["code"] != want.code {
			t.Errorf("%v = %d %v, want %d %s", err, resp.StatusCode, body, want.status, want.code)
		}
	}
}

func TestOnlyAnAdministratorRemovesATool(t *testing.T) {
	s, registry := toolsServer(t)
	path := APIPrefix + "/tools/" + registry.list[0].ID.String()
	if resp, _ := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodDelete, path, nil), "bob")); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member removing a tool = %d", resp.StatusCode)
	}
	if resp, _ := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodDelete, path, nil), "alice")); resp.StatusCode != http.StatusNoContent ||
		registry.removed != registry.list[0].ID {
		t.Fatalf("an administrator removing a tool = %d", resp.StatusCode)
	}
}

func TestWithoutOpenBaoTheToolsAreUnavailable(t *testing.T) {
	s, _ := toolsServer(t)
	s.Tools = nil
	resp, body := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/tools", nil), "bob"))
	if resp.StatusCode != http.StatusServiceUnavailable || errorOf(t, body)["code"] != "tools_unavailable" {
		t.Fatalf("GET /tools without OpenBao = %d %v", resp.StatusCode, body)
	}
}

// A tool updated to a release without what Rotor needs is kept, and says what is missing in a sentence.
func TestCheckingAToolAgainNamesWhatItNoLongerOffers(t *testing.T) {
	s, registry := toolsServer(t)
	path := APIPrefix + "/tools/" + registry.list[0].ID.String() + "/check"
	if resp, _ := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodPost, path, nil), "bob")); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member checking a tool = %d", resp.StatusCode)
	}
	registry.checked = []string{"webhooks@1"}
	resp, body := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodPost, path, nil), "alice"))
	tool, _ := body["tool"].(map[string]any)
	missing, _ := tool["missing"].([]any)
	if resp.StatusCode != http.StatusOK || len(missing) != 1 {
		t.Fatalf("checking a tool = %d %v", resp.StatusCode, body)
	}
	first, _ := missing[0].(map[string]any)
	if first["capability"] != "mcp@1" || !strings.Contains(fmt.Sprint(first["message"]), "Update Exciter") {
		t.Errorf("missing = %v, want mcp@1 with what to update", first)
	}
	resp, _ = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodPost, APIPrefix+"/tools/"+uuid.NewString()+"/check", nil), "alice"))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("checking an unknown tool = %d, want 404", resp.StatusCode)
	}
}
