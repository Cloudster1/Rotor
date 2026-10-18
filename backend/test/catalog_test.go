//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTheCatalogListsWhatTheConnectionSeesAndIsCached(t *testing.T) {
	product := uniqueName("catalog")
	stub := newToolStub(t, map[string]any{"product": product})
	toolID := addTool(t, stub)
	bob := newBrowser(t)
	bob.signIn("bob")
	if landed := bob.landing(t, bob.consent(t, toolID, "bob", "allow")); !strings.Contains(landed, "connected=") {
		t.Fatalf("connecting landed on %q", landed)
	}

	connected := stub.Stats()
	status, answer := bob.api(http.MethodGet, "/tools/"+toolID+"/catalog", nil)
	listed, _ := answer["tools"].([]any)
	if status != http.StatusOK || len(listed) < 10 || answer["build"] == "" {
		t.Fatalf("the catalog = %d %v", status, answer)
	}
	readOnly := 0
	for _, item := range listed {
		if item.(map[string]any)["readOnly"] == true {
			readOnly++
		}
	}
	if readOnly == 0 || readOnly == len(listed) {
		t.Errorf("%d of %d tools read only; want the hints carried over", readOnly, len(listed))
	}
	before := stub.Stats()
	if status, _ := bob.api(http.MethodGet, "/tools/"+toolID+"/catalog", nil); status != http.StatusOK {
		t.Fatalf("the catalog again = %d", status)
	}
	if after := stub.Stats(); after["mcpCalls"] != before["mcpCalls"] || after["refreshes"] != before["refreshes"] {
		t.Errorf("the second read reached the tool: %v then %v; want the cache", before, after)
	}
	if before["liveAccessTokens"] != connected["liveAccessTokens"] {
		t.Errorf("the catalog's token outlived the read: %v live before, %v after", connected["liveAccessTokens"], before["liveAccessTokens"])
	}

	carol := newBrowser(t)
	carol.signIn("carol")
	if status, answer := carol.api(http.MethodGet, "/tools/"+toolID+"/catalog", nil); status != http.StatusConflict {
		t.Errorf("an unconnected person's catalog = %d %v, want 409", status, answer)
	}

	// The saver's catalog decides what a binding may allow: a read binding cannot allow a writing tool.
	source := fmt.Sprintf(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: %s}
tools:
  t:
    tool: %s
    scopes: ["mcp:read"]
    allowTools: [update_requirement]
limits: {maxSteps: 3, timeout: 1m}
entry: a
nodes:
  - {id: a, type: tool, tool: t, call: update_requirement}
`, uniqueName("bound"), product)
	_, answer = bob.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": source})
	if !strings.Contains(fmt.Sprint(answer["problems"]), "writes_on_read_binding") {
		t.Errorf("a write through a read binding validated as %v", answer)
	}
}
