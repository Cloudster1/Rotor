//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// echoEvent is a workflow that reads nothing from a tool and returns the key of the requirement its event names.
func echoEvent() string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("echo-event") + `}
inputs: {type: object, required: [event], properties: {event: {type: object}}}
limits: {maxSteps: 5, timeout: 2m}
entry: echo
nodes:
  - id: echo
    type: code
    output: {schema: {type: object, required: [key]}}
    source: |
      def run(state, ctx):
          return {"key": state["inputs"]["event"]["payload"]["requirement"]["key"], "topic": state["inputs"]["event"]["topic"]}
`
}

// A delivery for an event trigger starts its workflow as the trigger's owner with the event, once the tool says the
// owner may read the event's requirement; an event about a requirement they may not see starts nothing (#35).
func TestAToolEventStartsARunAsTheOwnerOnlyForWhatTheyMayRead(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, map[string]any{"product": "exciter", "requirements": map[string]any{"RTR-1": []string{"bob"}, "RTR-2": []string{"alice"}}})
	bob, bobID, toolID := connected(t, stub)
	alice := signedIn(t, "alice")
	secret := "exciter_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	workflowID := createWorkflow(t, bob, echoEvent())
	event := func(topics []string) map[string]any {
		return map[string]any{"name": "On new requirements", "kind": "event",
			"event": map[string]any{"toolId": toolID.String(), "topics": topics, "projects": []string{"RTR"}}}
	}

	// Without the tool's secret its events cannot arrive, and a trigger needs topics that read.
	if answer := addTrigger(t, bob, workflowID, event([]string{"requirement.created"}), http.StatusUnprocessableEntity); !strings.Contains(stringify(answer), "event/toolId") {
		t.Errorf("a trigger on a tool without a secret was refused with %v", answer)
	}
	if status, hook := alice.api(http.MethodPut, "/tools/"+toolID.String()+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("setting the secret = %d %v", status, hook)
	}
	if answer := addTrigger(t, bob, workflowID, event([]string{"Requirement Created"}), http.StatusUnprocessableEntity); !strings.Contains(stringify(answer), "event/topics/0") {
		t.Errorf("a topic that does not read was refused with %v", answer)
	}
	trigger := addTrigger(t, bob, workflowID, event([]string{"requirement.created"}), http.StatusCreated)
	triggerID := trigger["id"].(string)

	live := stub.Stats()["liveAccessTokens"]
	readable := uuid.NewString()
	if status, answer := deliver(t, toolID.String(), "Exciter", secret, envelope(readable, "requirement.created",
		`{"requirement":{"key":"RTR-1","project":{"key":"RTR"}}}`), time.Now(), "1", nil); status != http.StatusAccepted {
		t.Fatalf("the delivery = %d %v", status, answer)
	}
	started := lastFire(t, bob, triggerID, "started", "skipped", "refused", "failed")
	fire := started["lastFire"].(map[string]any)
	runID, _ := fire["runId"].(string)
	if fire["outcome"] != "started" || runID == "" {
		t.Fatalf("the readable event's firing ended %v", fire)
	}
	done := waitRun(t, bob, runID, "succeeded", "failed")
	echo, _ := done["output"].(map[string]any)["echo"].(map[string]any)
	if done["status"] != "succeeded" || echo["key"] != "RTR-1" || echo["topic"] != "requirement.created" {
		t.Fatalf("the triggered run ended %v with %v", done["status"], done["output"])
	}
	var startedBy uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT user_id FROM run WHERE id = $1 AND trigger_id = $2`, runID, triggerID).Scan(&startedBy); err != nil || startedBy != bobID {
		t.Errorf("the run acts as %v (%v), want the trigger's owner %v", startedBy, err, bobID)
	}

	hidden := uuid.NewString()
	if status, answer := deliver(t, toolID.String(), "Exciter", secret, envelope(hidden, "requirement.created",
		`{"requirement":{"key":"RTR-2","project":{"key":"RTR"}}}`), time.Now(), "1", nil); status != http.StatusAccepted {
		t.Fatalf("the second delivery = %d %v", status, answer)
	}
	deadline := time.Now().Add(sandboxWait)
	var outcome, reason string
	var run *uuid.UUID
	for time.Now().Before(deadline) {
		err := h.super.QueryRow(context.Background(), `SELECT COALESCE(outcome, ''), reason, run_id FROM trigger_fire
			WHERE trigger_id = $1 AND fire_key = $2`, triggerID, "event:"+hidden).Scan(&outcome, &reason, &run)
		if err == nil && outcome != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if outcome != "skipped" || run != nil || !strings.Contains(reason, "cannot read") {
		t.Errorf("an event about a requirement bob may not see ended %q (%q), run %v", outcome, reason, run)
	}
	if now := stub.Stats()["liveAccessTokens"]; now != live {
		t.Errorf("the probes left %v access tokens live at the tool, %v before", now, live)
	}
}
