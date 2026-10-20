package toolstub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// armatureCall sends one request to an Armature tenant past the contract's check, since Armature's answers are not
// Exciter's, and answers the status, the JSON body and the response.
func (c *contract) armatureCall(method, path string, in request) (int, map[string]any, *http.Response) {
	c.t.Helper()
	var body io.Reader
	if in.form != nil {
		body = strings.NewReader(in.form.Encode())
	}
	req, _ := http.NewRequest(method, c.base+path, body)
	if in.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if in.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+in.bearer)
	}
	if in.basicID != "" {
		req.SetBasicAuth(url.QueryEscape(in.basicID), url.QueryEscape(in.basicToken))
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp
}

// An Armature tenant holds a new client until allowed, keeps one access token per grant, and trades it for tokens of
// a run's own that end alone, as Rotor relies on (#45).
func TestAnArmatureTenantHandsOutOneTokenPerGrantAndExchangesIt(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--x"
	issuer := c.base + "/t/" + tenant

	if status, _, resp := c.armatureCall("POST", "/t/"+tenant+mcpPath, request{}); status != http.StatusUnauthorized ||
		!strings.Contains(resp.Header.Get("WWW-Authenticate"), `resource_metadata="`+issuer+`/.well-known/oauth-protected-resource"`) {
		t.Fatalf("MCP without a token = %d %q", status, resp.Header.Get("WWW-Authenticate"))
	}
	if status, meta, _ := c.armatureCall("GET", "/t/"+tenant+"/.well-known/oauth-protected-resource", request{}); status != http.StatusOK ||
		meta["resource"] != issuer+apiPrefix || strings.Contains(stringsOf(meta["scopes_supported"]), ArmatureWebhooksPartner) {
		t.Errorf("the resource metadata = %d %v", status, meta)
	}
	if status, _, _ := c.armatureCall("GET", "/.well-known/oauth-protected-resource/t/"+tenant+mcpPath, request{}); status != http.StatusNotFound {
		t.Errorf("the path-inserted metadata = %d, want none at Armature", status)
	}

	made, _ := c.expect("POST", "/t/"+tenant+"/api/v1/oauth/register", request{json: map[string]any{
		"redirect_uris": []string{callback}, "client_name": "Rotor", "token_endpoint_auth_method": "client_secret_basic",
		"grant_types": []string{"authorization_code", "refresh_token", grantExchange}}}, http.StatusCreated)
	id, secret := made["client_id"].(string), made["client_secret"].(string)
	if status, _, _ := c.armatureCall("POST", "/t/"+tenant+"/api/v1/oauth/token", request{basicID: id, basicToken: secret,
		form: url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}}); status != http.StatusUnauthorized {
		t.Errorf("a pending client at the token endpoint = %d, want 401", status)
	}
	c.controlled("POST", "/control/tenants/"+tenant+"/clients/approve", nil, http.StatusOK)

	_, challenge := pkce()
	q := url.Values{"response_type": {"code"}, "client_id": {id}, "redirect_uri": {callback}, "state": {"s"}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "resource": {issuer + apiPrefix}, "scope": {"read issue.write"}}
	_, _, resp := c.armatureCall("GET", "/t/"+tenant+"/api/v1/oauth/authorize?"+q.Encode(), request{})
	page, _ := url.Parse(resp.Header.Get("Location"))
	_, _, resp = c.armatureCall("POST", "/t/"+tenant+"/oauth/consent", request{form: url.Values{FieldRequest: {page.Query().Get(FieldRequest)},
		FieldUsername: {"alice"}, FieldDecision: {DecisionAllow}}})
	back, _ := url.Parse(resp.Header.Get("Location"))
	verifier, _ := pkce()
	token := func(form url.Values) map[string]any {
		t.Helper()
		status, out, _ := c.armatureCall("POST", "/t/"+tenant+"/api/v1/oauth/token", request{basicID: id, basicToken: secret, form: form})
		if status != http.StatusOK {
			t.Fatalf("the token endpoint = %d %v", status, out)
		}
		return out
	}
	first := token(url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "redirect_uri": {callback},
		"code_verifier": {verifier}, "resource": {issuer + apiPrefix}})
	if first["scope"] != "issue.write read" {
		t.Errorf("the grant's scope = %v", first["scope"])
	}
	exchange := func(subject, scope string) map[string]any {
		return token(url.Values{"grant_type": {grantExchange}, "subject_token": {subject}, "subject_token_type": {tokenTypeAccess},
			"scope": {scope}, lifetimeParam: {"600"}})
	}
	runA := exchange(first["access_token"].(string), "read")
	second := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first["refresh_token"].(string)}})
	runB := exchange(second["access_token"].(string), "read issue.write")
	if runA["expires_in"] != float64(600) || runA["scope"] != "read" {
		t.Errorf("the exchanged token = %v", runA)
	}

	mcp := func(bearer string) int {
		status, _, _ := c.armatureCall("POST", "/t/"+tenant+mcpPath, request{bearer: bearer})
		return status
	}
	if mcp(first["access_token"].(string)) != http.StatusUnauthorized {
		t.Error("the grant's first access token still works after a refresh replaced it")
	}
	if mcp(runA["access_token"].(string)) == http.StatusUnauthorized || mcp(runB["access_token"].(string)) == http.StatusUnauthorized {
		t.Error("a run's exchanged token stopped working when the grant was refreshed")
	}
	revoke := func(tok string) {
		c.armatureCall("POST", "/t/"+tenant+"/api/v1/oauth/revoke", request{basicID: id, basicToken: secret, form: url.Values{"token": {tok}}})
	}
	revoke(runA["access_token"].(string))
	if mcp(runA["access_token"].(string)) != http.StatusUnauthorized || mcp(runB["access_token"].(string)) == http.StatusUnauthorized ||
		c.stats(tenant).GrantsEnded != 0 {
		t.Error("revoking one run's token ended more than that token")
	}
	revoke(second["access_token"].(string))
	if mcp(runB["access_token"].(string)) != http.StatusUnauthorized || c.stats(tenant).GrantsEnded != 1 || c.stats(tenant).Exchanges != 2 {
		t.Errorf("revoking the grant's own token = %+v, want the grant and its exchanged tokens ended", c.stats(tenant))
	}
}

func stringsOf(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
