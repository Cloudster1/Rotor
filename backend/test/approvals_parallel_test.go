//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// parallelReview asks the tool who the run acts as, then has the quality group and the starter review at once; gather
// waits for both and says who decided each (#135).
func parallelReview(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("parallel") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - {id: review-code, type: approval, message: 'Is the code fine?', approvers: {group: quality}}
  - {id: review-security, type: approval, message: 'Is it safe?'}
  - id: gather
    type: code
    join: all
    output: {schema: {type: object, required: [code, security]}}
    source: |
      def run(state, ctx):
          nodes = state["nodes"]
          return {"code": nodes["review-code"]["decidedBy"], "security": nodes["review-security"]["decidedBy"]}
edges:
  - {from: ask, to: review-code}
  - {from: ask, to: review-security}
  - {from: review-code, to: gather}
  - {from: review-security, to: gather}
`
}

// waitingAt waits until a run waits at want approvals, and answers them by node, as b sees them.
func waitingAt(t *testing.T, b *browser, runID string, want int) map[string]string {
	t.Helper()
	run := waitRun(t, b, runID, "waiting")
	ids, _ := run["approvalIds"].([]any)
	if len(ids) != want || run["approvalId"] != ids[0] {
		t.Fatalf("run %s waits at %v (first %v), want %d approvals", runID, ids, run["approvalId"], want)
	}
	out := map[string]string{}
	for _, id := range ids {
		_, a := b.api(http.MethodGet, "/approvals/"+id.(string), nil)
		out[a["node"].(string)] = id.(string)
	}
	return out
}

func TestParallelReviewsWaitTogetherAndTheRunGoesOnOnceBothAreDecided(t *testing.T) {
	h := newHarness(t)
	product := "parallel-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	carol := signedIn(t, "carol")
	runID := startRun(t, bob, createWorkflow(t, bob, parallelReview(product)), map[string]any{}, http.StatusCreated)["id"].(string)
	waiting := waitingAt(t, bob, runID, 2)
	if waiting["review-code"] == "" || waiting["review-security"] == "" {
		t.Fatalf("the run waits at %v", waiting)
	}
	if _, inbox := carol.api(http.MethodGet, "/approvals?waitingOnMe=true", nil); !strings.Contains(stringify(inbox), waiting["review-code"]) {
		t.Error("the code review is not in carol's inbox")
	}
	if _, inbox := bob.api(http.MethodGet, "/approvals?waitingOnMe=true", nil); !strings.Contains(stringify(inbox), waiting["review-security"]) {
		t.Error("the security review is not in bob's inbox")
	}

	// One decided, the run waits on for the other, with no sandbox.
	decide(t, carol, waiting["review-code"], map[string]any{"decision": "approve"}, http.StatusOK)
	if left := waitingAt(t, bob, runID, 1); left["review-security"] != waiting["review-security"] {
		t.Fatalf("after one decision the run waits at %v", left)
	}
	decide(t, bob, waiting["review-security"], map[string]any{"decision": "approve"}, http.StatusOK)

	done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
	gather, _ := done["output"].(map[string]any)["gather"].(map[string]any)
	if done["status"] != "succeeded" || gather["code"] == "" || gather["security"] == "" || gather["code"] == gather["security"] {
		t.Fatalf("the run ended %v with %v; events %+v", done["status"], done["output"], h.events(t, uuid.MustParse(runID)))
	}
	kinds := map[string]int{}
	for _, e := range h.events(t, uuid.MustParse(runID)) {
		kinds[e.Type+":"+e.Node]++
	}
	if kinds["approval.requested:review-code"] != 1 || kinds["approval.requested:review-security"] != 1 ||
		kinds["approval.decided:"] != 2 || kinds["node.started:ask"] != 1 || kinds["node.started:gather"] != 1 {
		t.Errorf("the run's events count %v", kinds)
	}
}

func TestOneRejectedReviewOfTwoEndsTheRunAndClosesTheOther(t *testing.T) {
	product := "parallel-no-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	runID := startRun(t, bob, createWorkflow(t, bob, parallelReview(product)), map[string]any{}, http.StatusCreated)["id"].(string)
	waiting := waitingAt(t, bob, runID, 2)

	decide(t, bob, waiting["review-security"], map[string]any{"decision": "reject", "comment": "not safe"}, http.StatusOK)
	_, ended := bob.api(http.MethodGet, "/runs/"+runID, nil)
	if ended["status"] != "cancelled" || ended["reason"] != "rejected" || len(ended["approvalIds"].([]any)) != 0 {
		t.Errorf("the rejected run reads %v, want cancelled at once with reason rejected", ended)
	}
	if _, a := bob.api(http.MethodGet, "/approvals/"+waiting["review-code"], nil); a["status"] != "cancelled" {
		t.Errorf("the other review is %v, want cancelled with its run", a["status"])
	}
}
