//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// An approval's message, preview and values are ciphertext at rest, while the people who see it read them as before
// and the run resumes with what was approved (#91).
func TestAnApprovalsContentIsCiphertextAtRest(t *testing.T) {
	h := newHarness(t)
	product := "sealed-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	proposed, approved := "proposed-"+uuid.NewString()[:8], "approved-"+uuid.NewString()[:8]
	run := startRun(t, bob, createWorkflow(t, bob, reviewed(product, "", "")), map[string]any{"title": proposed}, http.StatusCreated)
	runID := run["id"].(string)
	approvalID := approvalOf(t, bob, runID)

	status, seen := bob.api(http.MethodGet, "/approvals/"+approvalID, nil)
	if status != http.StatusOK || seen["message"] != "File "+proposed+"?" || !strings.Contains(stringify(seen["preview"]), proposed) {
		t.Fatalf("bob reads the approval = %d %v", status, seen)
	}
	if _, inbox := bob.api(http.MethodGet, "/approvals?waitingOnMe=true", nil); !strings.Contains(stringify(inbox), "File "+proposed+"?") {
		t.Errorf("bob's inbox does not show the approval's message: %v", inbox)
	}
	decide(t, bob, approvalID, map[string]any{"decision": "approve", "values": map[string]any{"title": approved}}, http.StatusOK)
	done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
	finish, _ := done["output"].(map[string]any)["finish"].(map[string]any)
	if done["status"] != "succeeded" || finish["title"] != approved {
		t.Fatalf("the resumed run ended %v with %v", done["status"], done["output"])
	}

	var message, preview, values, answer []byte
	if err := h.super.QueryRow(context.Background(), `SELECT sealed_message, sealed_preview, sealed_form_values,
		sealed_approved_values FROM approval WHERE id = $1`, approvalID).Scan(&message, &preview, &values, &answer); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{"message": message, "preview": preview, "form values": values, "approved values": answer} {
		if len(value) == 0 || value[0] != 1 || bytes.Contains(value, []byte(proposed)) || bytes.Contains(value, []byte(approved)) {
			t.Errorf("the approval's %s is not sealed: %s", name, describe(value))
		}
	}
	if _, after := bob.api(http.MethodGet, "/approvals/"+approvalID, nil); !strings.Contains(stringify(after), approved) {
		t.Errorf("the decided approval does not show the approved values: %v", after)
	}
}
