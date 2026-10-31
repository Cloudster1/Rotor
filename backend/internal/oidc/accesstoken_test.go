package oidc

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// accessClaimsAt is a client-credentials token of dynamo-connect as Keycloak issues one at now.
func (p *fakeProvider) accessClaimsAt(now time.Time) map[string]any {
	return map[string]any{
		"iss": p.server.URL, "aud": []string{testClient, "account"}, "sub": "service-account-1", "azp": "dynamo-connect",
		"typ": "Bearer", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix(), "groups": []string{"/" + AdminGroup},
	}
}

func TestAServiceAccountsAccessTokenIsLetIn(t *testing.T) {
	p := newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()
	identity, err := s.VerifyAccessToken(t.Context(), p.sign(t, p.accessClaimsAt(now)), now)
	if err != nil {
		t.Fatalf("a good token was refused: %v", err)
	}
	if identity.Subject != "service-account-1" || identity.Issuer != p.server.URL || identity.Name != "dynamo-connect" ||
		identity.Email != "dynamo-connect@"+serviceAccountDomain || len(identity.Groups) != 1 || identity.Groups[0] != AdminGroup {
		t.Errorf("the token names %+v", identity)
	}
}

func TestAnAccessTokenIsRefusedSayingWhatIsMissing(t *testing.T) {
	p := newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()
	for name, c := range map[string]struct {
		change func(map[string]any)
		want   error
		says   string
	}{
		"for another client":     {func(c map[string]any) { c["aud"] = "exciter" }, ErrTokenAudience, "audience does not hold rotor"},
		"without an audience":    {func(c map[string]any) { delete(c, "aud") }, ErrTokenAudience, "audience mapper for rotor"},
		"from another issuer":    {func(c map[string]any) { c["iss"] = "https://elsewhere.test" }, ErrTokenIssuer, "https://elsewhere.test"},
		"expired":                {func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() }, ErrTokenExpired, "expired"},
		"expiring now":           {func(c map[string]any) { c["exp"] = now.Unix() }, ErrTokenExpired, "expired"},
		"without an expiry":      {func(c map[string]any) { delete(c, "exp") }, ErrTokenExpired, "no expiry"},
		"that is an id token":    {func(c map[string]any) { c["typ"] = "ID" }, ErrTokenNotAccess, "not an access token"},
		"without a type":         {func(c map[string]any) { delete(c, "typ") }, ErrTokenNotAccess, "not an access token"},
		"naming nobody":          {func(c map[string]any) { delete(c, "sub") }, ErrTokenNoAccount, "names no account"},
		"of no client":           {func(c map[string]any) { delete(c, "azp") }, ErrTokenNoAccount, "names no client"},
		"without groups":         {func(c map[string]any) { delete(c, "groups") }, ErrTokenNotAdmin, AdminGroup},
		"of another group":       {func(c map[string]any) { c["groups"] = []string{"engineering"} }, ErrTokenNotAdmin, AdminGroup},
		"of an unverified email": {func(c map[string]any) { c["email"] = "eve@suite.test"; c["email_verified"] = false }, ErrEmailUnverified, "not verified"},
	} {
		claims := p.accessClaimsAt(now)
		c.change(claims)
		_, err := s.VerifyAccessToken(t.Context(), p.sign(t, claims), now)
		var refusal *Refusal
		if !errors.Is(err, c.want) || !errors.As(err, &refusal) || !strings.Contains(refusal.Message, c.says) {
			t.Errorf("a token %s = %v, want %v saying %q", name, err, c.want, c.says)
		}
	}
}

func TestAnAccessTokenNotSignedByTheRealmIsRefused(t *testing.T) {
	p, impostor := newFakeProvider(t), newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()
	claims := p.accessClaimsAt(now)
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
	signed := strings.Split(p.sign(t, claims), ".")
	claims["sub"] = "someone-else"
	forged, _ := json.Marshal(claims)
	for name, raw := range map[string]string{
		"signed by another key": impostor.sign(t, claims),
		"signed by nobody":      unsigned,
		"changed after signing": signed[0] + "." + base64.RawURLEncoding.EncodeToString(forged) + "." + signed[2],
		"that is no token":      "not-a-jwt",
	} {
		if _, err := s.VerifyAccessToken(t.Context(), raw, now); !errors.Is(err, ErrTokenUnverified) {
			t.Errorf("a token %s = %v, want it refused as unverified", name, err)
		}
	}
}

func TestAPersonsAccessTokenNamesThePerson(t *testing.T) {
	p := newFakeProvider(t)
	s := NewService(nil, nil, config.OIDC{Issuer: p.server.URL, ClientID: testClient, ClientSecret: "x"})
	now := time.Now()
	claims := p.accessClaimsAt(now)
	claims["email"], claims["email_verified"], claims["name"] = "Alice@Suite.test", true, "Alice Admin"
	identity, err := s.VerifyAccessToken(t.Context(), p.sign(t, claims), now)
	if err != nil || identity.Email != "alice@suite.test" || identity.Name != "Alice Admin" {
		t.Fatalf("a person's token = %+v, %v", identity, err)
	}
}

func TestWithoutAnIssuerNoAccessTokenIsChecked(t *testing.T) {
	s := NewService(nil, nil, config.OIDC{})
	if _, err := s.VerifyAccessToken(t.Context(), "x", time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("VerifyAccessToken = %v, want ErrNotConfigured", err)
	}
}
