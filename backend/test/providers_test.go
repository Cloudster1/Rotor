//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// addProvider adds an OpenAI-compatible provider at the tenant's stub model as alice, and removes it after the test.
func addProvider(t *testing.T, alice *browser, stub *toolStub, key string) map[string]any {
	t.Helper()
	name := "stub-" + uuid.NewString()[:8]
	status, answer := alice.api(http.MethodPost, "/providers", map[string]any{"name": name, "kind": "openai_compatible",
		"baseURL": stub.BaseURL + "/llm/v1", "models": []string{"stub-model"}, "defaultModel": "stub-model", "enabled": true, "apiKey": key})
	if status != http.StatusCreated {
		t.Fatalf("adding a provider = %d %v", status, answer)
	}
	provider, _ := answer["provider"].(map[string]any)
	id, _ := provider["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/providers/"+id, nil) })
	return provider
}

func TestAnAdministratorKeepsAProviderWhoseKeyOnlyOpenBaoHolds(t *testing.T) {
	h := newHarness(t)
	key := "llm-key-" + uuid.NewString()
	stub := newToolStub(t, map[string]any{"llmKey": key})
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	id, _ := provider["id"].(string)
	if provider["hasKey"] != true || provider["kind"] != "openai_compatible" {
		t.Errorf("the provider = %v", provider)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/providers/" + id + "/key"); err != nil || secret == nil {
		t.Fatalf("OpenBao holds no key for the provider: %v", err)
	}
	if where := h.find(t, key); where != "" {
		t.Errorf("the provider's key is in the database, in %s", where)
	}

	if status, answer := alice.api(http.MethodPost, "/providers/"+id+"/test", nil); status != http.StatusOK || answer["ok"] != true ||
		!strings.Contains(stringOf(answer["models"]), "stub-model") {
		t.Errorf("testing the provider = %d %v", status, answer)
	}
	change := map[string]any{"name": provider["name"], "kind": "openai_compatible", "baseURL": stub.BaseURL + "/llm/v1",
		"models": []string{"stub-model"}, "enabled": true, "apiKey": "a-wrong-key"}
	if status, answer := alice.api(http.MethodPut, "/providers/"+id, change); status != http.StatusOK {
		t.Fatalf("changing the key = %d %v", status, answer)
	}
	if _, answer := alice.api(http.MethodPost, "/providers/"+id+"/test", nil); answer["ok"] != false || !strings.Contains(stringOf(answer["message"]), "refused the key") {
		t.Errorf("testing with a wrong key = %v", answer)
	}

	bob := newBrowser(t)
	bob.signIn("bob")
	if status, _ := bob.api(http.MethodGet, "/providers", nil); status != http.StatusForbidden {
		t.Errorf("a member listing the providers = %d, want 403", status)
	}
	if _, answer := bob.api(http.MethodGet, "/models", nil); !strings.Contains(stringOf(answer), "stub-model") {
		t.Errorf("a member's models = %v", answer)
	}

	if status, _ := alice.api(http.MethodDelete, "/providers/"+id, nil); status != http.StatusNoContent {
		t.Fatalf("removing the provider = %d", status)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/providers/" + id + "/key"); err != nil || secret != nil {
		t.Errorf("the key outlived its provider: %v %v", secret, err)
	}
	var changes int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'provider.changed' AND target_id = $1`, id).Scan(&changes); err != nil || changes != 3 {
		t.Errorf("the audit log holds %d provider.changed entries (%v), want added, changed and removed", changes, err)
	}
}

func TestAnAgentInASandboxThinksWithTheProvidersModelAndActsAsItsPerson(t *testing.T) {
	h := newHarness(t)
	b, image := dockerBackend(t)
	key, product := "llm-key-"+uuid.NewString(), "agent-"+uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": []map[string]any{
		{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}})
	_, bobID, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)

	workflow := `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: agent}
tools:
  t: {tool: ` + product + `, scopes: [mcp], allowTools: [whoami]}
model: {name: stub-model, provider: ` + provider["name"].(string) + `}
limits: {maxSteps: 10, timeout: 5m, maxTokens: 10000}
entry: ask
nodes:
  - {id: ask, type: agent, prompt: 'Who am I?', tools: [t]}
`
	runID := h.newRun(t, bobID, workflow, map[string]any{})
	spawnRunner(t, b, image, runID, h.bootstrap(t, runID))
	if status := h.waitStatus(t, runID, "succeeded", "failed"); status != "succeeded" {
		t.Fatalf("the agent's run ended %s; events: %+v", status, h.events(t, runID))
	}
	events := h.events(t, runID)
	last := events[len(events)-1]
	output, _ := last.Payload["output"].(map[string]any)
	if ask, _ := output["ask"].(map[string]any); ask["text"] != "You are bob." {
		t.Errorf("the agent answered %v", output)
	}
	called := false
	for _, e := range events {
		if e.Type == "tool.called" && strings.Contains(stringOf(e.Payload["resultSummary"]), `"name": "bob"`) {
			called = true
		}
	}
	if !called {
		t.Errorf("the agent did not call whoami as bob: %+v", events)
	}
	if stats := stub.Stats(); stats["llmCalls"] != float64(2) {
		t.Errorf("the stub model was called %v times with the provider's key, want 2", stats["llmCalls"])
	}
}
