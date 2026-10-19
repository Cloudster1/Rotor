//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// reviewed asks the tool who the run acts as, waits for a person to approve a title, then calls the tool again
// through a write binding and returns the approved title and comment.
func reviewed(tool, group, expires string) string {
	approvers := ""
	if group != "" {
		approvers = "\n    approvers: {group: " + group + "}"
	}
	if expires != "" {
		approvers += "\n    expiresIn: " + expires
	}
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("reviewed") + `}
inputs: {type: object, properties: {title: {type: string}}}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], access: write, allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - id: review
    type: approval
    message: 'File {{ inputs.title }}?'` + approvers + `
    form:
      schema: {type: object, required: [title], properties: {title: {type: string, minLength: 1}}}
      values: {title: '{{ inputs.title }}'}
  - {id: file, type: tool, tool: t, call: whoami, arguments: {title: '{{ nodes.review.title }}'}}
  - id: finish
    type: code
    output: {schema: {type: object, required: [title]}}
    source: |
      def run(state, ctx):
          review = state["nodes"]["review"]
          return {"title": review["title"], "comment": review["comment"], "by": review["decidedBy"]}
edges:
  - {from: ask, to: review}
  - {from: review, to: file}
  - {from: file, to: finish}
`
}

// signedIn is a browser signed in as one of the realm's people.
func signedIn(t *testing.T, person string) *browser {
	t.Helper()
	b := newBrowser(t)
	b.signIn(person)
	return b
}

func TestAGroupDecidesAnApprovalAndTheRunGoesOnInAFreshSandbox(t *testing.T) {
	h := newHarness(t)
	b, _ := dockerBackend(t)
	product := "approve-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	carol, alice, dave := signedIn(t, "carol"), signedIn(t, "alice"), signedIn(t, "dave")
	run := startRun(t, bob, createWorkflow(t, bob, reviewed(product, "quality", "")), map[string]any{"title": "Login"}, http.StatusCreated)
	runID := run["id"].(string)
	approvalID := approvalOf(t, bob, runID)

	// The approval shows what the run is about to write, as the runner rendered it from the state.
	status, seen := carol.api(http.MethodGet, "/approvals/"+approvalID, nil)
	if status != http.StatusOK || seen["canDecide"] != true || seen["message"] != "File Login?" {
		t.Fatalf("carol reads the approval = %d %v", status, seen)
	}
	preview, _ := seen["preview"].([]any)
	if len(preview) != 1 || preview[0].(map[string]any)["node"] != "file" ||
		preview[0].(map[string]any)["arguments"].(map[string]any)["title"] != "Login" {
		t.Errorf("the preview is %v, want the file node's call with the proposed title", preview)
	}
	if form, _ := seen["form"].(map[string]any); form["values"].(map[string]any)["title"] != "Login" {
		t.Errorf("the form proposes %v", form)
	}

	// The starter is not of the group, the administrator only sees, and anyone else sees nothing.
	for name, who := range map[string]*browser{"bob": bob, "alice": alice} {
		decide(t, who, approvalID, map[string]any{"decision": "approve"}, http.StatusForbidden)
		if _, a := who.api(http.MethodGet, "/approvals/"+approvalID, nil); a["canDecide"] != false {
			t.Errorf("%s may decide an approval of the quality group", name)
		}
	}
	if status, _ := dave.api(http.MethodGet, "/approvals/"+approvalID, nil); status != http.StatusNotFound {
		t.Errorf("an outsider reads the approval with %d", status)
	}
	if status, _ := dave.api(http.MethodGet, "/runs/"+runID, nil); status != http.StatusNotFound {
		t.Errorf("an outsider reads the run with %d", status)
	}
	// carol sees the run she decides on, but it is not hers to list or cancel.
	if status, _ := carol.api(http.MethodGet, "/runs/"+runID, nil); status != http.StatusOK {
		t.Errorf("the approver reads the run with %d", status)
	}
	if _, listed := carol.api(http.MethodGet, "/runs", nil); strings.Contains(stringify(listed), runID) {
		t.Error("the approver finds the run under her runs")
	}
	if status, answer := carol.api(http.MethodPost, "/runs/"+runID+"/cancel", nil); status != http.StatusForbidden || errorCode(answer) != "not_run_manager" {
		t.Errorf("the approver cancelling = %d %v, want 403 not_run_manager", status, answer)
	}
	if _, count := carol.api(http.MethodGet, "/approvals/count", nil); count["waitingOnMe"].(float64) < 1 {
		t.Errorf("carol's badge says %v", count)
	}
	if _, inbox := carol.api(http.MethodGet, "/approvals?waitingOnMe=true", nil); !strings.Contains(stringify(inbox), approvalID) {
		t.Error("the approval is not in carol's inbox")
	}

	// While it waits the run keeps no sandbox and no token.
	waitNoSandbox(t, b, runID)
	waitRevoked(t, h, uuid.MustParse(runID))

	decide(t, carol, approvalID, map[string]any{"decision": "approve", "values": map[string]any{"title": ""}}, http.StatusUnprocessableEntity)
	decide(t, carol, approvalID, map[string]any{"decision": "approve", "values": map[string]any{"nope": 1}}, http.StatusUnprocessableEntity)
	decided := decide(t, carol, approvalID, map[string]any{"decision": "approve", "comment": "renamed", "values": map[string]any{"title": "Sign-in"}}, http.StatusOK)
	if decided["status"] != "approved" || decided["decision"].(map[string]any)["comment"] != "renamed" {
		t.Errorf("the decision reads %v", decided)
	}
	if answer := decide(t, carol, approvalID, map[string]any{"decision": "reject"}, http.StatusConflict); errorCode(answer) != "approval_decided" {
		t.Errorf("deciding twice = %v", answer)
	}

	done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
	finish, _ := done["output"].(map[string]any)["finish"].(map[string]any)
	if done["status"] != "succeeded" || finish["title"] != "Sign-in" || finish["comment"] != "renamed" || finish["by"] == "" {
		t.Fatalf("the resumed run ended %v with %v; events %+v", done["status"], done["output"], h.events(t, uuid.MustParse(runID)))
	}
	kinds := map[string]int{}
	for _, e := range h.events(t, uuid.MustParse(runID)) {
		kinds[e.Type+":"+e.Node]++
	}
	// The approval node starts again when it is answered; every other node runs once.
	if kinds["approval.requested:review"] != 1 || kinds["approval.decided:"] != 1 || kinds["node.started:ask"] != 1 ||
		kinds["node.started:file"] != 1 || kinds["node.started:finish"] != 1 {
		t.Errorf("the run's events count %v", kinds)
	}

	// The audit log names the edited field, never its value.
	var data string
	if err := h.super.QueryRow(context.Background(), `SELECT data::text FROM audit_log WHERE action = 'approval.decided' AND target_id = $1`,
		approvalID).Scan(&data); err != nil {
		t.Fatalf("no approval.decided in the audit log: %v", err)
	}
	if !strings.Contains(data, `"edited": ["title"]`) || strings.Contains(data, "Sign-in") {
		t.Errorf("the audit entry holds %s", data)
	}
}

