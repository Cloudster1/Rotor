//go:build integration

package test

import (
	"net/http"
	"testing"
)

// approvalOf waits until a run waits at an approval, as b sees it, and answers the approval's id.
func approvalOf(t *testing.T, b *browser, runID string) string {
	t.Helper()
	run := waitRun(t, b, runID, "waiting")
	id, _ := run["approvalId"].(string)
	if id == "" {
		t.Fatalf("run %s waits without an approval: %v", runID, run)
	}
	return id
}

// decide answers an approval as b, failing unless the api answers want.
func decide(t *testing.T, b *browser, approvalID string, body map[string]any, want int) map[string]any {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/approvals/"+approvalID+"/decision", body)
	if status != want {
		t.Fatalf("deciding approval %s with %v = %d %v, want %d", approvalID, body, status, answer, want)
	}
	return answer
}

// approve approves the approval a run waits at, as b.
func approve(t *testing.T, b *browser, runID string) {
	t.Helper()
	decide(t, b, approvalOf(t, b, runID), map[string]any{"decision": "approve"}, http.StatusOK)
}
