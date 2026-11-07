//go:build integration

package test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The weekly digest as it ships (#271): its schedule fires as the owner, with no model anywhere, and the digest
// issue gets one comment counting the week's filed and finished issues the owner can see, and no older ones.
func TestAWeeklyDigestSumsUpTheProjectWithoutAModel(t *testing.T) {
	h := newHarness(t)
	now := time.Now().UTC()
	ago := func(days int) string { return now.Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339) }
	issue := func(kind, summary string, created int, more ...string) map[string]any {
		out := map[string]any{"type": kind, "summary": summary, "readers": []string{"bob"}, "createdAt": ago(created)}
		for i := 0; i+1 < len(more); i += 2 {
			out[more[i]] = more[i+1]
		}
		return out
	}
	armature := newToolStub(t, map[string]any{"product": "armature", "issues": map[string]any{
		"CP-1": issue("Task", "Weekly digest", 60),
		"CP-2": issue("Bug", "Export crashes", 2),
		"CP-3": issue("Story", "Export as PDF", 3, "category", "done", "resolvedAt", ago(1)),
		"CP-4": issue("Task", "Tidy the exports", 40, "category", "done", "resolvedAt", ago(2)),
		"CP-5": issue("Story", "Import from CSV", 40, "category", "in_progress"),
		"CP-6": issue("Task", "Finished last month", 60, "category", "done", "resolvedAt", ago(20)),
		"CP-7": map[string]any{"type": "Bug", "summary": "Hidden from bob", "readers": []string{"carol"}, "createdAt": ago(1)},
	}})
	bob := signedIn(t, "bob")
	connectArmature(t, armature, bob)

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "weekly-digest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "name: weekly-digest\n", "name: "+uniqueName("weekly-digest")+"\n", 1)
	workflowID := createWorkflow(t, bob, source)
	trigger := addTrigger(t, bob, workflowID, map[string]any{"name": "Monday digest", "kind": "schedule",
		"schedule":        map[string]any{"cron": "0 8 * * 1", "timeZone": "Europe/Berlin", "noOverlap": true},
		"inputs":          map[string]any{"projectKey": "CP", "digestIssue": "CP-1"},
		"allowUnattended": []string{"armature-comment.add_comment"}}, http.StatusCreated)
	id := uuid.MustParse(trigger["id"].(string))

	fireAt(t, h, id, nextRun(t, trigger).Add(time.Second))
	fire := lastFire(t, bob, id.String(), "started", "refused", "failed")["lastFire"].(map[string]any)
	if fire["outcome"] != "started" {
		t.Fatalf("the firing ended %v", fire)
	}
	done := waitRun(t, bob, fire["runId"].(string), "succeeded", "failed", "cancelled")
	if done["status"] != "succeeded" {
		t.Fatalf("the digest ended %v: %v", done["status"], done["error"])
	}
	if post := done["output"].(map[string]any)["post"].(map[string]any); post["filed"] != float64(2) || post["finished"] != float64(2) {
		t.Errorf("the digest counted %v, want 2 filed and 2 finished", post)
	}

	comments, _ := armature.Records()["comments"].(map[string]any)["CP-1"].([]any)
	if len(comments) != 1 {
		t.Fatalf("the digest issue has the comments %v, want one", comments)
	}
	body := stringOf(comments[0])
	for _, want := range []string{"Weekly digest from Rotor", "**Filed: 2** (1 Bug, 1 Story)", "CP-2 Export crashes",
		"**Finished: 2** (1 Story, 1 Task)", "CP-4 Tidy the exports", "(UTC)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the digest lacks %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{"CP-5", "CP-6", "CP-7", "Hidden"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the digest names %s, which it should not: %s", unwanted, body)
		}
	}
}
