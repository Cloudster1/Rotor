//go:build integration && suite

package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// connectExciterToArmature gives Exciter alice's own Armature token, which Exciter traces requirements to issues
// with; make suite-up's seed connected the two installations.
func connectExciterToArmature(t *testing.T, alice *browser, exciterBase, base string) {
	t.Helper()
	status, made := alice.atArmature(t, base, http.MethodPost, "/tokens", map[string]any{"name": "Exciter " + uuid.NewString()[:8]})
	token, _ := made["token"].(map[string]any)
	secret, _ := token["secret"].(string)
	if status != http.StatusCreated || secret == "" {
		t.Fatalf("making alice an Armature token = %d %v", status, made)
	}
	t.Cleanup(func() { alice.atArmature(t, base, http.MethodDelete, "/tokens/"+stringOf(token["id"]), nil) })
	if status, answer := alice.atExciter(t, exciterBase, http.MethodPut, "/armature/account/token", map[string]any{"token": secret}); status != http.StatusOK {
		t.Fatalf("giving Exciter alice's Armature token = %d %v; make suite-up connects Exciter to Armature", status, answer)
	}
}

// approvedByAlice gives the Exciter project an approval rule in which alice approves, since Exciter asks for no
// review in a project without one.
func approvedByAlice(t *testing.T, alice *browser, exciterBase, project string) {
	t.Helper()
	_, listed := alice.atExciter(t, exciterBase, http.MethodGet, "/users", nil)
	members, _ := listed["members"].([]any)
	aliceID := ""
	for _, m := range members {
		if member, _ := m.(map[string]any); member["email"] == "alice@suite.test" {
			aliceID, _ = member["userId"].(string)
		}
	}
	rule := map[string]any{"required": 1, "authorMayApprove": true, "approvers": []map[string]any{{"subjectType": "user", "userId": aliceID}}}
	if status, answer := alice.atExciter(t, exciterBase, http.MethodPut, "/projects/"+project+"/approval-rules", rule); status != http.StatusOK {
		t.Fatalf("giving %s an approval rule = %d %v", project, status, answer)
	}
}

// armatureWebhook points a webhook of Armature's at the tool's hook in Rotor and gives Rotor its secret; its address
// names the tool, so the test makes it once the tool is added.
func armatureWebhook(t *testing.T, alice *browser, base, toolID string, topics []string) {
	t.Helper()
	admin := armatureAdmin(t, base)
	status, made := admin.atArmature(t, base, http.MethodPost, "/webhooks", map[string]any{"name": "Rotor " + uuid.NewString()[:8],
		"url": shownHookURL(t, alice, toolID), "topics": topics, "version": 1, "enabled": true})
	webhook, _ := made["webhook"].(map[string]any)
	secret, _ := webhook["secret"].(string)
	if status != http.StatusCreated || secret == "" {
		t.Fatalf("pointing Armature's webhook at Rotor = %d %v", status, made)
	}
	t.Cleanup(func() { admin.atArmature(t, base, http.MethodDelete, "/webhooks/"+stringOf(webhook["id"]), nil) })
	if status, hook := alice.api(http.MethodPut, "/tools/"+toolID+"/hook", map[string]any{"secret": secret}); status != http.StatusOK {
		t.Fatalf("giving Rotor Armature's secret = %d %v", status, hook)
	}
}

