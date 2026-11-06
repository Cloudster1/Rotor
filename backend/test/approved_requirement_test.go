//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The approved requirement workflow as it ships (#271): the quality group alone decides, its form offers the draft,
// and Exciter gets the values the reviewer corrected, through one approval that beforeWrite counts as its own.
func TestARequirementIsFiledAsItsReviewerCorrectedIt(t *testing.T) {
	h := newHarness(t)
	key := "llm-key-" + uuid.NewString()
	draft := map[string]any{"title": "Export requirments", "statement": "The system shall export requirements quickly.",
		"rationale": "Auditors ask for them.", "notes": "RTR has no export requirement yet; quickly is not measurable."}
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{analyseTurn(draft)}})
	alice, carol := signedIn(t, "alice"), signedIn(t, "carol")
	provider := addProvider(t, alice, llm, key)

	exciterName := "exciter-" + uuid.NewString()[:8]
	exciter := newToolStub(t, map[string]any{"product": exciterName})
	bob, _, _ := connected(t, exciter)
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "approved-requirement.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(example), "tool: exciter\n", "tool: "+exciterName+"\n")
	source = strings.Replace(source, "name: approved-requirement\n", "name: "+uniqueName("approved-requirement")+"\n", 1)
	source = strings.Replace(source, "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, bob, source)
	run := startRun(t, bob, workflowID, map[string]any{"projectKey": "RTR", "need": "Auditors need every requirement as a file."},
		http.StatusCreated)
	runID := run["id"].(string)
	approvalID := approvalOf(t, bob, runID)

	// The drafter may only read, and was offered the skill.
	if _, tools, _ := chatRequest(t, llm, 0); !slices.Contains(tools, "read_skill") || !strings.Contains(fmt.Sprint(tools), "search_requirements") {
		t.Errorf("the drafter was offered the tools %v, want search_requirements and read_skill", tools)
	}

	// carol, of the quality group, sees the draft in the form and the call it is about to make; bob, who started the
	// run, is no member and may not decide.
	status, seen := carol.api(http.MethodGet, "/approvals/"+approvalID, nil)
	if status != http.StatusOK || seen["canDecide"] != true || !strings.Contains(fmt.Sprint(seen["message"]), "quickly is not measurable") {
		t.Fatalf("carol reads the approval = %d %v", status, seen)
	}
	if values := seen["form"].(map[string]any)["values"].(map[string]any); values["title"] != draft["title"] ||
		values["statement"] != draft["statement"] || values["rationale"] != draft["rationale"] {
		t.Errorf("the form proposes %v, want the draft", values)
	}
	if preview, _ := seen["preview"].([]any); len(preview) != 1 || preview[0].(map[string]any)["node"] != "file" {
		t.Errorf("the preview is %v, want the file node's create_requirement", preview)
	}
	decide(t, bob, approvalID, map[string]any{"decision": "approve"}, http.StatusForbidden)

	corrected := map[string]any{"title": "Export requirements as CSV",
		"statement": "The system shall export a project's requirements as CSV within 10 seconds for 5000 requirements.",
		"rationale": "Auditors ask for every requirement as a file they can open in a spreadsheet."}
	decide(t, carol, approvalID, map[string]any{"decision": "approve", "comment": "Made it measurable.", "values": corrected}, http.StatusOK)
	if done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled"); done["status"] != "succeeded" {
		t.Fatalf("the approved run ended %v: %v", done["status"], done["error"])
	}

	requirements, _ := exciter.Records()["requirements"].([]any)
	if len(requirements) != 1 {
		t.Fatalf("Exciter holds the requirements %v, want one", requirements)
	}
	filed := requirements[0].(map[string]any)
	if filed["projectKey"] != "RTR" || filed["title"] != corrected["title"] || filed["statementMarkdown"] != corrected["statement"] ||
		filed["rationaleMarkdown"] != corrected["rationale"] || filed["createdByName"] != "bob" {
		t.Errorf("Exciter got %v, want the corrected values written as bob", filed)
	}

	// The review was the run's one approval: beforeWrite asked for no second one.
	var approvals int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM approval WHERE run_id = $1`, runID).Scan(&approvals); err != nil {
		t.Fatal(err)
	}
	if approvals != 1 {
		t.Errorf("the run waited at %d approvals, want 1", approvals)
	}
}
