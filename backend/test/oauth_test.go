//go:build integration

package test

// Adapted from Exciter at 3b340ca, backend/test/oauth_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// An assistant given Rotor's address alone connects as the official Go SDK does it: it meets the 401, reads the
// metadata, registers, sends its person to consent, trades the code, and starts and cancels a run as them (#47).
func TestAnAssistantConnectsThroughOAuthAndRunsAWorkflow(t *testing.T) {
	h := newHarness(t)
	product := "assist-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, bobID, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, greeter(uniqueName("assisted"), product))
	from := newAddress(t)
	forgetClients(t, h, from)
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs: []string{callback}, ClientName: "Integration assistant", GrantTypes: []string{"authorization_code", "refresh_token"}}},
		RedirectURL: callback,
		AuthorizationCodeFetcher: func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			back, err := consent(t, from, bob, args.URL, map[string]any{"allow": true})
			if err != nil {
				return nil, err
			}
			q := back.Query()
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
		Client: from.client(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "rotor-oauth", Version: "1"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: resource(t), HTTPClient: from.client(false), OAuthHandler: handler}, nil)
	if err != nil {
		t.Fatalf("the assistant does not connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if strings.Contains(tool.Name, "decide") || strings.Contains(tool.Name, "approve") || strings.Contains(tool.Name, "decision") {
			t.Errorf("the assistant is offered %s", tool.Name)
		}
	}
	who, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	if err != nil || who.IsError {
		t.Fatalf("whoami: %v %+v", err, who)
	}
	me := who.StructuredContent.(map[string]any)
	if assistant, _ := me["assistant"].(map[string]any); assistant["client"] != "Integration assistant" || assistant["readOnly"] != false ||
		me["user"].(map[string]any)["id"] != bobID.String() {
		t.Errorf("the assistant acts as %v", me)
	}
	started, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "start_run", Arguments: map[string]any{
		"workflowID": workflowID, "inputs": map[string]any{"greeting": "Hello"}, "idempotencyKey": "assisted-" + uuid.NewString()}})
	if err != nil || started.IsError {
		t.Fatalf("start_run: %v %+v", err, started)
	}
	runID := started.StructuredContent.(map[string]any)["id"].(string)
	cancelled, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "cancel_run", Arguments: map[string]any{"runID": runID}})
	if err != nil || cancelled.IsError {
		t.Errorf("cancel_run: %v %+v", err, cancelled)
	}
	// Ended for good, so no later test's worker takes it up.
	waitRun(t, bob, runID, "cancelled")
	waitTornDown(t, h, uuid.MustParse(runID))

	// The grant and what was done through it are on record, the latter naming the assistant.
	// Counted by the assistant this test registered, so an earlier run on the same stack is no difference.
	if n := countRows(t, h, `SELECT count(*) FROM audit_log a JOIN oauth_client c ON c.id = a.data->>'clientId'
		WHERE a.action = 'oauth.granted' AND a.actor_user_id = $1 AND c.registered_from = $2`, bobID, from.ip); n != 1 {
		t.Errorf("%d audit entries for the grant, want 1", n)
	}
	if n := countRows(t, h, `SELECT count(*) FROM audit_log WHERE action = 'run.started' AND target_id = $1
		AND via_client = 'Integration assistant' AND via_grant IS NOT NULL`, runID); n != 1 {
		t.Errorf("%d run.started entries name the assistant, want 1", n)
	}
}

// What an assistant reads before it is let in: the resource at both addresses, the server, and a 401 that points
// there.
func TestTheMetadataSaysHowToConnect(t *testing.T) {
	anyone := newAddress(t).client(false)
	app := rotorApp(t)
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/api/v1/mcp"} {
		got := getJSON(t, anyone, app+path)
		if got["resource"] != resource(t) || got["authorization_servers"].([]any)[0] != app {
			t.Errorf("%s reads %v", path, got)
		}
	}
	server := getJSON(t, anyone, app+"/.well-known/oauth-authorization-server")
	if server["issuer"] != app || server["token_endpoint"] != app+"/api/v1/oauth/token" || server["registration_endpoint"] != app+"/api/v1/oauth/register" ||
		server["authorization_response_iss_parameter_supported"] != true || server["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Errorf("the server reads %v", server)
	}
	status, challenge, _ := mcpAs(t, "", "tools/list", nil)
	if status != http.StatusUnauthorized || !strings.Contains(challenge, `resource_metadata="`+app+`/.well-known/oauth-protected-resource/api/v1/mcp"`) ||
		strings.Contains(challenge, "invalid_token") {
		t.Errorf("an anonymous call is told %d %q", status, challenge)
	}
	if _, challenge, _ := mcpAs(t, "rotor_oat_nothing", "tools/list", nil); !strings.Contains(challenge, `error="invalid_token"`) {
		t.Errorf("a refused token is told %q", challenge)
	}
}

