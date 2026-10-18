package oidc

import "testing"

func TestAnAddressIsRewrittenByTheOriginItHas(t *testing.T) {
	rewrites := ParseRewrites("http://localhost:8306=http://tool-stub:8080, http://localhost:8307=http://suite-exciter-web:80")
	for in, want := range map[string]string{
		"http://localhost:8306/t/a/api/v1/mcp": "http://tool-stub:8080/t/a/api/v1/mcp",
		"http://localhost:8307":                "http://suite-exciter-web:80",
		"http://localhost:83060/x":             "http://localhost:83060/x",
		"https://exciter.example.com/api":      "https://exciter.example.com/api",
	} {
		if got := Rewrite(rewrites, in); got != want {
			t.Errorf("Rewrite(%q) = %q, want %q", in, got, want)
		}
	}
}
