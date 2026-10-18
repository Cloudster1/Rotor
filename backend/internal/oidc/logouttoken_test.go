package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/cloudster1/rotor/backend/internal/config"
)

const testClient = "rotor"

// fakeProvider serves discovery and a key set, and signs tokens with the key it publishes.
type fakeProvider struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.server.URL, "jwks_uri": p.server.URL + "/certs",
			"authorization_endpoint": p.server.URL + "/auth", "token_endpoint": p.server.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/certs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeProvider) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// logoutClaimsAt is a logout token as Keycloak sends one, issued at now.
func (p *fakeProvider) logoutClaimsAt(now time.Time) map[string]any {
	return map[string]any{
		"iss": p.server.URL, "aud": testClient, "sub": "subject-1", "sid": "session-1", "jti": "jti-1",
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
		"events": map[string]any{backchannelLogoutEvent: map[string]any{}},
	}
}

func TestALogoutTokenIsCheckedAsTheSpecificationSays(t *testing.T) {
	p := newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()

	logout, err := s.VerifyLogoutToken(t.Context(), p.sign(t, p.logoutClaimsAt(now)), now)
	if err != nil {
		t.Fatalf("a good token was refused: %v", err)
	}
	if logout.SID != "session-1" || logout.Subject != "subject-1" || logout.JTI != "jti-1" || logout.Issuer != p.server.URL {
		t.Errorf("the token names %+v", logout)
	}

	for name, change := range map[string]func(map[string]any){
		"for another client":       func(c map[string]any) { c["aud"] = "exciter" },
		"from another issuer":      func(c map[string]any) { c["iss"] = "https://elsewhere.test" },
		"without the logout event": func(c map[string]any) { c["events"] = map[string]any{"other": map[string]any{}} },
		"carrying a nonce":         func(c map[string]any) { c["nonce"] = "n" },
		"naming nobody":            func(c map[string]any) { delete(c, "sid"); delete(c, "sub") },
		"without a jti":            func(c map[string]any) { delete(c, "jti") },
		"issued long ago":          func(c map[string]any) { c["iat"] = now.Add(-time.Hour).Unix(); c["exp"] = 0 },
		"issued in the future":     func(c map[string]any) { c["iat"] = now.Add(time.Hour).Unix() },
		"expired":                  func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() },
	} {
		claims := p.logoutClaimsAt(now)
		change(claims)
		if _, err := s.VerifyLogoutToken(t.Context(), p.sign(t, claims), now); !errors.Is(err, ErrBadLogoutToken) {
			t.Errorf("a token %s = %v, want it refused", name, err)
		}
	}
}

func TestALogoutTokenSignedByAnotherKeyIsRefused(t *testing.T) {
	p, impostor := newFakeProvider(t), newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()
	claims := p.logoutClaimsAt(now)
	if _, err := s.VerifyLogoutToken(t.Context(), impostor.sign(t, claims), now); !errors.Is(err, ErrBadLogoutToken) {
		t.Errorf("a token signed by another key = %v, want it refused", err)
	}
}

func TestALogoutTokenNamingOnlyThePersonIsAccepted(t *testing.T) {
	p := newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()
	claims := p.logoutClaimsAt(now)
	delete(claims, "sid")
	delete(claims, "exp")
	logout, err := s.VerifyLogoutToken(t.Context(), p.sign(t, claims), now)
	if err != nil || logout.SID != "" || logout.Subject != "subject-1" || !logout.Expires.After(now) {
		t.Fatalf("a token naming only the person = %+v, %v", logout, err)
	}
}

func TestWithoutAnIssuerNothingIsAsked(t *testing.T) {
	s := NewService(nil, nil, config.OIDC{})
	if _, err := s.Start(t.Context(), "/"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Start = %v, want ErrNotConfigured", err)
	}
	if _, err := s.VerifyLogoutToken(t.Context(), "x", time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("VerifyLogoutToken = %v, want ErrNotConfigured", err)
	}
}
