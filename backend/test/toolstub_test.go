//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// The deployed test double answers as a tool at the address an administrator types, and its control API is out of
// reach of that address: what the published port serves is the tools alone.
func TestTheToolStubIsAToolAtItsPublicAddress(t *testing.T) {
	stub := newToolStub(t, nil)

	resp, raw := stub.get("/.well-known/oauth-authorization-server")
	var server map[string]any
	if err := json.Unmarshal(raw, &server); resp.StatusCode != http.StatusOK || err != nil || server["issuer"] != stub.BaseURL {
		t.Fatalf("the metadata = %d %s, want the issuer %s", resp.StatusCode, raw, stub.BaseURL)
	}
	if resp, raw := stub.get("/api/v1/meta"); resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"mcp@1"`) {
		t.Errorf("meta = %d %s", resp.StatusCode, raw)
	}

	public, err := stub.client.Get(os.Getenv(envToolStub) + "/control/tenants/" + stub.name + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	_ = public.Body.Close()
	if public.StatusCode != http.StatusNotFound {
		t.Errorf("the control API answered %d through the published address, want 404", public.StatusCode)
	}
	if stats := stub.Stats(); stats["clients"] != float64(0) {
		t.Errorf("a fresh tenant counts %v", stats)
	}
}

// A tenant made without OAuth is the tool an administrator must be refused: it has no authorization server.
func TestATenantWithoutOAuthHasNoAuthorizationServer(t *testing.T) {
	stub := newToolStub(t, map[string]any{"oauth": false})
	if resp, _ := stub.get("/.well-known/oauth-protected-resource/api/v1/mcp"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the protected resource metadata answered %d, want 404", resp.StatusCode)
	}
	if resp, _ := stub.get("/api/v1/meta"); resp.StatusCode != http.StatusOK {
		t.Errorf("meta answered %d", resp.StatusCode)
	}
}
