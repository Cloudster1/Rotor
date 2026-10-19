package oidc

import "testing"

func TestTheRealmIsReadFromAKeycloakIssuerOnly(t *testing.T) {
	for issuer, want := range map[string]string{
		"https://sso.example.com/realms/dynamo":      "dynamo",
		"https://sso.example.com/realms/dynamo/":     "dynamo",
		"https://sso.example.com/auth/realms/dynamo": "dynamo",
		"https://login.example.com":                  "",
		"https://sso.example.com/realms/a/b":         "",
	} {
		if got := realmOf(issuer); got != want {
			t.Errorf("realmOf(%q) = %q, want %q", issuer, got, want)
		}
	}
}