// storyRun waits until the trigger started a run for an event that matches, and answers it once it succeeded; a
// firing keeps no event once it ends (#41), so the run's inputs say which event it was.
func storyRun(t *testing.T, h *harness, owner *browser, triggerID string, matches func(topic string, payload map[string]any) bool) string {
	t.Helper()
	deadline := time.Now().Add(hookWait)
	for time.Now().Before(deadline) {
		var runs []string
		rows, err := h.super.Query(context.Background(), `SELECT run_id::text FROM trigger_fire WHERE trigger_id = $1 AND run_id IS NOT NULL`, triggerID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				runs = append(runs, id)
			}
		}
		rows.Close()
		for _, run := range runs {
			_, got := owner.api(http.MethodGet, "/runs/"+run, nil)
			event, _ := got["inputs"].(map[string]any)["event"].(map[string]any)
			payload, _ := event["payload"].(map[string]any)
			if !matches(stringOf(event["topic"]), payload) {
				continue
			}
			done := waitRun(t, owner, run, "succeeded", "failed")
			if done["status"] != "succeeded" {
				t.Fatalf("the run for %v ended %v: %v; events %+v", payload, done["status"], done, h.events(t, uuid.MustParse(run)))
			}
			return run
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("no run of trigger %s for the event within %s", triggerID, hookWait)
	return ""
}

// eventIssue is the issue an Armature event is about, where Rotor's triggers read it (hooks.Subject).
func eventIssue(payload map[string]any) string {
	issue, _ := payload["issue"].(map[string]any)
	for _, key := range []any{payload["key"], issue["key"], payload["issueKey"]} {
		if k, ok := key.(string); ok && k != "" {
			return k
		}
	}
	return ""
}

// waitOwnWritesSkipped waits until the trigger skipped n events as the workflow's own writes (#127).
func waitOwnWritesSkipped(t *testing.T, h *harness, triggerID string, n int) {
	t.Helper()
	deadline := time.Now().Add(hookWait)
	var skipped int
	for time.Now().Before(deadline) {
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM trigger_fire WHERE trigger_id = $1
			AND outcome = 'skipped' AND reason LIKE '%own write%'`, triggerID).Scan(&skipped); err != nil {
			t.Fatal(err)
		}
		if skipped >= n {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("the trigger skipped %d events as its own writes within %s, want %d", skipped, hookWait, n)
}

// #130 end to end: a story filed in the real Armature reaches Rotor by Armature's own webhook, the run asks on the
// ticket, and the reporter's answer becomes requirements in the real Exciter, traced to the story and up for review.
func TestSuiteAStoryInArmatureBecomesRequirementsInExciter(t *testing.T) {
	h := newHarness(t)
	base, _ := armature(t)
	exciterBase, _ := exciter(t)
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		analyseTurn(map[string]any{"verdict": "questions", "questions": []string{"Which formats must the export offer?"}, "changes": []any{}}),
		analyseTurn(map[string]any{"verdict": "ready", "questions": []any{}, "changes": []map[string]any{
			{"action": "create", "title": "Export as CSV", "statementMarkdown": "The system shall export a project's requirements as CSV."},
			{"action": "create", "title": "Export as PDF", "statementMarkdown": "The system shall export a project's requirements as PDF."}}}),
	}})

	// alice owns the trigger, connected to both tools; Exciter traces to Armature with her own Armature token.
	alice := suiteBrowser(t, "alice", exciterBase)
	alice.signInToArmature(t, base, "alice")
	provider := addProvider(t, alice, llm, key)
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": exciterBase})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	exciterID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+exciterID, nil) })
	alice.connectExciter(t, exciterBase, exciterID, "alice")
	project := exciterProject(t, alice, exciterBase)
	approvedByAlice(t, alice, exciterBase, project)
	armatureID := addArmature(t, alice, base)
	alice.connectToArmature(t, base, armatureID, "alice")
	connectExciterToArmature(t, alice, exciterBase, base)
	armatureWebhook(t, alice, base, armatureID, []string{"issue.created", "comment.added"})

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "story-to-requirements.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, alice, source)
	trigger := addTrigger(t, alice, workflowID, map[string]any{"name": "Stories", "kind": "event", "inputs": map[string]any{"exciterProject": project},
		"allowUnattended": storyCalls, "event": map[string]any{"toolId": armatureID, "topics": []string{"issue.created", "comment.added"},
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
	if len(comments) != 1 || !strings.Contains(stringOf(comments[0]["body"]), "Questions from Rotor") ||
		!strings.Contains(stringOf(comments[0]["body"]), "Which formats") || !strings.Contains(stringOf(comments[0]["author"]), "alice@suite.test") {
		t.Fatalf("after the first run %s has the comments %v", story, comments)
	}
	waitOwnWritesSkipped(t, h, triggerID, 1)

	// bob answers in Armature: the next run files the requirements in Exciter and says so on the ticket.
	status, answered := bob.atArmature(t, base, http.MethodPost, "/issues/"+story+"/comments", map[string]any{"bodyMarkdown": "CSV and PDF."})
	comment, _ := answered["comment"].(map[string]any)
	answerID := stringOf(comment["id"])
	if status != http.StatusCreated || answerID == "" {
		t.Fatalf("bob answering on %s = %d %v", story, status, answered)
	}
	filed := storyRun(t, h, alice, triggerID, func(topic string, payload map[string]any) bool {
		return topic == "comment.added" && payload["commentId"] == answerID
	})
	_, done := alice.api(http.MethodGet, "/runs/"+filed, nil)
	output, _ := done["output"].(map[string]any)
	fileNode, _ := output["file"].(map[string]any)
	written, _ := fileNode["requirements"].([]any)
	if len(written) != 2 {
		t.Fatalf("the answer's run wrote %v", done["output"])
	}
	for _, w := range written {
		reqKey := stringOf(w.(map[string]any)["key"])
		_, links := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+reqKey+"/armature-links", nil)
		if !strings.Contains(stringOf(links), story) || !strings.Contains(stringOf(links), "implements") {
			t.Errorf("%s is traced to %v, want %s as implements", reqKey, links, story)
		}
		if _, reviews := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+reqKey+"/reviews", nil); reviews["total"] == float64(0) {
			t.Errorf("nobody was asked to review %s: %v", reqKey, reviews)
		}
	}
	if _, grid := alice.atExciter(t, exciterBase, http.MethodGet, "/projects/"+project+"/requirements", nil); grid["total"] != float64(2) {
		t.Errorf("the Exciter project holds %v requirements, want the two the story asked for", grid["total"])
	}
	comments = issueComments(t, bob, base, story)
	handover := stringOf(comments[len(comments)-1]["body"])
	if len(comments) != 3 || !strings.Contains(handover, "Requirements from Rotor") || !strings.Contains(handover, stringOf(written[0].(map[string]any)["key"])) {
		t.Fatalf("after the answer %s has the comments %v", story, comments)
	}
	waitOwnWritesSkipped(t, h, triggerID, 2)
}
