//go:build integration && suite

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// follow walks redirects from address as a browser does, signing in at Keycloak as username if it asks.
func (b *browser) follow(t *testing.T, address, username string) *url.URL {
	t.Helper()
	at, _ := url.Parse(address)
	for range 10 {
		resp, body := b.get(at.String())
		if match := loginForm.FindStringSubmatch(body); match != nil && resp.StatusCode == http.StatusOK {
			form := url.Values{"username": {username}, "password": {username + " password"}, "credentialId": {""}}
			req, _ := http.NewRequest(http.MethodPost, strings.ReplaceAll(match[1], "&amp;", "&"), strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, body = b.do(req)
		}
		if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
			return at
		}
		next, err := at.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("a redirect to %q: %v", resp.Header.Get("Location"), err)
		}
		at = next
	}
	t.Fatalf("more than 10 redirects from %s", address)
	return nil
}

// A person connects to the real Exciter, consenting there as themselves, and Rotor keeps the connection.
func TestSuiteAPersonConnectsToTheRealExciter(t *testing.T) {
	base, _ := exciter(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": base})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	toolID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+toolID, nil) })

	bob := newBrowser(t)
	bob.signIn("bob")
	bob.client.Transport = oidc.HTTPClient(map[string]string{
		os.Getenv(envKeycloakPublic): os.Getenv(envKeycloak), os.Getenv(envApp): apiURL(t), base: os.Getenv(envExciterInternal),
	}).Transport
	status, answer = bob.api(http.MethodPost, "/tools/"+toolID+"/connection", nil)
	authorize, _ := answer["authorizeURL"].(string)
	if status != http.StatusOK || authorize == "" {
		t.Fatalf("starting the connection = %d %v", status, answer)
	}
	consent := bob.follow(t, authorize, "bob")
	request := consent.Query().Get("request")
	if !strings.HasSuffix(consent.Path, "/oauth/consent") || request == "" {
		t.Fatalf("Exciter's authorize landed on %s, not its consent page", consent)
	}
	// Bob signs in to Exciter through the same realm, which knows him already, then allows Rotor there.
	bob.follow(t, base+"/api/v1/auth/oidc/start?next=/", "bob")
	body, _ := json.Marshal(map[string]any{"allow": true})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/oauth/requests/"+request, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, text := bob.do(req)
	var decided struct {
		RedirectTo string `json:"redirectTo"`
	}
	if err := json.Unmarshal([]byte(text), &decided); resp.StatusCode != http.StatusOK || err != nil || decided.RedirectTo == "" {
		t.Fatalf("allowing Rotor at Exciter = %d %s", resp.StatusCode, text)
	}
	if landed := bob.landing(t, decided.RedirectTo); landed != "/connections?connected="+toolID {
		t.Fatalf("the callback landed on %q", landed)
	}
	if status, _ := bob.api(http.MethodDelete, "/tools/"+toolID+"/connection", nil); status != http.StatusNoContent {
		t.Errorf("disconnecting from the real Exciter = %d", status)
	}
}
