package toolstub

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

const callback = "http://localhost:5173/api/v1/connections/callback"

// connection is what Rotor holds after connecting: its client and the tokens of one person's grant.
type connection struct {
	clientID, clientSecret string
	access, refresh, scope string
}

func pkce() (verifier, challenge string) {
	verifier = strings.Repeat("v", 20) + "-rotor-test-verifier-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// register makes a confidential client at a tenant, as Rotor registers itself.
func (c *contract) register(tenant string) (id, secret string) {
	c.t.Helper()
	made, _ := c.expect("POST", "/t/"+tenant+"/api/v1/oauth/register", request{json: map[string]any{
		"redirect_uris": []string{callback}, "client_name": "Rotor", "token_endpoint_auth_method": "client_secret_basic",
		"grant_types": []string{"authorization_code", "refresh_token"}}}, http.StatusCreated)
	return made["client_id"].(string), made["client_secret"].(string)
}

// authorize walks the browser's part: authorize, the consent form, and back to the callback with a code.
func (c *contract) authorize(tenant, clientID, username, scope, decision string) url.Values {
	c.t.Helper()
	issuer := c.base + "/t/" + tenant
	_, challenge := pkce()
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {callback}, "state": {"s-" + username},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {issuer + mcpPath}, "scope": {scope}}
	_, resp := c.expect("GET", "/t/"+tenant+"/api/v1/oauth/authorize?"+q.Encode(), request{}, http.StatusFound)
	page, _ := url.Parse(resp.Header.Get("Location"))
	if page.Path != "/t/"+tenant+"/oauth/consent" || page.Query().Get(FieldRequest) == "" {
		c.t.Fatalf("authorize sent the browser to %s, not the consent page", page)
	}
	form, _ := c.call("GET", page.RequestURI(), request{})
	if form.StatusCode != http.StatusOK {
		c.t.Fatalf("the consent page answered %d", form.StatusCode)
	}
	resp, _ = c.call("POST", "/t/"+tenant+"/oauth/consent", request{form: url.Values{FieldRequest: {page.Query().Get(FieldRequest)},
		FieldUsername: {username}, FieldDecision: {decision}}})
	back, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(back.String(), callback) {
		c.t.Fatalf("the consent answered %d to %s", resp.StatusCode, back)
	}
	if back.Query().Get("iss") != issuer || back.Query().Get("state") != "s-"+username {
		c.t.Errorf("the callback is missing its iss or state: %s", back)
	}
	return back.Query()
}

// connect registers, consents as username and trades the code, answering the connection.
func (c *contract) connect(tenant, username, scope string) connection {
	c.t.Helper()
	id, secret := c.register(tenant)
	back := c.authorize(tenant, id, username, scope, DecisionAllow)
	verifier, _ := pkce()
	tok, _ := c.expect("POST", "/t/"+tenant+"/api/v1/oauth/token", request{basicID: id, basicToken: secret, form: url.Values{
		"grant_type": {"authorization_code"}, "code": {back.Get("code")}, "redirect_uri": {callback}, "code_verifier": {verifier},
		"resource": {c.base + "/t/" + tenant + mcpPath}}}, http.StatusOK)
	return connection{clientID: id, clientSecret: secret, access: tok["access_token"].(string),
		refresh: tok["refresh_token"].(string), scope: tok["scope"].(string)}
}

func (c *contract) refresh(tenant string, conn connection, scope string, want int) (map[string]any, connection) {
	c.t.Helper()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {conn.refresh}}
	if scope != "" {
		form.Set("scope", scope)
	}
	out, _ := c.expect("POST", "/t/"+tenant+"/api/v1/oauth/token", request{basicID: conn.clientID, basicToken: conn.clientSecret, form: form}, want)
	if want == http.StatusOK {
		conn.access, conn.refresh, conn.scope = out["access_token"].(string), out["refresh_token"].(string), out["scope"].(string)
	}
	return out, conn
}

// mcp sends one JSON-RPC request and answers its result.
func (c *contract) mcp(tenant, bearer, method string, params any) map[string]any {
	c.t.Helper()
	out, _ := c.expect("POST", "/t/"+tenant+mcpPath, request{bearer: bearer, json: map[string]any{"jsonrpc": "2.0", "id": 1,
		"method": method, "params": params}}, http.StatusOK)
	result, _ := out["result"].(map[string]any)
	if result == nil {
		c.t.Fatalf("%s answered no result: %v", method, out)
	}
	return result
}

func (c *contract) whoami(tenant, bearer string) map[string]any {
	c.t.Helper()
	return c.mcp(tenant, bearer, "tools/call", map[string]any{"name": WhoamiTool, "arguments": map[string]any{}})["structuredContent"].(map[string]any)
}

