package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/hooks"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// realmToken stands for a client-credentials token of dynamo-connect; the fake knows no other.
const realmToken = "eyJhbGciOiJSUzI1NiJ9.realm.token"

// fakeServiceAccounts lets in dynamo-connect for realmToken, or answers err; asked counts the tokens it checked.
type fakeServiceAccounts struct {
	account *auth.Principal
	err     error
	asked   int
}

func (f *fakeServiceAccounts) Authenticate(_ context.Context, token string) (*auth.Principal, error) {
	f.asked++
	if f.err != nil {
		return nil, f.err
	}
	if token != realmToken {
		return nil, &oidc.Refusal{Reason: oidc.ErrTokenUnverified, Message: "The token is not signed with the realm's keys."}
	}
	return f.account, nil
}

// fakeHooks keeps one secret per tool and remembers who set it.
type fakeHooks struct {
	Hooks
	setBy  uuid.UUID
	secret string
}

func (f *fakeHooks) Get(_ context.Context, id uuid.UUID) (hooks.Hook, error) {
	return hooks.Hook{ToolID: id, Product: "exciter"}, nil
}

func (f *fakeHooks) SetSecret(ctx context.Context, id uuid.UUID, secret string) (hooks.Hook, error) {
	f.setBy, _ = db.UserFrom(ctx)
	f.secret = secret
	at := time.Now()
	return hooks.Hook{ToolID: id, Product: "exciter", SecretSetAt: &at}, nil
}

func serviceAccountServer(t *testing.T) (*Server, *fakeServiceAccounts, *fakeHooks) {
	t.Helper()
	s, _ := toolsServer(t)
	accounts := &fakeServiceAccounts{account: &auth.Principal{UserID: uuid.New(), Email: "dynamo-connect@service-account.invalid",
		Name: "dynamo-connect", Role: auth.RoleAdmin, Groups: []string{oidc.AdminGroup}}}
	h := &fakeHooks{}
	s.ServiceAccounts, s.Hooks = accounts, h
	return s, accounts, h
}

// concrete fills a path's parameters with ids, as a client calling it would.
func concrete(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") {
			parts[i] = uuid.NewString()
			if part == "{versionNumber}" {
				parts[i] = "1"
			}
		}
	}
	return strings.Join(parts, "/")
}

// The token is checked at the rows marked serviceAccount and nowhere else; everywhere a person signs in for, it is
// refused in a sentence that names the routes that take it.
func TestARealmTokenIsCheckedAtTheServiceAccountRoutesAlone(t *testing.T) {
	s, accounts, _ := serviceAccountServer(t)
	// Rotor's authorization server is set up, so its routes ask who is signed in rather than answering 503.
	s.OAuth = &fakeOAuth{}
	h := s.Routes(nil)
	marked := 0
	for _, op := range operations {
		if op.root {
			continue
		}
		path := APIPrefix + op.path
		accounts.asked = 0
		// Public routes answer whatever they answer, HTML and redirects included, so the body is read only below.
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, bearer(op.method, concrete(path), realmToken, `{}`))
		key := op.method + " " + op.path
		switch {
		case op.serviceAccount:
			marked++
			if accounts.asked != 1 || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Errorf("%s with the realm's token = %d, checked %d times", key, rec.Code, accounts.asked)
			}
		case accounts.asked != 0:
			t.Errorf("%s checked the realm's token, which only the serviceAccount rows take", key)
		case !op.public && op.path != "/mcp":
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			msg, _ := errorOf(t, body)["message"].(string)
			if rec.Code != http.StatusUnauthorized || !strings.Contains(msg, "POST "+APIPrefix+"/tools,") ||
				!strings.Contains(msg, "PUT "+APIPrefix+"/tools/{toolID}/hook") {
				t.Errorf("%s with the realm's token = %d %q, want 401 naming the routes that take one", key, rec.Code, msg)
			}
		}
	}
	if marked != 4 {
		t.Errorf("%d rows take the realm's token, want the four of #168", marked)
	}
}

func TestARealmTokenAddsAToolAndSetsItsHookSecretFromAnywhere(t *testing.T) {
	s, accounts, h := serviceAccountServer(t)
	routes := s.Routes(nil)
	// A bearer request rides on no cookie, so the origin it names is not asked about.
	req := bearer(http.MethodPost, APIPrefix+"/tools", realmToken, `{"baseURL":"https://exciter.test"}`)
	req.Header.Set("Origin", "https://elsewhere.test")
	if resp, body := serve(t, routes, req); resp.StatusCode != http.StatusCreated {
		t.Fatalf("adding a tool with the realm's token = %d %v", resp.StatusCode, body)
	}
	resp, body := serve(t, routes, bearer(http.MethodGet, APIPrefix+"/tools", realmToken, ""))
	listed, _ := body["tools"].([]any)
	if resp.StatusCode != http.StatusOK || len(listed) == 0 {
		t.Fatalf("listing the tools with the realm's token = %d %v", resp.StatusCode, body)
	}
	tool, _ := listed[0].(map[string]any)
	id, _ := tool["id"].(string)
	if tool["baseURL"] != "https://exciter.test" || tool["hookURL"] != "http://app.test"+APIPrefix+"/hooks/"+id {
		t.Errorf("the tool = %v, want its base URL and hook URL", tool)
	}
	req = bearer(http.MethodPut, APIPrefix+"/tools/"+id+"/hook", realmToken, `{"secret":"exciter_whs_x"}`)
	req.Header.Set("Origin", "https://elsewhere.test")
	if resp, body := serve(t, routes, req); resp.StatusCode != http.StatusOK || body["url"] != tool["hookURL"] {
		t.Fatalf("setting the hook secret = %d %v", resp.StatusCode, body)
	}
	if h.secret != "exciter_whs_x" || h.setBy != accounts.account.UserID {
		t.Errorf("the secret %q was set by %v, want by the service account", h.secret, h.setBy)
	}
	if resp, _ := serve(t, routes, bearer(http.MethodGet, APIPrefix+"/tools/"+id+"/hook", realmToken, "")); resp.StatusCode != http.StatusOK {
		t.Errorf("reading the hook = %d", resp.StatusCode)
	}
}

