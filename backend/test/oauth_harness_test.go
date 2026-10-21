//go:build integration

package test

// Adapted from Exciter at 3b340ca, backend/test/oauth_harness_test.go: here the api is the stack's, reached at its
// public address through the stack's network, as an assistant on the person's machine reaches nginx.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/oauth"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// callback is where a hand-made assistant listens, on the person's own machine.
const callback = "http://127.0.0.1:9/callback"

// rotorApp is Rotor's public address, the issuer and the resource's origin, as ROTOR_APP_URL names it.
func rotorApp(t *testing.T) string {
	t.Helper()
	app := os.Getenv(envApp)
	if app == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envApp)
	}
	return strings.TrimSuffix(app, "/")
}

// fromAddress sends every request as if from one address of the test's own, as nginx names the caller, so each test
// registers within a limit of its own; the stack's api trusts the stack's networks to say so.
type fromAddress struct {
	ip   string
	next http.RoundTripper
}

func newAddress(t *testing.T) fromAddress {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fromAddress{ip: fmt.Sprintf("10.%d.%d.%d", b[0], b[1], b[2]),
		next: oidc.HTTPClient(map[string]string{rotorApp(t): apiURL(t)}).Transport}
}

func (f fromAddress) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Real-IP", f.ip)
	return f.next.RoundTrip(r)
}

// client is an HTTP client from the address; noFollow stops at the first redirect, to read where it would go.
func (f fromAddress) client(noFollow bool) *http.Client {
	c := &http.Client{Transport: f}
	if noFollow {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}

// forgetClients removes what the test registered from its address once it ends, as the worker's sweep would a day
// later, so the stack is left as it was.
func forgetClients(t *testing.T, h *harness, from fromAddress) {
	t.Cleanup(func() {
		if _, err := h.super.Exec(context.Background(), `DELETE FROM oauth_client WHERE registered_from = $1`, from.ip); err != nil {
			t.Errorf("forget the test's clients: %v", err)
		}
	})
}

// handClient is an assistant the test drives by hand: registered, with a verifier for its next authorization.
type handClient struct {
	id, secret string
	from       fromAddress
	verifier   string
}

// register registers an assistant; extra fields go into its metadata.
func register(t *testing.T, h *harness, from fromAddress, extra map[string]any) (*handClient, int, map[string]any) {
	t.Helper()
	forgetClients(t, h, from)
	body := map[string]any{"redirect_uris": []string{callback}, "client_name": "Hand-made assistant"}
	for k, v := range extra {
		body[k] = v
	}
	encoded, _ := json.Marshal(body)
	resp, err := from.client(false).Post(rotorApp(t)+httpapi.APIPrefix+"/oauth/register", "application/json", strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	id, _ := answer["client_id"].(string)
	secret, _ := answer["client_secret"].(string)
	return &handClient{id: id, secret: secret, from: from}, resp.StatusCode, answer
}

// resource is the MCP endpoint, the one resource Rotor's tokens are for.
func resource(t *testing.T) string { return rotorApp(t) + httpapi.APIPrefix + "/mcp" }

// authorizeURL is an authorization as the assistant would send the browser, with what the test changes.
func (c *handClient) authorizeURL(t *testing.T, change map[string]string) string {
	c.verifier = oauth2.GenerateVerifier()
	q := url.Values{
		"response_type": {"code"}, "client_id": {c.id}, "redirect_uri": {callback}, "state": {"the-state"},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(c.verifier)}, "code_challenge_method": {"S256"},
		"resource": {resource(t)}, "scope": {"mcp"},
	}
	for k, v := range change {
		if v == "" {
			q.Del(k)
		} else {
			q.Set(k, v)
		}
	}
	return rotorApp(t) + httpapi.APIPrefix + "/oauth/authorize?" + q.Encode()
}

// sentTo is where the browser goes from an address.
func sentTo(t *testing.T, from fromAddress, address string) *url.URL {
	t.Helper()
	resp, err := from.client(true).Get(address)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("%s answered %d, want a redirect", address, resp.StatusCode)
	}
	to, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return to
}

