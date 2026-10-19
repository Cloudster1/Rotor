//go:build integration && suite

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rotorHookFromSuite is where the suite's Exciter reaches Rotor's api, on the suite's network.
const rotorHookFromSuite = "http://rotor-api:8080/api/v1/hooks/"

// hookWait is how long Exciter's worker may take to deliver an event.
const hookWait = 90 * time.Second

// The real Exciter delivers a requirement's creation to Rotor's hook (#35), signed with the secret it made, in
// CloudEvents and in envelope v1, and Rotor queues the event trigger that listens for it.
func TestSuiteExciterDeliversItsEventsToRotorsHook(t *testing.T) {
	h := newHarness(t)
	base, _ := exciter(t)
	alice := suiteBrowser(t, "alice", base)
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": base})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	toolID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+toolID, nil) })
	project := exciterProject(t, alice, base)
	trigger := h.eventTrigger(t, toolID, []string{"requirement.created"}, []string{project}, `[]`, true)

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

		title := "Hooked " + uuid.NewString()[:8]
		status, created := alice.atExciter(t, base, http.MethodPost, "/projects/"+project+"/requirements", map[string]any{"title": title})
		requirement, _ := created["requirement"].(map[string]any)
		if status != http.StatusCreated {
			t.Fatalf("creating a requirement at Exciter = %d %v", status, created)
		}
		key, _ := requirement["key"].(string)
		waitFiring(t, h, trigger, key)
		if status, _ := alice.atExciter(t, base, http.MethodDelete, "/webhooks/"+webhookID, nil); status != http.StatusNoContent && status != http.StatusOK {
			t.Errorf("removing the webhook at Exciter = %d", status)
		}
	}
}

// waitFiring waits until the trigger has a firing for the requirement key, which Exciter's delivery queued.
func waitFiring(t *testing.T, h *harness, trigger uuid.UUID, key string) {
	t.Helper()
	deadline := time.Now().Add(hookWait)
	for time.Now().Before(deadline) {
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM trigger_fire WHERE trigger_id = $1
			AND inputs->'event'->'payload'->'requirement'->>'key' = $2`, trigger, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			return
		}
		time.Sleep(time.Second)
	}
	var events []string
	rows, _ := h.super.Query(context.Background(), `SELECT event_id || ' ' || topic FROM hook_event ORDER BY received_at DESC LIMIT 5`)
	for rows != nil && rows.Next() {
		var e string
		_ = rows.Scan(&e)
		events = append(events, e)
	}
	t.Fatalf("no firing for %s within %s; Rotor's last events: %s", key, hookWait, strings.Join(events, ", "))
}
