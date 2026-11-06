//go:build integration && suite

package test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// #270 end to end: an issue filed in the real Armature reaches Rotor by its webhook, and the run adds a label in use
// through bulk_edit_issues and comments why; the name not in use is not coined.
func TestSuiteANewIssueInArmatureIsLabelled(t *testing.T) {
	h := newHarness(t)
	base, _ := armature(t)
	exciterBase, _ := exciter(t)
	label, unused := "triage-"+uuid.NewString()[:8], "unused-"+uuid.NewString()[:8]
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		analyseTurn(map[string]any{"verdict": "label", "labels": []string{strings.ToUpper(label), unused},
			"reason": "The issue is about exporting."}),
	}})

	// alice owns the trigger, connected to Armature, whose webhook tells Rotor about new issues.
	alice := suiteBrowser(t, "alice", exciterBase)
	alice.signInToArmature(t, base, "alice")
	provider := addProvider(t, alice, llm, key)
	armatureID := addArmature(t, alice, base)
	alice.connectToArmature(t, base, armatureID, "alice")
	armatureWebhook(t, alice, base, armatureID, []string{"issue.created"})

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "issue-triage.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, alice, source)
	trigger := addTrigger(t, alice, workflowID, map[string]any{"name": "Triage new issues", "kind": "event", "inputs": map[string]any{},
		"allowUnattended": triageCalls, "event": map[string]any{"toolId": armatureID, "topics": []string{"issue.created"},
			"projects": []string{armatureProject}}}, http.StatusCreated)

	// bob makes the label the team uses, then files an issue: the run labels it and says why.
	bob := suiteBrowser(t, "bob", exciterBase)
	bob.signInToArmature(t, base, "bob")
	if status, made := bob.atArmature(t, base, http.MethodPost, "/labels", map[string]any{"name": label}); status != http.StatusCreated {
		t.Fatalf("bob making the label %s = %d %v", label, status, made)
	}
	issue := fileIssue(t, bob, base, "Story", "Export crashes "+uuid.NewString()[:8], "Exporting a project as CSV fails.")
	storyRun(t, h, alice, trigger["id"].(string), func(topic string, payload map[string]any) bool {
		return topic == "issue.created" && eventIssue(payload) == issue
	})
	_, read := bob.atArmature(t, base, http.MethodGet, "/issues/"+issue, nil)
	labels, _ := read["issue"].(map[string]any)["labels"].([]any)
	if len(labels) != 1 || labels[0].(map[string]any)["name"] != label {
		t.Errorf("%s carries the labels %v, want %s alone", issue, labels, label)
	}
	comments := issueComments(t, bob, base, issue)
	if len(comments) != 1 || !strings.Contains(stringOf(comments[0]["body"]), "Labels from Rotor") {
		t.Errorf("%s has the comments %v, want Rotor's labels comment", issue, comments)
	}
	if _, found := bob.atArmature(t, base, http.MethodGet, "/labels?q="+url.QueryEscape(unused), nil); found["total"] != float64(0) {
		t.Errorf("the name not in use was coined: %v", found)
	}
}
