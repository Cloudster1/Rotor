//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// guardedWrite calls whoami through a binding that asks for an approval before writing, with no approval node: it
// saves only while the tool lists whoami as reading.
func guardedWrite(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("guarded") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], access: write, approval: beforeWrite, allowTools: [whoami]}
limits: {maxSteps: 5, timeout: 2m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
`
}

// A call a tool marks as writing after the workflow was saved cannot write behind no approval: the next start reads
// the tool afresh, past any cached catalog, and refuses (#90).
func TestARunIsRefusedOnceACallBecameAWriteBehindNoApproval(t *testing.T) {
	newHarness(t)
	product := "flips-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	workflow := createWorkflow(t, bob, guardedWrite(product))
	// The first run is carried to its end, so it holds no slot of bob's concurrent runs for the tests after this.
	first := startRun(t, bob, workflow, map[string]any{}, http.StatusCreated)
	if done := waitRun(t, bob, first["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run while whoami read ended %v: %v", done["status"], done["reason"])
	}

	stub.ListAsWrites("whoami")
	answer := startRun(t, bob, workflow, map[string]any{}, http.StatusUnprocessableEntity)
	refusal, _ := answer["error"].(map[string]any)
	if refusal["code"] != "tools_changed" {
		t.Errorf("starting after whoami became a write answered %v", answer)
	}
}
