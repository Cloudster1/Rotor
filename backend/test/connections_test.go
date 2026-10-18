//go:build integration

package test

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/tools"
)

var (
	consentForm    = regexp.MustCompile(`<form method="post" action="([^"]+)"`)
	consentRequest = regexp.MustCompile(`name="request" value="([^"]+)"`)
)

// addTool adds the tenant as alice and removes it after the test; it answers the tool's id.
func addTool(t *testing.T, stub *toolStub) string {
	t.Helper()
	alice := newBrowser(t)
	alice.signIn("alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusCreated {
		t.Fatalf("adding the tool = %d %v", status, answer)
	}
	id, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })
	return id
}

// reachStub lets the browser follow the tool's public address, as a person's browser would.
func (b *browser) reachStub(t *testing.T) {
	t.Helper()
	b.client.Transport = oidc.HTTPClient(map[string]string{
		os.Getenv(envKeycloakPublic): os.Getenv(envKeycloak),
		os.Getenv(envApp):            apiURL(t),
		os.Getenv(envToolStub):       os.Getenv(envToolStubInternal),
	}).Transport
}

// consent starts a connection, answers the tool's consent page as username, and returns where the tool sends the
// browser back: Rotor's callback, not yet followed.
func (b *browser) consent(t *testing.T, toolID, username, decision string) string {
	t.Helper()
	return b.consentFrom(t, toolID, username, decision, "")
}

