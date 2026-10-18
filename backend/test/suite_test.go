//go:build integration && suite

package test

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// The tests against the real Exciter (#8), run by make test-suite on the suite's network; each name starts with
// Suite, which is how the target picks them. Later issues add theirs here.
const (
	envExciter         = "ROTOR_TEST_EXCITER_URL"
	envExciterInternal = "ROTOR_TEST_EXCITER_INTERNAL_URL"
)

// exciter reaches the suite's Exciter at the address a browser opens, through the suite's network.
func exciter(t *testing.T) (base string, client *http.Client) {
	t.Helper()
	base = os.Getenv(envExciter)
	if base == "" || os.Getenv(envExciterInternal) == "" {
		t.Skipf("%s and %s are not set; run make suite-up, then make test-suite", envExciter, envExciterInternal)
	}
	return base, oidc.HTTPClient(map[string]string{base: os.Getenv(envExciterInternal)})
}

func getJSON(t *testing.T, client *http.Client, address string) map[string]any {
	t.Helper()
	resp, err := client.Get(address)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&out); resp.StatusCode != http.StatusOK || err != nil {
		t.Fatalf("GET %s = %d (%v)", address, resp.StatusCode, err)
	}
	return out
}

// Exciter is discovered the way docs/api-contract-m1.md says Rotor discovers a tool, from its address alone.
func TestSuiteExciterIsDiscoveredFromItsAddress(t *testing.T) {
	base, client := exciter(t)
	meta := getJSON(t, client, base+"/api/v1/meta")
	if caps, _ := meta["capabilities"].([]any); meta["product"] != "exciter" || !slices.Contains(caps, any("mcp@1")) {
		t.Fatalf("Exciter's meta reads %v", meta)
	}
	resource := getJSON(t, client, base+"/.well-known/oauth-protected-resource/api/v1/mcp")
	servers, _ := resource["authorization_servers"].([]any)
	if resource["resource"] != base+"/api/v1/mcp" || len(servers) == 0 {
		t.Fatalf("the protected resource reads %v", resource)
	}
	issuer, _ := servers[0].(string)
	server := getJSON(t, client, issuer+"/.well-known/oauth-authorization-server")
	methods, _ := server["code_challenge_methods_supported"].([]any)
	if server["issuer"] != issuer || !slices.Contains(methods, any("S256")) || server["registration_endpoint"] == nil {
		t.Errorf("the authorization server reads %v", server)
	}
}
