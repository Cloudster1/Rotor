//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
)

// The suite's client for Rotor in the development realm (#168), and its secret there.
const (
	connectClient = "dynamo-connect"
	connectSecret = "dynamo-connect-dev-secret"
	connectEmail  = connectClient + "@service-account.invalid"
	// shortLifespan is how long a token of the expiring test client lives, in seconds.
	shortLifespan = "1"
)

// clientToken takes an access token of a client of the realm with its credentials, as dynamo-connect does.
func clientToken(t *testing.T, client, secret string) string {
	t.Helper()
	resp, err := http.PostForm(os.Getenv(envKeycloak)+"/realms/"+realm+"/protocol/openid-connect/token", url.Values{
		"grant_type": {"client_credentials"}, "client_id": {client}, "client_secret": {secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("%s's token = %d, %v", client, resp.StatusCode, err)
	}
	return tok.AccessToken
}

// withToken calls Rotor's api with a bearer token and no cookie, and answers the status and the error's message.
func (b *browser) withToken(token, method, path string, body any) (int, map[string]any) {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, b.app+"/api/v1"+path, reader)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, text := b.do(req)
	out := map[string]any{}
	_ = json.Unmarshal([]byte(text), &out)
	return resp.StatusCode, out
}

func messageOf(answer map[string]any) string {
	e, _ := answer["error"].(map[string]any)
	msg, _ := e["message"].(string)
	return msg
}

func TestDynamoConnectAddsAToolAndHandsRotorItsHookSecret(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, map[string]any{"product": "exciter"})
	token := clientToken(t, connectClient, connectSecret)
	connect := newBrowser(t)

	status, answer := connect.withToken(token, http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusCreated {
		t.Fatalf("dynamo-connect adding the tool = %d %v", status, answer)
	}
	id, _ := toolOf(t, answer)["id"].(string)
	alice := signedIn(t, "alice")
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })

	// It finds the tool by its base URL, and learns where the tool's webhook points.
	status, answer = connect.withToken(token, http.MethodGet, "/tools", nil)
	var hookURL string
	listed, _ := answer["tools"].([]any)
	for _, each := range listed {
		if tool, _ := each.(map[string]any); tool["baseURL"] == stub.BaseURL {
			hookURL, _ = tool["hookURL"].(string)
		}
	}
	if status != http.StatusOK || !strings.HasSuffix(hookURL, "/api/v1/hooks/"+id) {
		t.Fatalf("dynamo-connect listing the tools = %d, hook URL %q", status, hookURL)
	}

	secret := "exciter_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := connect.withToken(token, http.MethodPut, "/tools/"+id+"/hook", map[string]any{"secret": secret}); status != http.StatusOK ||
		hook["secretSet"] != true || hook["url"] != hookURL {
		t.Fatalf("dynamo-connect setting the hook secret = %d %v", status, hook)
	}
	if status, hook := connect.withToken(token, http.MethodGet, "/tools/"+id+"/hook", nil); status != http.StatusOK || hook["secretSet"] != true {
		t.Errorf("dynamo-connect reading the hook = %d %v", status, hook)
	}
	// The secret it handed over is the one Rotor now checks deliveries with.
	if status, _ := deliver(t, id, "Exciter", secret, envelope(uuid.NewString(), "ping", `{}`), time.Now(), "1", nil); status != http.StatusOK {
		t.Errorf("a ping signed with the secret = %d, want 200", status)
	}

	// Both changes are the service account's own in the audit log.
	var acted int
	if err := h.super.QueryRow(context.Background(), `
		SELECT count(*) FROM audit_log a JOIN app_user u ON u.id = a.actor_user_id
		WHERE u.email = $1 AND a.target_id = $2 AND a.action IN ($3, $4)`,
		connectEmail, id, audit.ActionToolAdded, audit.ActionHookSecretChanged).Scan(&acted); err != nil {
		t.Fatal(err)
	}
	if acted != 2 {
		t.Errorf("%d of the two changes are audited as dynamo-connect", acted)
	}

	// Everywhere else the token is refused, in a sentence naming where it is taken.
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/workflows"}, {http.MethodPost, "/workflows"}, {http.MethodGet, "/auth/me"},
		{http.MethodDelete, "/tools/" + id}, {http.MethodDelete, "/tools/" + id + "/hook"}, {http.MethodGet, "/audit"},
	} {
		status, answer := connect.withToken(token, call.method, call.path, map[string]any{})
		if status != http.StatusUnauthorized || !strings.Contains(messageOf(answer), "PUT /api/v1/tools/{toolID}/hook") {
			t.Errorf("dynamo-connect at %s %s = %d %q, want 401 naming the routes that take its token", call.method, call.path, status, messageOf(answer))
		}
	}
	if status, _ := alice.api(http.MethodGet, "/tools/"+id+"/hook", nil); status != http.StatusOK {
		t.Errorf("the tool is gone after the refusals: %d", status)
	}
}