// consentFrom is consent started from a page of Rotor's the browser returns to, next.
func (b *browser) consentFrom(t *testing.T, toolID, username, decision, next string) string {
	t.Helper()
	b.reachStub(t)
	path := "/tools/" + toolID + "/connection"
	if next != "" {
		path += "?next=" + url.QueryEscape(next)
	}
	status, answer := b.api(http.MethodPost, path, nil)
	authorize, _ := answer["authorizeURL"].(string)
	if status != http.StatusOK || authorize == "" {
		t.Fatalf("starting the connection = %d %v", status, answer)
	}
	page := authorize
	resp, body := b.get(page)
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		next, _ := url.Parse(page)
		location, _ := next.Parse(resp.Header.Get("Location"))
		page = location.String()
		resp, body = b.get(page)
	}
	form, request := consentForm.FindStringSubmatch(body), consentRequest.FindStringSubmatch(body)
	if resp.StatusCode != http.StatusOK || form == nil || request == nil {
		t.Fatalf("the consent page = %d %.400s", resp.StatusCode, body)
	}
	// Against the public address, as a browser resolves it: the request itself went to the stack's network.
	base, _ := url.Parse(page)
	action, err := base.Parse(html.UnescapeString(form[1]))
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"request": {html.UnescapeString(request[1])}, "username": {username}, "decision": {decision}}
	req, _ := http.NewRequest(http.MethodPost, action.String(), strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body = b.do(req)
	if (resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther) || !strings.HasPrefix(resp.Header.Get("Location"), b.app+"/api/v1/connections/callback") {
		t.Fatalf("after consenting = %d to %q %.300s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	return resp.Header.Get("Location")
}

// landing follows the callback and returns the Connections page address it lands on.
func (b *browser) landing(t *testing.T, callback string) string {
	t.Helper()
	resp, body := b.get(callback)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the callback = %d %s", resp.StatusCode, body)
	}
	return resp.Header.Get("Location")
}

func TestAPersonConnectsTheirAccountAndOnlyOpenBaoHoldsTheToken(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	toolID := addTool(t, stub)
	bob := newBrowser(t)
	bob.signIn("bob")
	_, me := bob.me()
	bobID, _ := me["user"].(map[string]any)["id"].(string)

	if landed := bob.landing(t, bob.consent(t, toolID, "bob", "allow")); landed != "/connections?connected="+toolID {
		t.Fatalf("landed on %q", landed)
	}
	status, answer := bob.api(http.MethodGet, "/connections", nil)
	list, _ := answer["connections"].([]any)
	if status != http.StatusOK || len(list) != 1 {
		t.Fatalf("bob's connections = %d %v", status, answer)
	}
	connection := list[0].(map[string]any)
	if connection["status"] != "connected" || !strings.Contains(stringOf(connection["scopes"]), "mcp") {
		t.Errorf("the connection = %v", connection)
	}

	secret, err := rootBao(t).Logical().Read("rotor/data/connections/" + bobID + "/" + toolID)
	if err != nil || secret == nil {
		t.Fatalf("OpenBao holds no connection for bob: %v", err)
	}
	data, _ := secret.Data["data"].(map[string]any)
	refresh, _ := data["refresh_token"].(string)
	if refresh == "" {
		t.Fatalf("the connection in OpenBao = %v", data)
	}
	if where := h.find(t, refresh); where != "" {
		t.Errorf("bob's refresh token is in the database, in %s", where)
	}

	carol := newBrowser(t)
	carol.signIn("carol")
	if _, answer := carol.api(http.MethodGet, "/connections", nil); len(answer["connections"].([]any)) != 0 {
		t.Errorf("carol sees bob's connection: %v", answer)
	}
	if status, _ := carol.api(http.MethodDelete, "/tools/"+toolID+"/connection", nil); status != http.StatusNotFound {
		t.Errorf("carol ending a connection she does not have = %d, want 404", status)
	}

	if status, _ := bob.api(http.MethodDelete, "/tools/"+toolID+"/connection", nil); status != http.StatusNoContent {
		t.Fatalf("disconnecting = %d", status)
	}
	stats := stub.Stats()
	if stats["revocations"] != float64(1) || stats["liveGrants"] != float64(0) {
		t.Errorf("after disconnecting the tool counts %v; want one revocation and no grant", stats)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/connections/" + bobID + "/" + toolID); err != nil || secret != nil {
		t.Errorf("the refresh token outlived the connection: %v %v", secret, err)
	}
}

func TestAnAnswerIsRefusedFromAnotherSessionTwiceOrFromAnotherIssuer(t *testing.T) {
	stub := newToolStub(t, nil)
	toolID := addTool(t, stub)
	bob, carol := newBrowser(t), newBrowser(t)
	bob.signIn("bob")
	carol.signIn("carol")

	// Carol's answer, followed in bob's browser, is not bob's to keep.
	carols := carol.consent(t, toolID, "carol", "allow")
	if landed := bob.landing(t, carols); !strings.Contains(landed, "error=mismatch") {
		t.Errorf("another session's answer landed on %q", landed)
	}
	if landed := carol.landing(t, carols); !strings.Contains(landed, "error=expired") {
		t.Errorf("an answer used once already landed on %q", landed)
	}

	bobs := bob.consent(t, toolID, "bob", "allow")
	forged, _ := url.Parse(bobs)
	q := forged.Query()
	q.Set("iss", "https://evil.test")
	forged.RawQuery = q.Encode()
	if landed := bob.landing(t, forged.String()); !strings.Contains(landed, "error=mismatch") {
		t.Errorf("an answer naming another issuer landed on %q", landed)
	}

	if landed := bob.landing(t, bob.consent(t, toolID, "bob", "deny")); !strings.Contains(landed, "error=denied") {
		t.Errorf("a refusal at the tool landed on %q", landed)
	}
	if _, answer := bob.api(http.MethodGet, "/connections", nil); len(answer["connections"].([]any)) != 0 {
		t.Errorf("a refused answer left a connection: %v", answer)
	}
}

func TestRemovingAToolEndsEveryConnectionToIt(t *testing.T) {
	stub := newToolStub(t, nil)
	alice := newBrowser(t)
	alice.signIn("alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusCreated {
		t.Fatalf("adding the tool = %d %v", status, answer)
	}
	toolID, _ := toolOf(t, answer)["id"].(string)
	bob := newBrowser(t)
	bob.signIn("bob")
	if landed := bob.landing(t, bob.consent(t, toolID, "bob", "allow")); !strings.Contains(landed, "connected=") {
		t.Fatalf("connecting landed on %q", landed)
	}

	if status, _ := alice.api(http.MethodDelete, "/tools/"+toolID, nil); status != http.StatusNoContent {
		t.Fatalf("removing the tool = %d", status)
	}
	if stats := stub.Stats(); stats["revocations"] != float64(1) || stats["liveGrants"] != float64(0) {
		t.Errorf("removing the tool left the tool counting %v", stats)
	}
	if _, answer := bob.api(http.MethodGet, "/connections", nil); len(answer["connections"].([]any)) != 0 {
		t.Errorf("bob still has a connection to a removed tool: %v", answer)
	}
}

// Switching a person off ends their connections too, revoked at the tool, as the api wires it.
func TestSwitchingAPersonOffEndsTheirConnections(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	toolID := addTool(t, stub)
	dave := newBrowser(t)
	dave.signIn("dave")
	if landed := dave.landing(t, dave.consent(t, toolID, "dave", "allow")); !strings.Contains(landed, "connected=") {
		t.Fatalf("connecting landed on %q", landed)
	}
	var daveID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = 'dave@suite.test'`).Scan(&daveID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, daveID)
	})

	secrets, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	reach := oidc.HTTPClient(map[string]string{os.Getenv(envToolStub): os.Getenv(envToolStubInternal)})
	registry := tools.NewService(h.cluster, secrets, reach, os.Getenv(envApp)+config.ConnectionCallbackPath, true)
	people := connections.NewService(h.cluster, secrets, registry, reach, os.Getenv(envApp)+config.ConnectionCallbackPath, slog.New(slog.DiscardHandler))
	sessions := auth.NewService(h.cluster, config.DefaultSessionTTL)
	sessions.OnSwitchOff = people.EndPersonConnections
	if err := sessions.SwitchOff(context.Background(), uuid.Nil, daveID); err != nil {
		t.Fatal(err)
	}

	if stats := stub.Stats(); stats["revocations"] != float64(1) || stats["liveGrants"] != float64(0) {
		t.Errorf("switching dave off left the tool counting %v", stats)
	}
	var left int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM connection WHERE user_id = $1`, daveID).Scan(&left); err != nil || left != 0 {
		t.Errorf("dave keeps %d connections after being switched off (%v)", left, err)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/connections/" + daveID.String() + "/" + toolID); err != nil || secret != nil {
		t.Errorf("dave's refresh token outlived him being switched off: %v %v", secret, err)
	}
}
