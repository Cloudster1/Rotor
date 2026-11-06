package oidc

import (
	"errors"
	"testing"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

func TestAProviderWithoutAnIssuerOrAClientIsRefusedNamingEach(t *testing.T) {
	for name, c := range map[string]struct {
		in     ProviderInput
		fields []string
	}{
		"neither":        {ProviderInput{}, []string{"issuer", "clientId"}},
		"no client":      {ProviderInput{Issuer: "https://id.test/realms/dynamo"}, []string{"clientId"}},
		"not an address": {ProviderInput{Issuer: "id.test/realms/dynamo", ClientID: "rotor"}, []string{"issuer"}},
		"another scheme": {ProviderInput{Issuer: "ftp://id.test", ClientID: "rotor"}, []string{"issuer"}},
	} {
		var refused *ProviderError
		if err := checkProvider(c.in); !errors.As(err, &refused) || len(refused.Fields) != len(c.fields) {
			t.Errorf("%s = %v, want %v named", name, err, c.fields)
			continue
		}
		for _, field := range c.fields {
			if refused.Fields[field] == "" {
				t.Errorf("%s does not say what to fix in %s", name, field)
			}
		}
	}
	if err := checkProvider(ProviderInput{Issuer: "http://localhost:8302/realms/dynamo", ClientID: "rotor"}); err != nil {
		t.Errorf("a provider that could work = %v", err)
	}
}

// The secret saved is a new one, the one OpenBao holds, or the environment's while it still fits, which moves to
// OpenBao once the issuer or the client changes; with none of them the save is refused.
func TestASavedProviderKeepsTheSecretItHas(t *testing.T) {
	env := config.OIDC{Issuer: "https://id.test/realms/dynamo", ClientID: "rotor", ClientSecret: "from-env"}
	s := NewService(nil, nil, env)
	stored := time.Now()
	same := ProviderInput{Issuer: env.Issuer, ClientID: env.ClientID}
	other := ProviderInput{Issuer: env.Issuer, ClientID: "rotor-elsewhere"}
	seeded := &Provider{Issuer: env.Issuer, ClientID: env.ClientID}
	for name, c := range map[string]struct {
		in     ProviderInput
		held   *Provider
		secret string
		kept   bool
	}{
		"a new one":                        {ProviderInput{Issuer: env.Issuer, ClientID: "x", ClientSecret: "new"}, seeded, "new", false},
		"the one in OpenBao":               {other, &Provider{Issuer: env.Issuer, ClientID: "x", secretSetAt: &stored}, "", true},
		"the environment's, still fitting": {same, seeded, "", false},
		"the environment's, moved":         {other, seeded, "from-env", false},
	} {
		secret, setAt, _, err := s.secretFor(c.in, c.held)
		if err != nil || secret != c.secret || (setAt != nil) != c.kept {
			t.Errorf("%s = %q %v %v", name, secret, setAt, err)
		}
	}
	var refused *ProviderError
	if _, _, _, err := s.secretFor(other, nil); !errors.As(err, &refused) || refused.Fields["clientSecret"] == "" {
		t.Errorf("no secret anywhere = %v, want the secret asked for", err)
	}
}

func TestTheEnvironmentsSecretFitsOnlyItsOwnIssuerAndClient(t *testing.T) {
	s := NewService(nil, nil, config.OIDC{Issuer: "https://id.test/realms/dynamo", ClientID: "rotor", ClientSecret: "x"})
	if !s.envSecretFits("https://id.test/realms/dynamo", "rotor") || s.envSecretFits("https://id.test/realms/other", "rotor") ||
		s.envSecretFits("https://id.test/realms/dynamo", "other") {
		t.Error("the environment's secret fits a provider it did not come with")
	}
	if NewService(nil, nil, config.OIDC{Issuer: "https://id.test", ClientID: "rotor"}).envSecretFits("https://id.test", "rotor") {
		t.Error("an environment without a secret fits")
	}
}
