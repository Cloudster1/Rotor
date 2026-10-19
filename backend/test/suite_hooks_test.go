//go:build integration && suite

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rotorHookFromSuite is where the suite's Exciter reaches Rotor's api, on the suite's network.
const rotorHookFromSuite = "http://rotor-api:8080/api/v1/hooks/"

// hookWait is how long Exciter's worker may take to deliver an event and Rotor's to start its run.
const hookWait = 2 * time.Minute

// The real Exciter delivers a requirement's creation in CloudEvents and in envelope v1 (#35), and alice's event
// trigger runs her workflow as her with the event, since Exciter shows her the requirement.
func TestSuiteExciterEventsStartATriggerAsItsOwner(t *testing.T) {
	h := newHarness(t)
	base, _ := exciter(t)
	alice := suiteBrowser(t, "alice", base)
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": base})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	toolID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+toolID, nil) })
	alice.connectExciter(t, base, toolID, "alice")
	project := exciterProject(t, alice, base)
	workflowID := createWorkflow(t, alice, echoEvent())
	var triggerID string

	for _, version := range []int{2, 1} {
		status, made := alice.atExciter(t, base, http.MethodPost, "/webhooks", map[string]any{"name": "Rotor " + uuid.NewString()[:8],
			"url": rotorHookFromSuite + toolID, "topics": []string{"requirement.created"}, "projects": []string{project}, "version": version})
		webhook, _ := made["webhook"].(map[string]any)
		secret, _ := webhook["secret"].(string)
		if status != http.StatusCreated || secret == "" {
			t.Fatalf("registering Rotor's webhook (version %d) at Exciter = %d %v", version, status, made)
		}
		webhookID, _ := webhook["id"].(string)
		if status, hook := alice.api(http.MethodPut, "/tools/"+toolID+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
			t.Fatalf("giving Rotor Exciter's secret = %d %v", status, hook)
		}
		if triggerID == "" {
			trigger := addTrigger(t, alice, workflowID, map[string]any{"name": "On new requirements", "kind": "event",
				"event": map[string]any{"toolId": toolID, "topics": []string{"requirement.created"}, "projects": []string{project}}}, http.StatusCreated)
			triggerID, _ = trigger["id"].(string)
		}

		status, created := alice.atExciter(t, base, http.MethodPost, "/projects/"+project+"/requirements", map[string]any{"title": "Hooked " + uuid.NewString()[:8]})
		requirement, _ := created["requirement"].(map[string]any)
		if status != http.StatusCreated {
			t.Fatalf("creating a requirement at Exciter = %d %v", status, created)
		}
		key, _ := requirement["key"].(string)
		runID := waitTriggeredRun(t, h, triggerID, key)
		done := waitRun(t, alice, runID, "succeeded", "failed")
		echo, _ := done["output"].(map[string]any)["echo"].(map[string]any)
		if done["status"] != "succeeded" || echo["key"] != key {
			t.Errorf("the run of version %d's event ended %v with %v", version, done["status"], done["output"])
		}
		if status, _ := alice.atExciter(t, base, http.MethodDelete, "/webhooks/"+webhookID, nil); status != http.StatusNoContent && status != http.StatusOK {
			t.Errorf("removing the webhook at Exciter = %d", status)
		}
	}
}

// waitTriggeredRun waits until the trigger started a run for the event about the requirement key, and answers it.
func waitTriggeredRun(t *testing.T, h *harness, triggerID, key string) string {
	t.Helper()
	deadline := time.Now().Add(hookWait)
	var outcome, reason string
	for time.Now().Before(deadline) {
		var run *uuid.UUID
		err := h.super.QueryRow(context.Background(), `SELECT COALESCE(outcome, ''), reason, run_id FROM trigger_fire WHERE trigger_id = $1
			AND inputs->'event'->'payload'->'requirement'->>'key' = $2`, triggerID, key).Scan(&outcome, &reason, &run)
		if err == nil && outcome != "" {
			if run == nil {
				t.Fatalf("the firing for %s ended %s: %s", key, outcome, reason)
			}
			return run.String()
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("no run for %s within %s (firing: %q %q)", key, hookWait, outcome, reason)
	return ""
}