// Each refusal is the error the RFCs name, with a sentence; a code or refresh token used twice ends its grant, and
// an access token is for MCP alone.
func TestOAuthRefusesWhatTheRFCsRefuse(t *testing.T) {
	h := newHarness(t)
	person := signedIn(t, "bob")
	c, status, _ := register(t, h, newAddress(t), nil)
	if status != http.StatusCreated || c.id == "" || c.secret != "" {
		t.Fatalf("an assistant keeping no secret registers: %d %q", status, c.secret)
	}

	t.Run("an assistant or redirect Rotor does not know is never sent back to", func(t *testing.T) {
		for change, reason := range map[string]string{"client_id": "invalid_client", "redirect_uri": "invalid_redirect"} {
			to := sentTo(t, c.from, c.authorizeURL(t, map[string]string{change: "https://elsewhere.example/" + change}))
			if to.Path != "/oauth/consent" || to.Query().Get("error") != reason {
				t.Errorf("an unknown %s goes to %s", change, to)
			}
		}
	})

	t.Run("anything else goes back with the error, the state and the issuer", func(t *testing.T) {
		for name, tc := range map[string]struct {
			change map[string]string
			code   string
		}{
			"another response type": {map[string]string{"response_type": "token"}, "unsupported_response_type"},
			"no challenge":          {map[string]string{"code_challenge": ""}, "invalid_request"},
			"the plain method":      {map[string]string{"code_challenge_method": "plain"}, "invalid_request"},
			"no state":              {map[string]string{"state": ""}, "invalid_request"},
			"another resource":      {map[string]string{"resource": "https://elsewhere.example/mcp"}, "invalid_target"},
			"another scope":         {map[string]string{"scope": "admin"}, "invalid_scope"},
		} {
			to := sentTo(t, c.from, c.authorizeURL(t, tc.change))
			q := to.Query()
			if to.Host != "127.0.0.1:9" || q.Get("error") != tc.code || q.Get("error_description") == "" || q.Get("iss") != rotorApp(t) {
				t.Errorf("%s goes back to %s", name, to)
			}
		}
	})

	t.Run("a code is traded once, with its verifier and redirect", func(t *testing.T) {
		code := c.allow(t, person, map[string]any{"allow": true})
		good := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {c.verifier}}
		for name, change := range map[string][2]string{
			"a wrong verifier":    {"code_verifier", "a-verifier-that-is-long-enough-but-not-the-one-it-was-made-from"},
			"another redirect":    {"redirect_uri", "http://127.0.0.1:9/other"},
			"another resource":    {"resource", "https://elsewhere.example/mcp"},
			"an unknown grant":    {"grant_type", "password"},
			"another client's id": {"client_id", "nobody"},
		} {
			form := url.Values{}
			for k, v := range good {
				form[k] = v
			}
			form.Set(change[0], change[1])
			if status, got := c.form(t, "/oauth/token", form); status/100 != 4 || got["error"] == nil || got["error_description"] == nil {
				t.Errorf("%s answers %d %v", name, status, got)
			}
		}
		tokens := c.exchange(t, code)
		access := tokens["access_token"].(string)
		if tokens["token_type"] != "Bearer" || tokens["expires_in"] != float64(3600) || tokens["scope"] != "mcp" {
			t.Errorf("the tokens read %v", tokens)
		}
		callTool(t, access, "whoami", map[string]any{})
		if status, got := c.form(t, "/oauth/token", good); status != http.StatusBadRequest || got["error"] != "invalid_grant" {
			t.Errorf("a code traded twice answers %d %v", status, got)
		}
		if status, _, _ := mcpAs(t, access, "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("the grant a code traded twice made still works: %d", status)
		}
	})

	t.Run("a refresh token turns over, only narrows, and one used twice ends the grant", func(t *testing.T) {
		first := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))
		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first["refresh_token"].(string)}, "scope": {"mcp:read"}}
		status, second := c.form(t, "/oauth/token", refresh)
		if status != http.StatusOK || second["refresh_token"] == first["refresh_token"] || second["scope"] != "mcp:read" {
			t.Fatalf("a narrowing refresh answers %d %v", status, second)
		}
		widen := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {second["refresh_token"].(string)}, "scope": {"mcp"}}
		if status, got := c.form(t, "/oauth/token", widen); status != http.StatusBadRequest || got["error"] != "invalid_scope" {
			t.Errorf("a widening refresh answers %d %v", status, got)
		}
		if status, got := c.form(t, "/oauth/token", refresh); status != http.StatusBadRequest || got["error"] != "invalid_grant" {
			t.Errorf("a refresh token used twice answers %d %v", status, got)
		}
		if status, _, _ := mcpAs(t, second["access_token"].(string), "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("the grant a stolen refresh token reached still works: %d", status)
		}
	})

	t.Run("an access token is for MCP alone, and revoking one ends it", func(t *testing.T) {
		access := c.exchange(t, c.allow(t, person, map[string]any{"allow": true}))["access_token"].(string)
		for _, path := range []string{"/auth/me", "/runs", "/oauth/grants"} {
			if status := bearerAt(t, access, http.MethodGet, path); status != http.StatusUnauthorized {
				t.Errorf("GET %s with an access token answers %d", path, status)
			}
		}
		if status, _ := c.form(t, "/oauth/revoke", url.Values{"token": {access}}); status != http.StatusOK {
			t.Errorf("a revocation answers %d", status)
		}
		if status, _, _ := mcpAs(t, access, "tools/list", nil); status != http.StatusUnauthorized {
			t.Errorf("a revoked access token still works: %d", status)
		}
		if status, _ := c.form(t, "/oauth/revoke", url.Values{"token": {"rotor_oat_unknown"}}); status != http.StatusOK {
			t.Errorf("revoking a token nobody knows answers %d", status)
		}
	})

	t.Run("an assistant with a secret is held to it, and an address registers ten an hour", func(t *testing.T) {
		secret, status, made := register(t, h, newAddress(t), map[string]any{"token_endpoint_auth_method": "client_secret_basic"})
		if status != http.StatusCreated || !strings.HasPrefix(secret.secret, "rotor_ocs_") {
			t.Fatalf("a confidential assistant registers: %d %v", status, made)
		}
		code := secret.allow(t, person, map[string]any{"allow": true})
		wrong := *secret
		wrong.secret = "rotor_ocs_wrong"
		if status, got := wrong.form(t, "/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callback},
			"code_verifier": {secret.verifier}}); status != http.StatusUnauthorized || got["error"] != "invalid_client" {
			t.Errorf("a wrong secret answers %d %v", status, got)
		}
		secret.exchange(t, code)
		from := newAddress(t)
		for i := range 10 {
			if _, status, _ := register(t, h, from, nil); status != http.StatusCreated {
				t.Fatalf("registration %d answers %d", i+1, status)
			}
		}
		if _, status, got := register(t, h, from, nil); status != http.StatusTooManyRequests || got["error"] != "temporarily_unavailable" {
			t.Errorf("the eleventh registration answers %d %v", status, got)
		}
		if _, status, got := register(t, h, newAddress(t), map[string]any{"redirect_uris": []string{"http://elsewhere.example/cb"}}); status != http.StatusBadRequest ||
			got["error"] != "invalid_redirect_uri" {
			t.Errorf("a plain http redirect registers: %d %v", status, got)
		}
	})
}

// getJSON reads a JSON object, failing unless it answers 200.
func getJSON(t *testing.T, client *http.Client, address string) map[string]any {
	t.Helper()
	resp, err := client.Get(address)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&out); resp.StatusCode != http.StatusOK || err != nil {
		t.Fatalf("GET %s = %d (%v)", address, resp.StatusCode, err)
	}
	return out
}