// consent follows an authorization as the person's browser would, to the consent page, and answers it there.
func consent(t *testing.T, from fromAddress, person *browser, address string, answer map[string]any) (*url.URL, error) {
	resp, err := from.client(true).Get(address)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	page, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || page.Path != oauth.ConsentPath || page.Query().Get("request") == "" {
		return nil, fmt.Errorf("the authorization went to %q, not to the consent page", resp.Header.Get("Location"))
	}
	path := "/oauth/requests/" + page.Query().Get("request")
	if status, got := person.api(http.MethodGet, path, nil); status != http.StatusOK {
		return nil, fmt.Errorf("the consent page reads %d: %v", status, got)
	}
	status, got := person.api(http.MethodPost, path, answer)
	if status != http.StatusOK {
		return nil, fmt.Errorf("the answer is %d: %v", status, got)
	}
	return url.Parse(got["redirectTo"].(string))
}

// allow connects a hand-made assistant as the person, and answers the code it is sent back with.
func (c *handClient) allow(t *testing.T, person *browser, answer map[string]any) string {
	t.Helper()
	back, err := consent(t, c.from, person, c.authorizeURL(t, nil), answer)
	if err != nil {
		t.Fatal(err)
	}
	if back.Query().Get("state") != "the-state" || back.Query().Get("iss") != rotorApp(t) || back.Query().Get("code") == "" {
		t.Fatalf("the browser goes back to %s", back)
	}
	return back.Query().Get("code")
}

// form posts a form to a client endpoint as the assistant, and answers the status and the answer.
func (c *handClient) form(t *testing.T, path string, form url.Values) (int, map[string]any) {
	t.Helper()
	if form.Get("client_id") == "" && c.secret == "" {
		form.Set("client_id", c.id)
	}
	req, _ := http.NewRequest(http.MethodPost, rotorApp(t)+httpapi.APIPrefix+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.secret != "" {
		req.SetBasicAuth(url.QueryEscape(c.id), url.QueryEscape(c.secret))
	}
	resp, err := c.from.client(false).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("%s answered without Cache-Control: no-store", path)
	}
	return resp.StatusCode, answer
}

// exchange trades a code for tokens, as an assistant that knows its verifier.
func (c *handClient) exchange(t *testing.T, code string) map[string]any {
	t.Helper()
	status, got := c.form(t, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback},
		"code_verifier": {c.verifier}, "resource": {resource(t)}})
	access, _ := got["access_token"].(string)
	if status != http.StatusOK || !strings.HasPrefix(access, oauth.AccessTokenPrefix) {
		t.Fatalf("the code was not traded: %d %v", status, got)
	}
	return got
}

// connectHand registers an assistant and connects it as the person, answering it and its access token.
func connectHand(t *testing.T, h *harness, person *browser, answer map[string]any) (*handClient, string) {
	t.Helper()
	c, status, got := register(t, h, newAddress(t), nil)
	if status != http.StatusCreated {
		t.Fatalf("register: %d %v", status, got)
	}
	return c, c.exchange(t, c.allow(t, person, answer))["access_token"].(string)
}

// mcpAs calls the MCP endpoint with a bearer, and answers the status, the challenge and the JSON-RPC result.
func mcpAs(t *testing.T, bearer, method string, params map[string]any) (int, string, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, resource(t), strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := newAddress(t).client(false).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer struct {
		Result map[string]any `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	return resp.StatusCode, resp.Header.Get("WWW-Authenticate"), answer.Result
}

// callTool calls one tool over MCP with a bearer and answers its result, failing unless MCP let the call in.
func callTool(t *testing.T, bearer, name string, args map[string]any) map[string]any {
	t.Helper()
	status, challenge, result := mcpAs(t, bearer, "tools/call", map[string]any{"name": name, "arguments": args})
	if status != http.StatusOK || result == nil {
		t.Fatalf("%s over MCP: %d %q", name, status, challenge)
	}
	return result
}

// bearerAt sends an assistant's token straight to an API path, as an assistant that skipped MCP would.
func bearerAt(t *testing.T, bearer, method, path string) int {
	t.Helper()
	req, _ := http.NewRequest(method, rotorApp(t)+httpapi.APIPrefix+path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := newAddress(t).client(false).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// keepAssistantsOn switches assistants back on once the test ends, whatever it did.
func keepAssistantsOn(t *testing.T, h *harness) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := h.super.Exec(context.Background(), `UPDATE assistant_setting SET connections_on = true`); err != nil {
			t.Errorf("switch assistants back on: %v", err)
		}
	})
}

// countRows counts behind the policies, as the superuser.
func countRows(t *testing.T, h *harness, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