func TestARefusedRealmTokenSaysWhatIsMissing(t *testing.T) {
	s, accounts, _ := serviceAccountServer(t)
	routes := s.Routes(nil)
	for name, c := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"not in the group":  {&oidc.Refusal{Reason: oidc.ErrTokenNotAdmin, Message: "Not in rotor-administrators."}, http.StatusForbidden, "forbidden"},
		"for another":       {&oidc.Refusal{Reason: oidc.ErrTokenAudience, Message: "Not for rotor."}, http.StatusUnauthorized, "invalid_token"},
		"switched off":      {auth.ErrSwitchedOff, http.StatusForbidden, "account_switched_off"},
		"no realm":          {oidc.ErrNotConfigured, http.StatusUnauthorized, "invalid_token"},
		"realm unreachable": {fmt.Errorf("%w: down", oidc.ErrUnreachable), http.StatusServiceUnavailable, "realm_unreachable"},
	} {
		accounts.err = c.err
		resp, body := serve(t, routes, bearer(http.MethodPost, APIPrefix+"/tools", realmToken, `{"baseURL":"https://exciter.test"}`))
		e := errorOf(t, body)
		var refusal *oidc.Refusal
		if resp.StatusCode != c.status || e["code"] != c.code || (errors.As(c.err, &refusal) && e["message"] != refusal.Message) {
			t.Errorf("%s = %d %v, want %d %s", name, resp.StatusCode, e, c.status, c.code)
		}
		if c.status == http.StatusUnauthorized && c.err != oidc.ErrNotConfigured && resp.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("%s answers no challenge", name)
		}
	}

	s.ServiceAccounts = nil
	resp, body := serve(t, s.Routes(nil), bearer(http.MethodGet, APIPrefix+"/tools", realmToken, ""))
	if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "invalid_token" {
		t.Errorf("a realm token where no realm is configured = %d %v", resp.StatusCode, body)
	}
}

// A cookie write from a foreign origin is refused as before, at the routes that also take the realm's token.
func TestTheOriginCheckStillHoldsForCookieWrites(t *testing.T) {
	s, accounts, _ := serviceAccountServer(t)
	req := withSession(httptestRequest(http.MethodPost, APIPrefix+"/tools", `{"baseURL":"https://exciter.test"}`), "alice")
	req.Header.Set("Origin", "https://elsewhere.test")
	resp, body := serve(t, s.Routes(nil), req)
	if resp.StatusCode != http.StatusForbidden || errorOf(t, body)["code"] != "cross_site" || accounts.asked != 0 {
		t.Errorf("a cross-site cookie write = %d %v", resp.StatusCode, body)
	}
}

// An assistant's token stays good for MCP alone, at the routes that take the realm's token too.
func TestAnAssistantsTokenIsNotTakenForTheRealms(t *testing.T) {
	s, accounts, _ := serviceAccountServer(t)
	s.OAuth = &fakeOAuth{tokens: map[string]*auth.Principal{writerToken: assistant(false)}}
	resp, _ := serve(t, s.Routes(nil), bearer(http.MethodPost, APIPrefix+"/tools", writerToken, `{"baseURL":"https://exciter.test"}`))
	if resp.StatusCode != http.StatusUnauthorized || accounts.asked != 0 {
		t.Errorf("an assistant's token adding a tool = %d, checked as the realm's %d times", resp.StatusCode, accounts.asked)
	}
	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/tools", nil)
	req.Header.Set("Authorization", "Basic "+realmToken)
	if resp, _ := serve(t, s.Routes(nil), req); resp.StatusCode != http.StatusUnauthorized || accounts.asked != 0 {
		t.Errorf("a token under another scheme = %d", resp.StatusCode)
	}
}

func TestHookURLsUseTheAddressToolsDeliverToWhenSet(t *testing.T) {
	s, _, _ := serviceAccountServer(t)
	id := uuid.New()
	if got, want := s.hookURL(id), "http://app.test"+APIPrefix+"/hooks/"+id.String(); got != want {
		t.Errorf("without the setting = %q, want %q", got, want)
	}
	s.HooksBaseURL = "http://rotor-api:8080/"
	if got, want := s.hookURL(id), "http://rotor-api:8080"+APIPrefix+"/hooks/"+id.String(); got != want {
		t.Errorf("with the setting = %q, want %q", got, want)
	}
}
