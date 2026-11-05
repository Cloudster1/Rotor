//go:build integration && suite

package test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// #212 end to end: a story filed in the real Armature reaches Rotor by Armature's own webhook, the run asks on the
// ticket, and the reporter's answer becomes a refined story commented on it.
func TestSuiteAStoryInArmatureIsRefinedOnItsTicket(t *testing.T) {
	h := newHarness(t)
	base, _ := armature(t)
	exciterBase, _ := exciter(t)
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		analyseTurn(map[string]any{"verdict": "questions", "questions": []string{"Which formats must the export offer?"}}),
		analyseTurn(map[string]any{"verdict": "refined", "questions": []any{}, "refinement": map[string]any{
			"story": "As a reviewer, I want to export a project's requirements, so that I can review them offline.",
			"criteria": []map[string]any{
				{"given": "a project with requirements", "when": "the reviewer exports it as CSV", "then": "the file holds one row per requirement"}},
			"outOfScope": []string{"Importing the file again"}, "assumptions": []any{}, "split": []any{}}}),
	}})

	// alice owns the trigger, connected to Armature, whose webhook tells Rotor about new stories and comments.
	alice := suiteBrowser(t, "alice", exciterBase)
	alice.signInToArmature(t, base, "alice")
	provider := addProvider(t, alice, llm, key)
	armatureID := addArmature(t, alice, base)
	alice.connectToArmature(t, base, armatureID, "alice")
	armatureWebhook(t, alice, base, armatureID, []string{"issue.created", "comment.added"})

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "story-refinement.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, alice, source)
	trigger := addTrigger(t, alice, workflowID, map[string]any{"name": "Refine stories", "kind": "event", "inputs": map[string]any{},
		"allowUnattended": refinementCalls, "event": map[string]any{"toolId": armatureID, "topics": []string{"issue.created", "comment.added"},
			"projects": []string{armatureProject}, "conditions": []map[string]any{{"path": "payload.internal", "op": "notEquals", "value": true}}}},
		http.StatusCreated)
	triggerID := trigger["id"].(string)

	// bob files a story: Armature tells Rotor, and the run asks on the ticket; its own comment starts nothing.
	bob := suiteBrowser(t, "bob", exciterBase)
	bob.signInToArmature(t, base, "bob")
	story := fileIssue(t, bob, base, "Story", "Export requirements "+uuid.NewString()[:8], "As a reviewer I want to export them.")
	storyRun(t, h, alice, triggerID, func(topic string, payload map[string]any) bool {
		return topic == "issue.created" && eventIssue(payload) == story
	})
	comments := issueComments(t, bob, base, story)
	if len(comments) != 1 || !strings.Contains(stringOf(comments[0]["body"]), "Refinement questions from Rotor") ||
		!strings.Contains(stringOf(comments[0]["body"]), "Which formats") {
		t.Fatalf("after the first run %s has the comments %v", story, comments)
	}
	waitOwnWritesSkipped(t, h, triggerID, 1)

	// bob answers in Armature: the next run comments the refined story.
	status, answered := bob.atArmature(t, base, http.MethodPost, "/issues/"+story+"/comments", map[string]any{"bodyMarkdown": "CSV only, for now."})
	comment, _ := answered["comment"].(map[string]any)
	answerID := stringOf(comment["id"])
	if status != http.StatusCreated || answerID == "" {
		t.Fatalf("bob answering on %s = %d %v", story, status, answered)
	}
	storyRun(t, h, alice, triggerID, func(topic string, payload map[string]any) bool {
		return topic == "comment.added" && payload["commentId"] == answerID
	})
	comments = issueComments(t, bob, base, story)
	refined := stringOf(comments[len(comments)-1]["body"])
	for _, want := range []string{"Refined story from Rotor", "As a reviewer", "when the reviewer exports it as CSV", "Importing the file again"} {
		if len(comments) != 3 || !strings.Contains(refined, want) {
			t.Fatalf("after the answer %s has the comments %v, want the refined story with %q", story, comments, want)
		}
	}
	waitOwnWritesSkipped(t, h, triggerID, 2)
}
