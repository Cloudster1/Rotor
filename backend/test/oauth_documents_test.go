//go:build integration

package test

// Adapted from Exciter at 3b340ca, backend/test/oauth_test.go's TestAClientIsKnownByItsMetadataDocument (#114).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	rotorauth "github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/netguard"
	"github.com/cloudster1/rotor/backend/internal/oauth"
)

// documentAPI is an api in this process on the stack's database, named by its own address as ROTOR_APP_URL names a
// real one, which reads clients' documents with fetch; the documents a test names cannot be served over https here.
type documentAPI struct {
	URL     string
	cookie  *http.Cookie
	browser *http.Client
}

func newDocumentAPI(t *testing.T, h *harness, fetch oauth.Fetcher) *documentAPI {
	t.Helper()
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(server.Close)
	sessions := rotorauth.NewService(h.cluster, h.cfg.Auth.SessionTTL)
	handler = (&httpapi.Server{DB: h.cluster, Log: slog.New(slog.DiscardHandler), Sessions: sessions, CookieName: h.cfg.Auth.SessionCookie,
		AppBaseURL: server.URL, OAuth: oauth.NewService(h.cluster, server.URL, httpapi.APIPrefix, fetch)}).Routes(nil)

	// A person signed in to it, as the consent page needs.
	person := h.addPerson(t, "member")
	var token string
	if err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		token, err = sessions.Open(ctx, tx, rotorauth.SessionInput{UserID: person})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return &documentAPI{URL: server.URL, cookie: &http.Cookie{Name: h.cfg.Auth.SessionCookie, Value: token},
		browser: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// documents stands in for the web a metadata document is fetched from: address to body, kept for a minute.
func documents(served map[string]string) oauth.Fetcher {
	return func(_ context.Context, address string) ([]byte, time.Duration, error) {
		body, ok := served[address]
		if !ok {
			return nil, 0, fmt.Errorf("the document answered 404 Not Found")
		}
		return []byte(body), time.Minute, nil
	}
}

func forgetDocuments(t *testing.T, h *harness, ids ...string) {
	t.Cleanup(func() {
		if _, err := h.super.Exec(context.Background(), `DELETE FROM oauth_client WHERE id = ANY ($1)`, ids); err != nil {
			t.Errorf("forget the test's documents: %v", err)
		}
	})
}

// sentTo is where the browser goes from an authorization of the client.
func (a *documentAPI) sentTo(t *testing.T, client string) *url.URL {
	t.Helper()
	q := url.Values{"response_type": {"code"}, "client_id": {client}, "redirect_uri": {callback}, "state": {"s"},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(oauth2.GenerateVerifier())}, "code_challenge_method": {"S256"},
		"resource": {a.URL + httpapi.APIPrefix + "/mcp"}, "scope": {"mcp"}}
	resp, err := a.browser.Get(a.URL + httpapi.APIPrefix + "/oauth/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	to, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("the authorization answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	return to
}

// api calls the in-process api as the signed-in person.
func (a *documentAPI) api(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = strings.NewReader(string(encoded))
	} else {
		reader = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, a.URL+httpapi.APIPrefix+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(a.cookie)
	resp, err := a.browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// An assistant known by its document connects as the official Go SDK does it, with nothing registered (#114).
func TestAnAssistantIsKnownByItsMetadataDocument(t *testing.T) {
	const (
		good     = "https://assistant.example/oauth/client.json"
		elsewise = "https://assistant.example/oauth/other.json"
		secretly = "https://assistant.example/oauth/secret.json"
		missing  = "https://assistant.example/oauth/missing.json"
	)
	doc := func(id, extra string) string {
		return `{"client_id":"` + id + `","client_name":"Document assistant","redirect_uris":["` + callback + `"]` + extra + `}`
	}
	h := newHarness(t)
	forgetDocuments(t, h, good, elsewise, secretly, missing)
	a := newDocumentAPI(t, h, documents(map[string]string{
		good: doc(good, ""), elsewise: doc(good, ""), secretly: doc(secretly, `,"token_endpoint_auth_method":"client_secret_basic"`),
	}))

	var meta map[string]any
	resp, err := http.Get(a.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if meta["client_id_metadata_document_supported"] != true {
		t.Errorf("the server does not offer documents: %v", meta)
	}

	var kind any
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: good},
		RedirectURL:                    callback,
		AuthorizationCodeFetcher: func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			resp, err := a.browser.Get(args.URL)
			if err != nil {
				return nil, err
			}
			resp.Body.Close()
			page, err := url.Parse(resp.Header.Get("Location"))
			if err != nil || page.Query().Get("request") == "" {
				return nil, fmt.Errorf("the authorization went to %q", resp.Header.Get("Location"))
			}
			path := "/oauth/requests/" + page.Query().Get("request")
			_, read := a.api(t, http.MethodGet, path, nil)
			if request, _ := read["request"].(map[string]any); request != nil {
				kind = request["client"].(map[string]any)["kind"]
			}
			status, decided := a.api(t, http.MethodPost, path, map[string]any{"allow": true})
			if status != http.StatusOK {
				return nil, fmt.Errorf("the consent answered %d %v", status, decided)
			}
			back, _ := url.Parse(decided["redirectTo"].(string))
			q := back.Query()
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "rotor-documents", Version: "1"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: a.URL + httpapi.APIPrefix + "/mcp", OAuthHandler: handler}, nil)
	if err != nil {
		t.Fatalf("the assistant does not connect by its document: %v", err)
	}
	defer session.Close()
	who, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	if err != nil || who.IsError {
		t.Fatalf("whoami: %v %+v", err, who)
	}
	if assistant, _ := who.StructuredContent.(map[string]any)["assistant"].(map[string]any); assistant["client"] != "Document assistant" {
		t.Errorf("the assistant acts as %v", who.StructuredContent)
	}
	if kind != string(oauth.ClientMetadata) {
		t.Errorf("the consent page says the assistant is %v", kind)
	}
	if n := countRows(t, h, `SELECT count(*) FROM oauth_client WHERE id = $1 AND kind = 'metadata' AND registered_from IS NULL
		AND fresh_until > now()`, good); n != 1 {
		t.Errorf("the document is kept %d times, want once, fresh", n)
	}

	// A document that names another client, asks for a secret or is not there is never sent back to.
	for _, id := range []string{elsewise, secretly, missing} {
		if to := a.sentTo(t, id); to.Path != oauth.ConsentPath || to.Query().Get("error") != oauth.PageInvalidClient {
			t.Errorf("%s goes to %s", id, to)
		}
	}

	// The database holds a document to keeping no secret and to being fetched, and a registration to its address.
	for name, query := range map[string]string{
		"a document with a secret": `INSERT INTO oauth_client (id, kind, name, redirect_uris, auth_method, secret_hash, grant_types, fresh_until)
			VALUES ('https://x.example/a.json', 'metadata', 'x', ARRAY['https://x.example/cb'], 'client_secret_basic', sha256('s'),
			ARRAY['authorization_code'], now())`,
		"a document never fetched": `INSERT INTO oauth_client (id, kind, name, redirect_uris, grant_types)
			VALUES ('https://x.example/b.json', 'metadata', 'x', ARRAY['https://x.example/cb'], ARRAY['authorization_code'])`,
		"a registration from nowhere": `INSERT INTO oauth_client (id, name, redirect_uris, grant_types)
			VALUES ('` + uuid.NewString() + `', 'x', ARRAY['https://x.example/cb'], ARRAY['authorization_code'])`,
	} {
		if _, err := h.super.Exec(context.Background(), query); err == nil {
			t.Errorf("%s is kept", name)
		}
	}
}

// The real fetcher never reaches Rotor's own network, whatever a document's address names.
func TestADocumentInsideRotorsNetworkIsNeverFetched(t *testing.T) {
	h := newHarness(t)
	guarded := oauth.HTTPFetcher(netguard.Client(config.OAuthMetadataFetchTimeout, netguard.ParseAllow("")))
	a := newDocumentAPI(t, h, guarded)
	for _, id := range []string{"https://127.0.0.1/client.json", "https://169.254.169.254/latest/meta-data.json",
		"https://" + strings.TrimPrefix(a.URL, "http://") + "/client.json"} {
		forgetDocuments(t, h, id)
		if to := a.sentTo(t, id); to.Query().Get("error") != oauth.PageInvalidClient {
			t.Errorf("%s goes to %s", id, to)
		}
		if n := countRows(t, h, `SELECT count(*) FROM oauth_client WHERE id = $1`, id); n != 0 {
			t.Errorf("%s was kept", id)
		}
	}
}