func TestARejectedApprovalEndsTheRunAndOnlyItsStarterDecides(t *testing.T) {
	product := "reject-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	carol := signedIn(t, "carol")
	run := startRun(t, bob, createWorkflow(t, bob, reviewed(product, "", "")), map[string]any{"title": "Login"}, http.StatusCreated)
	runID := run["id"].(string)
	approvalID := approvalOf(t, bob, runID)
	if status, _ := carol.api(http.MethodGet, "/approvals/"+approvalID, nil); status != http.StatusNotFound {
		t.Errorf("someone else reads the starter's approval with %d", status)
	}

	decide(t, bob, approvalID, map[string]any{"decision": "reject", "comment": "not now"}, http.StatusOK)
	_, ended := bob.api(http.MethodGet, "/runs/"+runID, nil)
	if ended["status"] != "cancelled" || ended["reason"] != "rejected" || ended["approvalId"] != nil {
		t.Errorf("the rejected run reads %v, want cancelled at once with reason rejected", ended)
	}
}

func TestAnApprovalNobodyDecidesExpiresItsRunAndACancelEndsIt(t *testing.T) {
	product := "expire-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)

	expiring := startRun(t, bob, createWorkflow(t, bob, reviewed(product, "", "1s")), map[string]any{"title": "Login"}, http.StatusCreated)
	runID := expiring["id"].(string)
	approvalID := approvalOf(t, bob, runID)
	if done := waitRun(t, bob, runID, "expired", "cancelled", "failed"); done["status"] != "expired" || done["reason"] != "approval_expired" {
		t.Errorf("the run ended %v (%v), want expired with approval_expired", done["status"], done["reason"])
	}
	if _, a := bob.api(http.MethodGet, "/approvals/"+approvalID, nil); a["status"] != "expired" {
		t.Errorf("the approval is %v, want expired", a["status"])
	}
	decide(t, bob, approvalID, map[string]any{"decision": "approve"}, http.StatusConflict)

	cancelled := startRun(t, bob, createWorkflow(t, bob, reviewed(product, "", "")), map[string]any{"title": "Login"}, http.StatusCreated)
	runID = cancelled["id"].(string)
	approvalID = approvalOf(t, bob, runID)
	if status, answer := bob.api(http.MethodPost, "/runs/"+runID+"/cancel", nil); status != http.StatusOK {
		t.Fatalf("cancelling the waiting run = %d %v", status, answer)
	}
	if _, a := bob.api(http.MethodGet, "/approvals/"+approvalID, nil); a["status"] != "cancelled" {
		t.Errorf("the approval of a cancelled run is %v", a["status"])
	}
}

func errorCode(answer map[string]any) string {
	e, _ := answer["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func stringify(v any) string {
	out, _ := json.Marshal(v)
	return string(out)
}
