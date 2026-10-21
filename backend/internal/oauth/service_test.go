package oauth

import (
	"strings"
	"testing"
)

func TestTheMetadataNamesEverythingFromTheAppsAddress(t *testing.T) {
	s := NewService(nil, "https://rotor.example/", "/api/v1")
	if s.Resource() != "https://rotor.example/api/v1/mcp" || s.Issuer() != "https://rotor.example" ||
		s.ResourceMetadata() != "https://rotor.example/.well-known/oauth-protected-resource/api/v1/mcp" ||
		s.ConsentPage() != "https://rotor.example/oauth/consent" {
		t.Errorf("the service is named %s, %s, %s", s.Issuer(), s.Resource(), s.ResourceMetadata())
	}
	m := s.Metadata()
	if m.TokenEndpoint != "https://rotor.example/api/v1/oauth/token" || m.AuthorizationEndpoint != "https://rotor.example/api/v1/oauth/authorize" ||
		m.RegistrationEndpoint != "https://rotor.example/api/v1/oauth/register" || len(m.CodeChallengeMethodsSupported) != 1 ||
		m.CodeChallengeMethodsSupported[0] != ChallengeS256 || !m.AuthorizationResponseIssParameterSupported {
		t.Errorf("the server reads %+v", m)
	}
	if r := s.ProtectedResource(); r.Resource != s.Resource() || len(r.AuthorizationServers) != 1 || r.AuthorizationServers[0] != s.Issuer() {
		t.Errorf("the resource reads %+v", r)
	}
	for _, asked := range []string{"https://rotor.example/api/v1/mcp", "https://rotor.example/api/v1/mcp/"} {
		if !s.isResource(asked) {
			t.Errorf("%s is not the resource", asked)
		}
	}
	if s.isResource("https://rotor.example/api/v1") || s.isResource("") {
		t.Error("another address is taken for the resource")
	}
}

func TestEverySecretIsPrefixedRandomAndKeptAsItsDigestAlone(t *testing.T) {
	seen := map[string]bool{}
	for _, prefix := range []string{AccessTokenPrefix, RefreshTokenPrefix, codePrefix, clientSecretPrefix} {
		value, digest, err := secret(prefix)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(value, prefix) || len(value) < len(prefix)+40 || len(digest) != 32 || seen[value] {
			t.Errorf("%s made %q with a digest of %d bytes", prefix, value, len(digest))
		}
		seen[value] = true
		if strings.Contains(string(digest), value) {
			t.Error("the digest holds the secret")
		}
	}
	if !IsAccessToken(AccessTokenPrefix+"x") || IsAccessToken(RefreshTokenPrefix+"x") || IsAccessToken("") {
		t.Error("an access token is told apart wrongly")
	}
}