func (c *contract) stats(tenant string) Stats {
	c.t.Helper()
	raw, _ := json.Marshal(c.controlled("GET", "/control/tenants/"+tenant+"/stats", nil, http.StatusOK))
	var s Stats
	_ = json.Unmarshal(raw, &s)
	return s
}

func TestEveryAnswerFitsExcitersDocument(t *testing.T) {
	c := newContract(t)
	issuer := c.base + "/t/acme"

	resp, raw := c.call("GET", "/t/acme/api/v1/meta", request{})
	for _, tool := range []string{"armature", "stator"} {
		validates(t, tool, "Meta", raw)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"mcp@1"`) {
		t.Errorf("meta = %d %s", resp.StatusCode, raw)
	}
	for _, path := range []string{"/t/acme/.well-known/oauth-protected-resource", "/t/acme/.well-known/oauth-protected-resource" + mcpPath,
		"/.well-known/oauth-protected-resource/t/acme" + mcpPath} {
		if pr, _ := c.expect("GET", path, request{}, http.StatusOK); pr["resource"] != issuer+mcpPath {
			t.Errorf("%s names the resource %v", path, pr["resource"])
		}
	}
	for _, path := range []string{"/t/acme/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/t/acme"} {
		if as, _ := c.expect("GET", path, request{}, http.StatusOK); as["issuer"] != issuer || as["token_endpoint"] != issuer+"/api/v1/oauth/token" {
			t.Errorf("%s describes %v", path, as)
		}
	}

	// Registration as Exciter refuses it, and the authorize refusals that never reach the client.
	c.expect("POST", "/t/acme/api/v1/oauth/register", request{json: map[string]any{"redirect_uris": []string{"http://evil.test/cb"}}}, http.StatusBadRequest)
	c.expect("POST", "/t/acme/api/v1/oauth/register", request{json: map[string]any{"redirect_uris": []string{callback},
		"token_endpoint_auth_method": "private_key_jwt"}}, http.StatusBadRequest)
	_, resp = c.expect("GET", "/t/acme/api/v1/oauth/authorize?client_id=nobody", request{}, http.StatusFound)
	if !strings.HasSuffix(resp.Header.Get("Location"), "/oauth/consent?error=invalid_client") {
		t.Errorf("an unknown client went to %s", resp.Header.Get("Location"))
	}
	id, secret := c.register("acme")
	_, resp = c.expect("GET", "/t/acme/api/v1/oauth/authorize?"+url.Values{"response_type": {"code"}, "client_id": {id},
		"redirect_uri": {callback}, "state": {"s"}, "code_challenge": {strings.Repeat("a", 43)}, "code_challenge_method": {"S256"},
		"resource": {"https://elsewhere.test/mcp"}}.Encode(), request{}, http.StatusFound)
	if back, _ := url.Parse(resp.Header.Get("Location")); back.Query().Get("error") != errInvalidTarget || back.Query().Get("iss") != issuer {
		t.Errorf("a wrong resource went back as %s", back)
	}
	if denied := c.authorize("acme", id, "bob", "", DecisionDeny); denied.Get("error") != errAccessDenied {
		t.Errorf("cancelling went back as %v", denied)
	}
	c.expect("POST", "/t/acme/api/v1/oauth/token", request{basicID: id, basicToken: secret + "x", form: url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {"stub_ort_nothing"}}}, http.StatusUnauthorized)
	c.expect("POST", "/t/acme/api/v1/oauth/token", request{form: url.Values{"grant_type": {"refresh_token"}, "client_id": {id},
		"client_secret": {secret}, "refresh_token": {"stub_ort_nothing"}}}, http.StatusUnauthorized)

	// A connection, what MCP answers for it, and refusals at MCP.
	bob := c.connect("acme", "bob", "mcp mcp:read")
	if bob.scope != ScopeMCP {
		t.Errorf("a grant of everything has scope %q", bob.scope)
	}
	if init := c.mcp("acme", bob.access, "initialize", map[string]any{"protocolVersion": "2025-06-18"}); init["protocolVersion"] != "2025-06-18" {
		t.Errorf("initialize answered %v", init)
	}
	if tools := c.mcp("acme", bob.access, "tools/list", nil)["tools"].([]any); len(tools) != len(newTestStub(t).tools) {
		t.Errorf("tools/list answered %d tools", len(tools))
	}
	if me := c.whoami("acme", bob.access); nameOf(me) != "bob" || !slices.Contains(scopesOf(me), ScopeMCP) {
		t.Errorf("whoami answered %v", me)
	}
	_, resp = c.expect("POST", "/t/acme"+mcpPath, request{json: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}}, http.StatusUnauthorized)
	if challenge := resp.Header.Get("WWW-Authenticate"); !strings.Contains(challenge, `resource_metadata="`+issuer+"/.well-known/oauth-protected-resource"+mcpPath) {
		t.Errorf("a 401 at MCP challenges with %q", challenge)
	}
	c.expect("POST", "/t/acme"+mcpPath, request{bearer: bob.refresh, json: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}}, http.StatusUnauthorized)

	// Refresh tokens rotate; the replaced one coming back ends the grant and everything it made.
	_, rotated := c.refresh("acme", bob, "", http.StatusOK)
	c.refresh("acme", bob, "", http.StatusBadRequest)
	if s := c.stats("acme"); s.Refreshes != 1 || s.ReuseEvents != 1 || s.LiveGrants != 0 {
		t.Errorf("after a reuse the stats read %+v", s)
	}
	c.refresh("acme", rotated, "", http.StatusBadRequest)
	c.expect("POST", "/t/acme"+mcpPath, request{bearer: rotated.access, json: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}}, http.StatusUnauthorized)

	// Narrowing on a refresh sticks, as it does at Exciter, and a read-only token sees only the reading tools.
	carol := c.connect("acme", "carol", "")
	_, carol = c.refresh("acme", carol, ScopeMCPRead, http.StatusOK)
	_, carol = c.refresh("acme", carol, "", http.StatusOK)
	if carol.scope != ScopeMCPRead || !slices.Equal(scopesOf(c.whoami("acme", carol.access)), []string{ScopeMCPRead}) {
		t.Errorf("a narrowed grant refreshed to %q", carol.scope)
	}
	c.refresh("acme", carol, ScopeMCP, http.StatusBadRequest)
	if read := c.mcp("acme", carol.access, "tools/list", nil)["tools"].([]any); len(read) >= len(newTestStub(t).tools) {
		t.Errorf("a read-only token lists %d tools", len(read))
	}

	// Revoking an access token ends it alone; revoking a refresh token ends the grant.
	c.expect("POST", "/t/acme/api/v1/oauth/revoke", request{basicID: carol.clientID, basicToken: carol.clientSecret,
		form: url.Values{"token": {carol.access}}}, http.StatusOK)
	c.expect("POST", "/t/acme"+mcpPath, request{bearer: carol.access, json: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}}, http.StatusUnauthorized)
	_, carol = c.refresh("acme", carol, "", http.StatusOK)
	c.expect("POST", "/t/acme/api/v1/oauth/revoke", request{basicID: carol.clientID, basicToken: carol.clientSecret,
		form: url.Values{"token": {carol.refresh}, "token_type_hint": {"refresh_token"}}}, http.StatusOK)
	c.refresh("acme", carol, "", http.StatusBadRequest)
	c.expect("POST", "/t/acme/api/v1/oauth/revoke", request{basicID: carol.clientID, basicToken: carol.clientSecret,
		form: url.Values{"token": {"stub_oat_unknown"}}}, http.StatusOK)

	for _, op := range []string{"GET /meta", "GET /.well-known/oauth-protected-resource", "GET /.well-known/oauth-protected-resource/api/v1/mcp",
		"GET /.well-known/oauth-authorization-server", "POST /oauth/register", "GET /oauth/authorize", "POST /oauth/token",
		"POST /oauth/revoke", "POST /mcp"} {
		if !c.served[op] {
			t.Errorf("the scenario never had %s answer with a success", op)
		}
	}
}

