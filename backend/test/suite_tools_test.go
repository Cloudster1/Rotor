//go:build integration && suite

package test

import (
	"net/http"
	"testing"
)

// The real Exciter is added as the test double is: discovered from its address, with Rotor registered as its client.
func TestSuiteAnAdministratorAddsTheRealExciter(t *testing.T) {
	base, _ := exciter(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": base})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	tool := toolOf(t, answer)
	id, _ := tool["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })
	if tool["name"] != "exciter" || tool["issuer"] != base || tool["resource"] != base+"/api/v1/mcp" || tool["status"] != "ready" {
		t.Errorf("the real Exciter was added as %v", tool)
	}
}
