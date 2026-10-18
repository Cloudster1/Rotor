//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// Where the suite finds Keycloak and the web origin: the public addresses a browser uses, and the
// addresses on the stack's network the requests are sent to instead.
const (
	envKeycloak       = "ROTOR_TEST_KEYCLOAK_URL"
	envKeycloakPublic = "ROTOR_TEST_KEYCLOAK_PUBLIC_URL"
	envApp            = "ROTOR_TEST_APP_URL"
	realm             = "dynamo"
)

var loginForm = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)

// browser signs in as a person would, cookies and redirects included, reaching the published addresses
// through the stack's network.
type browser struct {
	t      *testing.T
	client *http.Client
	app    string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	for _, key := range []string{envKeycloak, envKeycloakPublic, envApp} {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set; run the suite with make test-integration", key)
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := oidc.HTTPClient(map[string]string{
		os.Getenv(envKeycloakPublic): os.Getenv(envKeycloak),
		os.Getenv(envApp):            apiURL(t),
	}).Transport
	return &browser{t: t, app: os.Getenv(envApp), client: &http.Client{
		Jar: jar, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (b *browser) do(req *http.Request) (*http.Response, string) {
	b.t.Helper()
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func (b *browser) get(url string) (*http.Response, string) {
	b.t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	return b.do(req)
}

// signIn walks the whole flow and returns where the callback sends the browser.
func (b *browser) signIn(username string) string {
	b.t.Helper()
	resp, body := b.get(b.app + "/api/v1/auth/oidc/start?next=/runs")
	if resp.StatusCode != http.StatusFound {
		b.t.Fatalf("start = %d %s", resp.StatusCode, body)
	}
	resp, body = b.get(resp.Header.Get("Location"))
	match := loginForm.FindStringSubmatch(body)
	if resp.StatusCode != http.StatusOK || match == nil {
		b.t.Fatalf("Keycloak's login page = %d, no form in %.300s", resp.StatusCode, body)
	}
	form := url.Values{"username": {username}, "password": {username + " password"}, "credentialId": {""}}
	req, err := http.NewRequest(http.MethodPost, html.UnescapeString(match[1]), strings.NewReader(form.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body = b.do(req)
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), b.app) {
		b.t.Fatalf("Keycloak after the password = %d to %q %.300s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	resp, body = b.get(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound {
		b.t.Fatalf("callback = %d %s", resp.StatusCode, body)
	}
	return resp.Header.Get("Location")
}

// me is who the api says is signed in, with the status it answered.
func (b *browser) me() (int, map[string]any) {
	b.t.Helper()
	resp, body := b.get(b.app + "/api/v1/auth/me")
	var out map[string]any
	_ = json.Unmarshal([]byte(body), &out)
	return resp.StatusCode, out
}

// keycloakAdmin calls Keycloak's admin API as the stack's bootstrap administrator.
type keycloakAdmin struct {
	t     *testing.T
	base  string
	token string
}

func newKeycloakAdmin(t *testing.T) *keycloakAdmin {
	t.Helper()
	base := os.Getenv(envKeycloak)
	resp, err := http.PostForm(base+"/realms/master/protocol/openid-connect/token", url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {"admin"}, "password": {"admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("Keycloak's admin token = %d, %v", resp.StatusCode, err)
	}
	return &keycloakAdmin{t: t, base: base, token: tok.AccessToken}
}

func (k *keycloakAdmin) call(method, path string, out any) {
	k.t.Helper()
	req, err := http.NewRequest(method, k.base+"/admin/realms/"+realm+path, nil)
	if err != nil {
		k.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+k.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		k.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		k.t.Fatalf("%s %s = %d %s", method, path, resp.StatusCode, body)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			k.t.Fatal(err)
		}
	}
}

// id finds a realm user or group by its exact name.
func (k *keycloakAdmin) id(kind, name string) string {
	k.t.Helper()
	var found []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		User string `json:"username"`
	}
	query := "username=" + url.QueryEscape(name) + "&exact=true"
	if kind == "groups" {
		query = "search=" + url.QueryEscape(name) + "&exact=true"
	}
	k.call(http.MethodGet, fmt.Sprintf("/%s?%s", kind, query), &found)
	for _, f := range found {
		if f.Name == name || f.User == name {
			return f.ID
		}
	}
	k.t.Fatalf("the realm has no %s called %s", kind, name)
	return ""
}
