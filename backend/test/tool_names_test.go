//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// bindsTool is a workflow that reads whoami through a binding of the tool name, which a run carries to its end.
func bindsTool(name string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("binds") + `}
tools:
  t: {tool: ` + name + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 5, timeout: 2m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
`
}

// problemsAt are the codes validating source as b answers, by where each is.
func problemsAt(t *testing.T, b *browser, source string) map[string]string {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": source})
	if status != http.StatusOK {
		t.Fatalf("validating = %d %v", status, answer)
	}
	out := map[string]string{}
	problems, _ := answer["problems"].([]any)
	for _, raw := range problems {
		p, _ := raw.(map[string]any)
		out[stringOf(p["path"])] = stringOf(p["code"])
	}
	return out
}

// errorOf is the error an answer carries.
func errorOf(answer map[string]any) map[string]any {
	e, _ := answer["error"].(map[string]any)
	return e
}

// Adding a tool under a name another has is refused in a sentence asking for another, and a name given is taken (#267).
func TestAddingAToolUnderATakenNameIsRefused(t *testing.T) {
	newHarness(t)
	product := "names-" + uuid.NewString()[:8]
	addTool(t, newToolStub(t, map[string]any{"product": product}))
	alice := signedIn(t, "alice")
	twin := newToolStub(t, map[string]any{"product": product})

	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": twin.BaseURL})
	if e := errorOf(answer); status != http.StatusConflict || e["code"] != "tool_name_taken" ||
		!strings.Contains(stringOf(e["message"]), "Give this one another name, such as "+product+"-2.") {
		t.Fatalf("adding a second tool called %s = %d %v", product, status, answer)
	}
	if status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": twin.BaseURL, "name": "Not One Word"}); status !=
		http.StatusUnprocessableEntity || errorOf(answer)["code"] != "validation_failed" {
		t.Errorf("adding under a name of several words = %d %v", status, answer)
	}

	status, answer = alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": twin.BaseURL, "name": product + "-two"})
	if status != http.StatusCreated {
		t.Fatalf("adding under a name of its own = %d %v", status, answer)
	}
	tool := toolOf(t, answer)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+stringOf(tool["id"]), nil) })
	if tool["name"] != product+"-two" || tool["product"] != product {
		t.Errorf("the tool added under a name of its own is %v", tool)
	}
}

// Renaming a tool is an administrator's, audited, lists the workflows binding the old name, and a workflow that binds
// the new one validates and runs while the old binding finds no tool (#267).
func TestRenamingAToolIsAuditedAndTheNewNameBinds(t *testing.T) {
	h := newHarness(t)
	product := "renamed-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, toolID := connected(t, stub)
	alice := signedIn(t, "alice")
	old := createWorkflow(t, bob, bindsTool(product))
	id := toolID.String()

	status, answer := alice.api(http.MethodGet, "/tools/"+id+"/workflows", nil)
	if status != http.StatusOK || !strings.Contains(stringOf(answer["workflows"]), old) {
		t.Errorf("the workflows binding %s = %d %v, want %s", product, status, answer, old)
	}
	if status, _ := bob.api(http.MethodPut, "/tools/"+id+"/name", map[string]any{"name": product + "-x"}); status != http.StatusForbidden {
		t.Errorf("a member renaming a tool = %d, want 403", status)
	}
	if status, answer := alice.api(http.MethodPut, "/tools/"+id+"/name", map[string]any{"name": "-dash"}); status != http.StatusUnprocessableEntity {
		t.Errorf("renaming to -dash = %d %v", status, answer)
	}

	renamed := product + "-prod"
	status, answer = alice.api(http.MethodPut, "/tools/"+id+"/name", map[string]any{"name": renamed})
	if tool := toolOf(t, answer); status != http.StatusOK || tool["name"] != renamed || tool["product"] != product {
		t.Fatalf("renaming the tool = %d %v", status, answer)
	}
	if !strings.Contains(stringOf(answer["workflows"]), old) {
		t.Errorf("renaming does not name the workflow still binding %s: %v", product, answer)
	}
	if !h.audited(t, "tool.renamed", id) {
		t.Error("the audit log does not record tool.renamed")
	}

	if codes := problemsAt(t, bob, bindsTool(product)); codes["/tools/t/tool"] != "unknown_tool" {
		t.Errorf("binding the old name validates as %v, want unknown_tool", codes)
	}
	if codes := problemsAt(t, bob, bindsTool(renamed)); len(codes) != 0 {
		t.Errorf("binding the new name validates as %v, want no problems", codes)
	}
	status, answer = bob.api(http.MethodPost, "/workflows/"+old+"/runs", map[string]any{"inputs": map[string]any{}})
	if e := errorOf(answer); status != http.StatusConflict || e["code"] != "connections_needed" {
		t.Errorf("starting the workflow binding the old name = %d %v", status, answer)
	}
	run := startRun(t, bob, createWorkflow(t, bob, bindsTool(renamed)), map[string]any{}, http.StatusCreated)
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Errorf("the run binding the new name ended %v: %v", done["status"], done["reason"])
	}
}

// Checking a tool again keeps the name workflows bind, even once the tool's meta names another product, and keeps
// that product instead (#267).
func TestCheckingAToolAgainKeepsItsName(t *testing.T) {
	h := newHarness(t)
	product := "checked-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	id := addTool(t, stub)
	alice := signedIn(t, "alice")

	// The tenant made again names another product, as a tool updated to another release might.
	stub.controlCall(http.MethodPost, "/control/tenants", map[string]any{"name": stub.name, "product": product + "-next"}, http.StatusCreated)
	status, answer := alice.api(http.MethodPost, "/tools/"+id+"/check", nil)
	if tool := toolOf(t, answer); status != http.StatusOK || tool["name"] != product || tool["product"] != product+"-next" {
		t.Fatalf("checking the tool once its product changed = %d %v, want the name %s kept", status, answer, product)
	}
	var was string
	if err := h.super.QueryRow(context.Background(), `SELECT data ->> 'product_was' FROM audit_log
		WHERE action = 'tool.checked' AND target_id = $1 ORDER BY created_at DESC LIMIT 1`, id).Scan(&was); err != nil || was != product {
		t.Errorf("tool.checked records the product before as %q (%v), want %s", was, err, product)
	}

	renamed := product + "-mine"
	if status, answer := alice.api(http.MethodPut, "/tools/"+id+"/name", map[string]any{"name": renamed}); status != http.StatusOK {
		t.Fatalf("renaming = %d %v", status, answer)
	}
	if status, answer := alice.api(http.MethodPost, "/tools/"+id+"/check", nil); status != http.StatusOK || toolOf(t, answer)["name"] != renamed {
		t.Errorf("checking a renamed tool = %d %v, want %s kept", status, answer, renamed)
	}
}