func TestTheControlAPIShapesTenants(t *testing.T) {
	c := newContract(t)
	c.controlled("POST", "/control/tenants", map[string]any{"name": "plain", "oauth": false, "product": "stator", "capabilities": []string{}}, http.StatusCreated)
	c.expect("GET", "/t/plain/.well-known/oauth-authorization-server", request{}, http.StatusNotFound)
	c.expect("POST", "/t/plain/api/v1/oauth/register", request{json: map[string]any{"redirect_uris": []string{callback}}}, http.StatusNotFound)
	if meta, _ := c.expect("GET", "/t/plain/api/v1/meta", request{}, http.StatusOK); meta["product"] != "stator" || len(meta["capabilities"].([]any)) != 0 {
		t.Errorf("a tenant told otherwise answers %v", meta)
	}
	if me := c.whoami("plain", PATPrefix+"dave"); nameOf(me) != "dave" {
		t.Errorf("a personal token acts for %v", me)
	}

	c.controlled("POST", "/control/tenants", map[string]any{"name": "lenient", "reuseDetection": false}, http.StatusCreated)
	bob := c.connect("lenient", "bob", "")
	_, rotated := c.refresh("lenient", bob, "", http.StatusOK)
	c.refresh("lenient", bob, "", http.StatusBadRequest)
	if _, again := c.refresh("lenient", rotated, "", http.StatusOK); again.access == "" {
		t.Error("without reuse detection the grant should outlive a replayed refresh token")
	}

	// The tool ending a grant on its own, and forgetting its clients, as Rotor will meet them.
	alice := c.connect("acme", "alice", "")
	dave := c.connect("acme", "dave", "")
	if ended := c.controlled("DELETE", "/control/tenants/acme/grants?username=alice", nil, http.StatusOK); ended["ended"] != float64(1) {
		t.Errorf("ending alice's grants answered %v", ended)
	}
	c.refresh("acme", alice, "", http.StatusBadRequest)
	c.refresh("acme", dave, "", http.StatusOK)
	c.controlled("DELETE", "/control/tenants/acme/clients", nil, http.StatusOK)
	c.refresh("acme", dave, "", http.StatusUnauthorized)

	c.controlled("GET", "/control/tenants/nobody/stats", nil, http.StatusNotFound)
	c.controlled("POST", "/control/tenants", map[string]any{"name": "Not A Name"}, http.StatusUnprocessableEntity)
	c.controlled("DELETE", "/control/tenants/acme", nil, http.StatusNoContent)
	c.controlled("GET", "/control/tenants/acme/stats", nil, http.StatusNotFound)
	c.controlled("GET", "/healthz", nil, http.StatusNoContent)
	c.expect("GET", "/t/Not%20A%20Name/api/v1/meta", request{}, http.StatusNotFound)
}

