//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// triageCalls are the calls the triage workflow makes with no approval, which the trigger's owner agrees to.
var triageCalls = []string{"armature-write.add_comment", "armature-write.bulk_edit_issues"}

// The triage workflow as it ships (#270): fitting labels in use and a comment why, an unused name dropped rather than
// coined, a labelled issue left alone, and a run by hand that finds nothing fitting writes nothing.
func TestANewIssueIsLabelledFromTheLabelsInUse(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		analyseTurn(map[string]any{"verdict": "label", "labels": []string{"BUG", "export", "crash-report"},
			"reason": "Exporting fails with an error, which is a defect in the export."}),
		analyseTurn(map[string]any{"verdict": "skip", "labels": []any{}, "reason": "Nothing fits a question."}),
	}})
	alice := signedIn(t, "alice")
	provider := addProvider(t, alice, llm, key)

	bob := signedIn(t, "bob")
	armature := newToolStub(t, map[string]any{"product": "armature", "labels": []string{"export", "docs"}, "issues": map[string]any{
		"CP-20": map[string]any{"type": "Bug", "summary": "Export crashes", "descriptionMarkdown": "Exporting a project as CSV fails.",
			"reporter": "bob", "readers": []string{"bob"}},
		"CP-21": map[string]any{"type": "Story", "summary": "Export as PDF", "reporter": "bob", "readers": []string{"bob"},
			"labels": []string{"bug"}},
		"CP-22": map[string]any{"type": "Task", "summary": "Who owns exports?", "reporter": "bob", "readers": []string{"bob"}},
	}})
	armatureID := connectArmature(t, armature, bob)
	secret := "armature_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := alice.api(http.MethodPut, "/tools/"+armatureID.String()+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("setting Armature's secret = %d %v", status, hook)
	}

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "issue-triage.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, bob, source)
	trigger := addTrigger(t, bob, workflowID, map[string]any{"name": "Triage new issues", "kind": "event", "inputs": map[string]any{},
		"allowUnattended": triageCalls, "event": map[string]any{"toolId": armatureID.String(), "topics": []string{"issue.created"},
			"projects": []string{"CP"}}}, http.StatusCreated)
	triggerID := trigger["id"].(string)
	succeeded := func(runID string) map[string]any {
		t.Helper()
		done := waitRun(t, bob, runID, "succeeded", "failed")
		if done["status"] != "succeeded" {
			t.Fatalf("the run ended %v: %v", done["status"], done["error"])
		}
		return done
	}
	created := func(issue string) {
		t.Helper()
		eventID := armatureEvent(t, armatureID.String(), secret, "issue.created",
			fmt.Sprintf(`{"issueId":%q,"key":%q,"summary":"s","actorId":"5d3a8c19-7e2b-4f60-a1d4-8b9c0e6f2a37"}`, uuid.NewString(), issue))
		outcome, reason, run := firing(t, h, triggerID, eventID)
		if outcome != "started" || run == nil {
			t.Fatalf("the firing for %s ended %q (%q)", issue, outcome, reason)
		}
		t.Logf("run %s answered %v", run, succeeded(run.String())["output"])
	}
	labelsOf := func(issue string) []any {
		labels, _ := armature.Records()["labels"].(map[string]any)[issue].([]any)
		return labels
	}
	commentsOn := func(issue string) []any {
		comments, _ := armature.Records()["comments"].(map[string]any)[issue].([]any)
		return comments
	}

	// A new bug: the labels in use that fit, in their own spelling, and one comment saying why; the name nobody uses
	// is dropped, since Armature would coin it.
	created("CP-20")
	if got := fmt.Sprint(labelsOf("CP-20")); got != "[bug export]" {
		t.Errorf("CP-20 was labelled %s, want bug and export", got)
	}
	comments := commentsOn("CP-20")
	for _, want := range []string{"Labels from Rotor", "`bug`, `export`", "a defect in the export"} {
		if len(comments) != 1 || !strings.Contains(stringOf(comments[0]), want) {
			t.Fatalf("CP-20 has the comments %v, want one with %q", comments, want)
		}
	}
	if strings.Contains(stringOf(comments[0]), "crash-report") {
		t.Errorf("the comment names a label that was not added: %v", comments[0])
	}
	if _, tools, _ := chatRequest(t, llm, 0); len(tools) != 1 {
		t.Errorf("the agent was offered the tools %v, want only its answer", tools)
	}

	// An issue that has labels is left alone, and the model is not asked.
	created("CP-21")
	if labelsOf("CP-21") != nil || len(commentsOn("CP-21")) != 0 {
		t.Errorf("CP-21, labelled before, got the labels %v and comments %v", labelsOf("CP-21"), commentsOn("CP-21"))
	}
	if requests, _ := llm.Stats()["llmRequests"].([]any); len(requests) != 1 {
		t.Errorf("the stub model was asked %d times, want once", len(requests))
	}

	// By hand, with an issue key: nothing fits, so nothing is written.
	run := startRun(t, bob, workflowID, map[string]any{"issueKey": "CP-22"}, http.StatusCreated)
	if output := fmt.Sprint(succeeded(run["id"].(string))["output"]); !strings.Contains(output, "No label in use fits") {
		t.Errorf("the run by hand answered %s, want that no label fits", output)
	}
	if labelsOf("CP-22") != nil || len(commentsOn("CP-22")) != 0 {
		t.Errorf("CP-22 got the labels %v and comments %v", labelsOf("CP-22"), commentsOn("CP-22"))
	}

	// The labels and the comment are the triggered run's only writes, each unattended and audited.
	var audited int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'trigger.unattended_write' AND target_id = $1`, triggerID).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 2 {
		t.Errorf("the audit log holds %d unattended writes, want 2", audited)
	}
}
