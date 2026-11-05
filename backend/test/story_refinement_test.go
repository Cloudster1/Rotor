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

	"github.com/cloudster1/rotor/backend/internal/connections"
)

// refinementCalls are the calls the refinement workflow makes with no approval, which the trigger's owner agrees to.
var refinementCalls = []string{"armature-write.add_comment"}

// The refinement workflow as it ships (#212): questions, its own comments ignored, the answer turned into a refined
// story on the ticket, story-to-requirements' questions taken for Rotor's, and a bug left alone.
func TestAStoryIsRefinedOnItsTicket(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		analyseTurn(map[string]any{"verdict": "questions", "questions": []string{"Which formats must the export offer?"}}),
		analyseTurn(map[string]any{"verdict": "refined", "questions": []any{}, "refinement": map[string]any{
			"story": "As a reviewer, I want to export a project's requirements, so that I can review them offline.",
			"criteria": []map[string]any{
				{"given": "a project with requirements", "when": "the reviewer exports it as CSV", "then": "the file holds one row per requirement"},
				{"given": "a reviewer who may not read the project", "when": "they export it", "then": "the export is refused"}},
			"outOfScope": []string{"Importing the file again"}, "assumptions": []string{"PDF can follow later"},
			"split": []any{}}}),
	}})
	alice := signedIn(t, "alice")
	provider := addProvider(t, alice, llm, key)

	bob := signedIn(t, "bob")
	bobID := signedInID(t, bob)
	armature := newToolStub(t, map[string]any{"product": "armature", "issues": map[string]any{
		"CP-12": map[string]any{"type": "Story", "summary": "Export requirements", "descriptionMarkdown": "As a reviewer I want to export them.",
			"reporter": "bob", "readers": []string{"bob"}},
		"CP-13": map[string]any{"type": "Bug", "summary": "Export crashes", "reporter": "bob", "readers": []string{"bob"}},
	}})
	armatureID := connectArmature(t, armature, bob)
	secret := "armature_whs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if status, hook := alice.api(http.MethodPut, "/tools/"+armatureID.String()+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("setting Armature's secret = %d %v", status, hook)
	}

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "story-refinement.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, bob, source)
	trigger := addTrigger(t, bob, workflowID, map[string]any{"name": "Refine stories", "kind": "event", "inputs": map[string]any{},
		"allowUnattended": refinementCalls, "event": map[string]any{"toolId": armatureID.String(), "topics": []string{"issue.created", "comment.added"},
			"projects": []string{"CP"}, "conditions": []map[string]any{{"path": "payload.internal", "op": "notEquals", "value": true}}}},
		http.StatusCreated)
	triggerID := trigger["id"].(string)
	ran := func(eventID string) {
		t.Helper()
		outcome, reason, run := firing(t, h, triggerID, eventID)
		if outcome != "started" || run == nil {
			t.Fatalf("the firing ended %q (%q)", outcome, reason)
		}
		done := waitRun(t, bob, run.String(), "succeeded", "failed")
		if done["status"] != "succeeded" {
			t.Fatalf("the run ended %v: %v", done["status"], done["error"])
		}
		t.Logf("run %s answered %v", run, done["output"])
	}
	commented := func(commentID string) string {
		return armatureEvent(t, armatureID.String(), secret, "comment.added",
			fmt.Sprintf(`{"issueId":"i","key":"CP-12","commentId":%q,"actorId":"a","internal":false,"mentions":null}`, commentID))
	}
	commentsOn := func(issue string) []any {
		comments, _ := armature.Records()["comments"].(map[string]any)[issue].([]any)
		return comments
	}
	token, err := controlPlane(t, h, nil).Mint(ctx, connections.MintRequest{UserID: bobID, ToolID: armatureID, Scopes: []string{"comment.write"}, RunID: "bob-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	comment := func(body string) string {
		answer := mcpCall(t, armature, token.Value, "add_comment", map[string]any{"issueKey": "CP-12", "bodyMarkdown": body})
		id, _ := answer["comment"].(map[string]any)["id"].(string)
		return id
	}

	// A new story: refinement questions on its ticket, and that comment does not start the workflow again.
	ran(armatureEvent(t, armatureID.String(), secret, "issue.created",
		`{"issueId":"0b6e2f7a-3c41-4d8e-9f52-1a7c9e4b2d60","key":"CP-12","summary":"Export requirements","actorId":"5d3a8c19-7e2b-4f60-a1d4-8b9c0e6f2a37"}`))
	comments := commentsOn("CP-12")
	if len(comments) != 1 || !strings.Contains(stringOf(comments[0]), "Refinement questions from Rotor") || !strings.Contains(stringOf(comments[0]), "Which formats") {
		t.Fatalf("after the first run CP-12 has the comments %v", comments)
	}
	offeredShippedSkill(t, llm, 0, "requirements-engineering")
	questions, _ := comments[0].(map[string]any)["id"].(string)
	if outcome, reason, _ := firing(t, h, triggerID, commented(questions)); outcome != "skipped" || !strings.Contains(reason, "own write") {
		t.Errorf("the questions' own event ended %q (%q), want skipped as the workflow's own write", outcome, reason)
	}

	// Questions story-to-requirements asks are Rotor's too: the run they start comments nothing.
	ran(commented(comment("**Questions from Rotor**\n\n1. Which formats?")))
	if comments = commentsOn("CP-12"); len(comments) != 2 {
		t.Fatalf("story-to-requirements' questions were taken for an answer: %v", comments)
	}

	// The reporter answers: the workflow comments the refined story, with criteria a test can decide.
	ran(commented(comment("CSV only, for now.")))
	comments = commentsOn("CP-12")
	refined := stringOf(comments[len(comments)-1])
	for _, want := range []string{"Refined story from Rotor", "As a reviewer", "1. Given a project with requirements, when the reviewer exports it as CSV",
		"Out of scope", "Importing the file again", "Assumptions to confirm"} {
		if len(comments) != 4 || !strings.Contains(refined, want) {
			t.Fatalf("after the answer CP-12 has the comments %v, want the refined story with %q", comments, want)
		}
	}
	if strings.Contains(refined, "Split proposal") {
		t.Errorf("a story that fits one iteration got a split proposal: %s", refined)
	}

	// A bug starts a run that leaves it alone.
	ran(armatureEvent(t, armatureID.String(), secret, "issue.created", `{"issueId":"i2","key":"CP-13","summary":"Export crashes","actorId":"a"}`))
	if bugs := commentsOn("CP-13"); len(bugs) != 0 {
		t.Errorf("the bug got comments %v", bugs)
	}

	// The questions and the refined story are the only writes, each unattended and audited.
	var audited int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'trigger.unattended_write' AND target_id = $1`, triggerID).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 2 {
		t.Errorf("the audit log holds %d unattended writes, want 2", audited)
	}
}