// The issuer is the address the browser uses, whichever way the request reached the stub, so the api behind a
// rewrite and the browser behind a proxy see one issuer.
func TestTheIssuerIsThePublicAddress(t *testing.T) {
	c := newContract(t)
	req, _ := http.NewRequest("GET", c.base+"/.well-known/oauth-authorization-server/t/acme", nil)
	req.Host = "tools.example:8443"
	req.Header.Set("X-Forwarded-Host", "rotor.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var server map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&server)
	if server["issuer"] != "https://rotor.example/t/acme" || server["authorization_endpoint"] != "https://rotor.example/t/acme/api/v1/oauth/authorize" {
		t.Errorf("behind a proxy the metadata reads %v", server)
	}
}

// nameOf and scopesOf read whoami's answer, which has the shape of Exciter's.
func nameOf(me map[string]any) string {
	user, _ := me["user"].(map[string]any)
	name, _ := user["name"].(string)
	return name
}

func scopesOf(me map[string]any) []string {
	token, _ := me["token"].(map[string]any)
	var out []string
	for _, s := range token["scopes"].([]any) {
		out = append(out, s.(string))
	}
	return out
}

// The stub model answers from its tenant's script behind its key, in OpenAI's shapes, streamed or not.
func TestTheStubModelAnswersFromItsScript(t *testing.T) {
	stub := newTestStub(t)
	public, control := httptest.NewServer(stub.Public()), httptest.NewServer(stub.Control())
	t.Cleanup(public.Close)
	t.Cleanup(control.Close)
	send := func(base, method, path, bearer string, body any) (int, string) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			encoded, _ := json.Marshal(body)
			reader = bytes.NewReader(encoded)
		}
		req, _ := http.NewRequest(method, base+path, reader)
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	script := []map[string]any{{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}
	if status, raw := send(control.URL, "POST", "/control/tenants", "", map[string]any{"name": "model", "llmKey": "k", "llmScript": script}); status != http.StatusCreated {
		t.Fatalf("make the tenant = %d %s", status, raw)
	}
	if status, _ := send(public.URL, "GET", "/t/model"+llmPath+"/models", "wrong", nil); status != http.StatusUnauthorized {
		t.Errorf("models with the wrong key = %d", status)
	}
	if status, raw := send(public.URL, "GET", "/t/model"+llmPath+"/models", "k", nil); status != http.StatusOK || !strings.Contains(raw, StubModel) {
		t.Errorf("models = %d %s", status, raw)
	}
	status, raw := send(public.URL, "POST", "/t/model"+llmPath+"/chat/completions", "k", map[string]any{"model": StubModel, "messages": []any{}})
	if status != http.StatusOK || !strings.Contains(raw, `"finish_reason":"tool_calls"`) || !strings.Contains(raw, `"name":"whoami"`) {
		t.Errorf("the first turn = %d %s", status, raw)
	}
	status, raw = send(public.URL, "POST", "/t/model"+llmPath+"/chat/completions", "k",
		map[string]any{"model": StubModel, "messages": []any{}, "stream": true, "stream_options": map[string]any{"include_usage": true}})
	if status != http.StatusOK || !strings.Contains(raw, "You are bob.") || !strings.Contains(raw, `"usage"`) || !strings.HasSuffix(strings.TrimSpace(raw), "data: [DONE]") {
		t.Errorf("the streamed second turn = %d %s", status, raw)
	}
}
