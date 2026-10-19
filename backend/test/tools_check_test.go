//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A tool updated to a release without what Rotor needs is kept and says so, a hook is refused at a tool that sends
// no webhooks, and checking again after the next update clears it (#43).
func TestCheckingAToolAgainReadsWhatItOffersNow(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	id := addTool(t, stub)
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	check := "/tools/" + id + "/check"

	if status, _ := bob.api(http.MethodPost, check, nil); status != http.StatusForbidden {
		t.Errorf("a member checking a tool = %d, want 403", status)
	}
	stub.ListCapabilities("webhooks@1")
	status, answer := alice.api(http.MethodPost, check, nil)
	if status != http.StatusOK || !strings.Contains(stringOf(toolOf(t, answer)["missing"]), "mcp@1") {
		t.Fatalf("checking a tool without mcp@1 = %d %v", status, answer)
	}
	if status, answer := bob.api(http.MethodGet, "/tools/"+id, nil); status != http.StatusOK ||
		!strings.Contains(stringOf(toolOf(t, answer)["missing"]), "Update Exciter") {
		t.Errorf("a member reads the tool as %d %v, want what to update", status, answer)
	}

	stub.ListCapabilities("mcp@1")
	if status, answer := alice.api(http.MethodPost, check, nil); status != http.StatusOK ||
		!strings.Contains(stringOf(toolOf(t, answer)["missing"]), "webhooks@1") {
		t.Fatalf("checking a tool without webhooks = %d %v", status, answer)
	}
	status, answer = alice.api(http.MethodPut, "/tools/"+id+"/hook", map[string]any{"secret": "exciter_whs_abc"})
	if e, _ := answer["error"].(map[string]any); status != http.StatusUnprocessableEntity || e["code"] != "tool_lacks" {
		t.Errorf("a hook at a tool without webhooks = %d %v", status, answer)
	}

	stub.ListCapabilities("mcp@1", "webhooks@1")
	status, answer = alice.api(http.MethodPost, check, nil)
	if missing, ok := toolOf(t, answer)["missing"].([]any); status != http.StatusOK || !ok || len(missing) != 0 {
		t.Errorf("checking the tool once updated = %d %v, want nothing missing", status, answer)
	}
	var checks int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'tool.checked' AND target_id = $1`,
		id).Scan(&checks); err != nil || checks != 3 {
		t.Errorf("tool.checked entries = %d %v, want 3", checks, err)
	}
}
