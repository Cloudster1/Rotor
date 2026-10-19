//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A workflow that writes after reading, with no approval between, saves with a warning that says so (#40).
func TestAWriteAfterAReadWithoutAnApprovalIsWarnedAboutAndStillSaves(t *testing.T) {
	newHarness(t)
	product := "warned-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)

	status, body := bob.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": asker(product, false)})
	warnings, _ := body["warnings"].([]any)
	if status != http.StatusOK || body["valid"] != true || len(warnings) != 1 ||
		warnings[0].(map[string]any)["code"] != "untrusted_then_write" || warnings[0].(map[string]any)["line"] == float64(0) {
		t.Fatalf("validating a write after a read = %d %v", status, body)
	}
	if status, body := bob.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": asker(product, true)}); status != http.StatusOK ||
		len(body["warnings"].([]any)) != 0 {
		t.Errorf("validating a write behind an approval = %d %v", status, body)
	}
	createWorkflow(t, bob, asker(product, false))
}

// An agent's answer names only its tools' hosts: a link elsewhere is removed, said in an event, and the run's page
// knows which hosts it may link to (#40).
func TestAnAgentsLinksPointOnlyAtItsTools(t *testing.T) {
	h := newHarness(t)
	key, product := "llm-key-"+uuid.NewString(), "links-"+uuid.NewString()[:8]
	own := os.Getenv(envToolStub) + "/t/" + product + "/r/1"
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": []map[string]any{
		{"content": "Filed at " + own + " and mirrored at https://evil.example/x?d=secret"}}})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	run := startRun(t, bob, createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string))), map[string]any{}, http.StatusCreated)
	runID := run["id"].(string)
	done := waitRun(t, bob, runID, "succeeded", "failed")
	output, _ := json.Marshal(done["output"])
	if done["status"] != "succeeded" || strings.Contains(string(output), "evil.example") || strings.Contains(string(output), "secret") ||
		!strings.Contains(string(output), own) || !strings.Contains(string(output), "[link removed]") {
		t.Fatalf("the run ended %v with output %s", done["status"], output)
	}
	public, _ := url.Parse(os.Getenv(envToolStub))
	if _, page := bob.api(http.MethodGet, "/runs/"+runID, nil); !slices.Contains(anyStrings(page["linkHosts"]), public.Hostname()) {
		t.Errorf("the run's page may link to %v, not its tool at %s", page["linkHosts"], public.Hostname())
	}
	removed := false
	for _, e := range h.events(t, uuid.MustParse(runID)) {
		if e.Type == "link.removed" && e.Node == "ask" && slices.Contains(anyStrings(e.Payload["hosts"]), "evil.example") {
			removed = true
		}
	}
	if !removed {
		t.Errorf("no link.removed event names evil.example: %+v", h.events(t, uuid.MustParse(runID)))
	}
}

// anyStrings is a JSON array of strings as Go strings; anything else is none.
func anyStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
