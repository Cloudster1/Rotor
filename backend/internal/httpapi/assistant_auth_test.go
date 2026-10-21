package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/oauth"
)

// fakeOAuth knows one access token per principal; what a test does not call stays unimplemented.
type fakeOAuth struct {
	OAuthServer
	tokens map[string]*auth.Principal
	off    bool
}

func (f *fakeOAuth) Authenticate(_ context.Context, token string) (*auth.Principal, error) {
	if f.off {
		return nil, oauth.ErrSwitchedOff
	}
	if p, ok := f.tokens[token]; ok {
		return p, nil
	}
	return nil, oauth.ErrInvalidToken
}

func (f *fakeOAuth) Enabled(context.Context) (bool, error) { return !f.off, nil }

func (f *fakeOAuth) ResourceMetadata() string {
	return "http://app.test/.well-known/oauth-protected-resource" + mcpPath
}

const (
	writerToken = oauth.AccessTokenPrefix + "writer"
	readerToken = oauth.AccessTokenPrefix + "reader"
)

func assistantServer(t *testing.T) (http.Handler, *fakeOAuth) {
	t.Helper()
	s, _, _ := signedInServer(t)
	f := &fakeOAuth{tokens: map[string]*auth.Principal{writerToken: assistant(false), readerToken: assistant(true)}}
	s.OAuth = f
	return s.Routes(nil), f
}

func bearer(method, path, token, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// rpc calls one MCP method with a token and answers its result.
func rpc(t *testing.T, h http.Handler, token, method, params string) (int, json.RawMessage) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bearer(http.MethodPost, mcpPath, token, body))
	var resp rpcResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp.Result
}

func TestAnAssistantsTokenActsThroughMCPAlone(t *testing.T) {
	h, _ := assistantServer(t)
	status, result := rpc(t, h, writerToken, "tools/call", `{"name":"whoami","arguments":{}}`)
	if status != http.StatusOK || !strings.Contains(string(result), `Claude Code`) || strings.Contains(string(result), `"isError":true`) {
		t.Fatalf("whoami through MCP: %d %s", status, result)
	}
	// The same token sent straight to the API is refused, wherever it is sent.
	for _, req := range []*http.Request{
		bearer(http.MethodGet, APIPrefix+"/auth/me", writerToken, ""),
		bearer(http.MethodPost, APIPrefix+"/approvals/"+uuid.NewString()+"/decision", writerToken, `{"decision":"approve"}`),
		bearer(http.MethodGet, APIPrefix+"/runs", writerToken, ""),
	} {
		resp, body := serve(t, h, req)
		if resp.StatusCode != http.StatusUnauthorized || errorOf(t, body)["code"] != "invalid_token" {
			t.Errorf("%s %s with an assistant's token: %d %v", req.Method, req.URL.Path, resp.StatusCode, body)
		}
	}
}

func TestMCPTellsAnAssistantWhereToLearnHowToBeLetIn(t *testing.T) {
	h, f := assistantServer(t)
	for token, invalid := range map[string]bool{"": false, oauth.AccessTokenPrefix + "unknown": true, "not-ours": true} {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, _ := serve(t, h, req)
		challenge := resp.Header.Get("WWW-Authenticate")
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, `resource_metadata="`+f.ResourceMetadata()+`"`) ||
			!strings.Contains(challenge, `scope="mcp"`) || strings.Contains(challenge, "invalid_token") != invalid {
			t.Errorf("token %q: %d %q", token, resp.StatusCode, challenge)
		}
	}
	f.off = true
	resp, body := serve(t, h, bearer(http.MethodPost, mcpPath, writerToken, `{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(errorOf(t, body)["message"].(string), "switched off") {
		t.Errorf("assistants switched off: %d %v", resp.StatusCode, body)
	}
}

func TestAnAssistantNeverDoesWhatAPersonDoesAtAKeyboard(t *testing.T) {
	ctx := auth.WithPrincipal(context.Background(), assistant(false))
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	requireSession(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil).WithContext(ctx))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "session_only") {
		t.Errorf("an assistant passes requireSession: %d %s", rec.Code, rec.Body.String())
	}
	person := auth.WithPrincipal(context.Background(), &auth.Principal{UserID: uuid.New(), SessionID: uuid.New()})
	rec = httptest.NewRecorder()
	requireSession(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil).WithContext(person))
	if rec.Code != http.StatusNoContent {
		t.Errorf("a person is refused by requireSession: %d", rec.Code)
	}
}

func TestAReadOnlyGrantWritesNothingButReadsAndChecks(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, c := range []struct {
		method, path string
		readOnly     bool
		allowed      bool
	}{
		{http.MethodPost, APIPrefix + "/runs/x/cancel", true, false},
		{http.MethodPost, APIPrefix + "/workflows/x/runs", true, false},
		{http.MethodGet, APIPrefix + "/runs", true, true},
		{http.MethodPost, APIPrefix + "/workflows/validate", true, true},
		{http.MethodPost, mcpPath, true, true},
		{http.MethodPost, APIPrefix + "/runs/x/cancel", false, true},
	} {
		ctx := auth.WithPrincipal(context.Background(), assistant(c.readOnly))
		rec := httptest.NewRecorder()
		readOnlyGrant(ok).ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil).WithContext(ctx))
		if (rec.Code == http.StatusNoContent) != c.allowed {
			t.Errorf("%s %s read only %v: %d", c.method, c.path, c.readOnly, rec.Code)
		}
	}
}

func TestTheClientEndpointsTakeFormsWhateverCookieRidesAlong(t *testing.T) {
	h, _ := assistantServer(t)
	req := withSession(httptest.NewRequest(http.MethodPost, APIPrefix+"/oauth/token", strings.NewReader("grant_type=x")), "alice")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://evil.test")
	resp, _ := serve(t, h, req)
	if resp.StatusCode == http.StatusUnsupportedMediaType || resp.StatusCode == http.StatusForbidden {
		t.Errorf("the token endpoint was held to the session's rules: %d", resp.StatusCode)
	}
}

func TestTheCallersAddressIsTheProxysWordOnlyFromATrustedProxy(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.18.0.5:41000"
	req.Header.Set(realIPHeader, "203.0.113.9")
	if got := s.clientIP(req); got != "172.18.0.5" {
		t.Errorf("an untrusted peer named %s", got)
	}
	s.TrustedProxies = mustPrefixes(t, "172.16.0.0/12")
	if got := s.clientIP(req); got != "203.0.113.9" {
		t.Errorf("a trusted proxy's word is %s", got)
	}
	req.Header.Set(realIPHeader, "nonsense")
	if got := s.clientIP(req); got != "172.18.0.5" {
		t.Errorf("a proxy naming nonsense gives %s", got)
	}
}

func mustPrefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}
