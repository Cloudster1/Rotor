//go:build integration && suite

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// shownHookURL is the address Rotor shows an administrator for the tool, which the suite's tools must be able to reach.
func shownHookURL(t *testing.T, admin *browser, toolID string) string {
	t.Helper()
	status, hook := admin.api(http.MethodGet, "/tools/"+toolID+"/hook", nil)
	url, _ := hook["url"].(string)
	if status != http.StatusOK || url != rotorHookFromSuite+toolID {
		t.Fatalf("the hook address Rotor shows = %d %q, want %q", status, url, rotorHookFromSuite+toolID)
	}
	return url
}

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
			"url": shownHookURL(t, alice, toolID), "topics": []string{"requirement.created"}, "projects": []string{project}, "version": version})
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
		runID := waitTriggeredRun(t, h, alice, triggerID, key)
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

// waitTriggeredRun waits until the trigger started a run for the event about the requirement key, and answers it;
// a firing keeps no event once it ends (#41), so the run's inputs, read as its owner, say which event it was.
func waitTriggeredRun(t *testing.T, h *harness, owner *browser, triggerID, key string) string {
	t.Helper()
	deadline := time.Now().Add(hookWait)
	for time.Now().Before(deadline) {
		rows, err := h.super.Query(context.Background(), `SELECT run_id FROM trigger_fire WHERE trigger_id = $1 AND run_id IS NOT NULL`, triggerID)
		if err != nil {
			t.Fatal(err)
		}
		runs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range runs {
			_, got := owner.api(http.MethodGet, "/runs/"+run.String(), nil)
			event, _ := got["inputs"].(map[string]any)["event"].(map[string]any)
			payload, _ := event["payload"].(map[string]any)
			requirement, _ := payload["requirement"].(map[string]any)
			if requirement["key"] == key {
				return run.String()
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("no run for %s within %s", key, hookWait)
	return ""
}