// testClient adds a client to the realm for one test, a service account whose tokens name rotor, and removes it after.
func testClient(t *testing.T, kc *keycloakAdmin, attributes map[string]string) (string, string) {
	t.Helper()
	name, secret := "rotor-test-"+uuid.NewString()[:8], uuid.NewString()
	kc.send(http.MethodPost, "/clients", map[string]any{
		"clientId": name, "secret": secret, "enabled": true, "publicClient": false, "serviceAccountsEnabled": true,
		"standardFlowEnabled": false, "directAccessGrantsEnabled": false, "defaultClientScopes": []string{"basic"}, "attributes": attributes,
		"protocolMappers": []map[string]any{
			{"name": "rotor audience", "protocol": "openid-connect", "protocolMapper": "oidc-audience-mapper",
				"config": map[string]string{"included.client.audience": "rotor", "access.token.claim": "true", "id.token.claim": "false"}},
			{"name": "groups", "protocol": "openid-connect", "protocolMapper": "oidc-group-membership-mapper",
				"config": map[string]string{"full.path": "false", "claim.name": "groups", "access.token.claim": "true"}},
		},
	}, nil)
	var found []struct {
		ID string `json:"id"`
	}
	kc.call(http.MethodGet, "/clients?clientId="+name, &found)
	if len(found) != 1 {
		t.Fatalf("the realm holds %d clients called %s", len(found), name)
	}
	t.Cleanup(func() { kc.call(http.MethodDelete, "/clients/"+found[0].ID, nil) })
	return name, secret
}

func TestATokenWithoutTheGroupTheAudienceOrTheRealmIsRefused(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloakAdmin(t)
	b := newBrowser(t)
	body := map[string]any{"baseURL": newToolStub(t, nil).BaseURL}

	outsider, outsiderSecret := testClient(t, kc, nil)
	expiring, expiringSecret := testClient(t, kc, map[string]string{"access.token.lifespan": shortLifespan})
	expired := clientToken(t, expiring, expiringSecret)
	good := clientToken(t, connectClient, connectSecret)
	parts := strings.Split(good, ".")
	tampered := parts[0] + "." + parts[1] + "." + strings.Map(func(r rune) rune {
		if r == 'A' {
			return 'B'
		}
		return 'A'
	}, parts[2])
	// The token lives a second; waiting twice that lets it lapse.
	time.Sleep(2 * time.Second)

	for name, c := range map[string]struct {
		token  string
		status int
		says   string
	}{
		"without the group":             {clientToken(t, outsider, outsiderSecret), http.StatusForbidden, "rotor-administrators"},
		"without rotor in its audience": {clientToken(t, "rotor-control-plane", "rotor-control-plane-dev-secret"), http.StatusUnauthorized, "audience does not hold rotor"},
		"expired":                       {expired, http.StatusUnauthorized, "expired"},
		"of another realm":              {kc.token, http.StatusUnauthorized, "not signed with the keys"},
		"whose signature is changed":    {tampered, http.StatusUnauthorized, "not signed with the keys"},
	} {
		status, answer := b.withToken(c.token, http.MethodPost, "/tools", body)
		if status != c.status || !strings.Contains(messageOf(answer), c.says) {
			t.Errorf("a token %s = %d %q, want %d saying %q", name, status, messageOf(answer), c.status, c.says)
		}
	}

	// A refused account is never made a person of Rotor.
	var made int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM app_user WHERE email IN ($1, $2)`,
		outsider+"@service-account.invalid", expiring+"@service-account.invalid").Scan(&made); err != nil {
		t.Fatal(err)
	}
	if made != 0 {
		t.Errorf("%d refused service accounts were made people of Rotor", made)
	}
}
