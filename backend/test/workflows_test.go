//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// concurrentSaves is how many people save the same version at once in the numbering test.
const concurrentSaves = 5

// approvalOnly is a workflow that binds no tool, so the shared stack's tools never decide whether it saves.
func approvalOnly(name, message string) string {
	return fmt.Sprintf("apiVersion: rotor.dev/v1\nkind: Workflow\nmetadata: {name: %s}\nlimits: {maxSteps: 3, timeout: 1m}\n"+
		"entry: ask\nnodes:\n  - {id: ask, type: approval, message: %q}\n", name, message)
}

// createWorkflow saves a new workflow as b and answers its id.
func createWorkflow(t *testing.T, b *browser, source string) string {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows", map[string]any{"specYAML": source, "comment": "first"})
	if status != http.StatusCreated {
		t.Fatalf("creating a workflow = %d %v", status, answer)
	}
	wf, _ := answer["workflow"].(map[string]any)
	id, _ := wf["id"].(string)
	return id
}

func uniqueName(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
}

func TestAWorkflowKeepsNumberedVersionsAndTheirDiff(t *testing.T) {
	alice := newBrowser(t)
	alice.signIn("alice")
	name := uniqueName("versions")
	id := createWorkflow(t, alice, approvalOnly(name, "Go on?"))

	status, answer := alice.api(http.MethodPut, "/workflows/"+id, map[string]any{"specYAML": approvalOnly(name, "Really go on?"), "baseVersion": 1})
	if version, _ := answer["version"].(map[string]any); status != http.StatusOK || version["number"] != float64(2) {
		t.Fatalf("saving version 2 = %d %v", status, answer)
	}
	if status, answer := alice.api(http.MethodPut, "/workflows/"+id, map[string]any{"specYAML": approvalOnly(name, "Stale"), "baseVersion": 1}); status != http.StatusConflict {
		t.Errorf("saving on a stale version = %d %v, want 409", status, answer)
	}
	_, answer = alice.api(http.MethodGet, "/workflows/"+id+"/versions", nil)
	if versions, _ := answer["versions"].([]any); len(versions) != 2 || versions[0].(map[string]any)["number"] != float64(2) {
		t.Errorf("the versions = %v", answer)
	}
	_, answer = alice.api(http.MethodGet, "/workflows/"+id+"/versions/1", nil)
	if !strings.Contains(fmt.Sprint(answer["specYAML"]), "Go on?") || strings.Contains(fmt.Sprint(answer["specYAML"]), "Really") {
		t.Errorf("version 1 changed: %v", answer["specYAML"])
	}
	_, answer = alice.api(http.MethodGet, "/workflows/"+id+"/diff?from=1&to=2", nil)
	if hunks, _ := answer["hunks"].([]any); len(hunks) != 1 || !strings.Contains(fmt.Sprint(hunks), "Really go on?") {
		t.Errorf("the diff = %v", answer)
	}
}

func TestSavesOfOneVersionAtOnceNumberWithoutGaps(t *testing.T) {
	alice := newBrowser(t)
	alice.signIn("alice")
	name := uniqueName("race")
	id := createWorkflow(t, alice, approvalOnly(name, "Go on?"))
	statuses := make([]int, concurrentSaves)
	var wg sync.WaitGroup
	for i := range concurrentSaves {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], _ = alice.api(http.MethodPut, "/workflows/"+id,
				map[string]any{"specYAML": approvalOnly(name, fmt.Sprintf("Attempt %d?", i)), "baseVersion": 1})
		}()
	}
	wg.Wait()
	saved := 0
	for _, s := range statuses {
		switch s {
		case http.StatusOK:
			saved++
		case http.StatusConflict:
		default:
			t.Errorf("a save answered %d", s)
		}
	}
	_, answer := alice.api(http.MethodGet, "/workflows/"+id+"/versions", nil)
	if versions, _ := answer["versions"].([]any); saved != 1 || len(versions) != 2 {
		t.Errorf("%d of %d saves of version 1 succeeded, leaving %d versions; want one, and two", saved, concurrentSaves, len(versions))
	}
}

func TestSharingDecidesWhoSeesAWorkflowAndOnlyTheOwnerChangesIt(t *testing.T) {
	alice, bob, carol := newBrowser(t), newBrowser(t), newBrowser(t)
	alice.signIn("alice")
	bob.signIn("bob")
	carol.signIn("carol")
	name := uniqueName("shared")
	id := createWorkflow(t, alice, approvalOnly(name, "Go on?"))
	sees := func(b *browser) bool {
		status, _ := b.api(http.MethodGet, "/workflows/"+id, nil)
		return status == http.StatusOK
	}
	share := func(body map[string]any) {
		t.Helper()
		if status, answer := alice.api(http.MethodPut, "/workflows/"+id+"/sharing", body); status != http.StatusOK {
			t.Fatalf("sharing %v = %d %v", body, status, answer)
		}
	}
	if sees(bob) || sees(carol) {
		t.Fatal("a private workflow is seen by others")
	}
	// bob is in developers, carol is not.
	share(map[string]any{"sharing": "groups", "groups": []string{"developers"}})
	if !sees(bob) || sees(carol) {
		t.Errorf("shared with developers: bob sees %v, carol sees %v", sees(bob), sees(carol))
	}
	if status, _ := bob.api(http.MethodPut, "/workflows/"+id, map[string]any{"specYAML": approvalOnly(name, "Mine now"), "baseVersion": 1}); status != http.StatusForbidden {
		t.Errorf("bob saving alice's workflow = %d, want 403", status)
	}
	if status, _ := bob.api(http.MethodPut, "/workflows/"+id+"/sharing", map[string]any{"sharing": "everyone"}); status != http.StatusForbidden {
		t.Errorf("bob sharing alice's workflow = %d, want 403", status)
	}
	if status, _ := bob.api(http.MethodDelete, "/workflows/"+id, nil); status != http.StatusForbidden {
		t.Errorf("bob archiving alice's workflow = %d, want 403", status)
	}
	share(map[string]any{"sharing": "everyone"})
	if !sees(carol) {
		t.Error("carol does not see a workflow shared with everyone")
	}
	share(map[string]any{"sharing": "private"})
	if sees(bob) {
		t.Error("bob still sees a workflow made private again")
	}

	if status, _ := alice.api(http.MethodDelete, "/workflows/"+id, nil); status != http.StatusNoContent {
		t.Fatalf("archiving = %d", status)
	}
	_, answer := alice.api(http.MethodGet, "/workflows", nil)
	if strings.Contains(fmt.Sprint(answer), id) {
		t.Error("an archived workflow is listed")
	}
	if _, answer := alice.api(http.MethodGet, "/workflows?archived=true", nil); !strings.Contains(fmt.Sprint(answer), id) {
		t.Error("an archived workflow is not listed among the archived")
	}
	if status, _ := alice.api(http.MethodPut, "/workflows/"+id, map[string]any{"specYAML": approvalOnly(name, "After"), "baseVersion": 1}); status != http.StatusConflict {
		t.Errorf("saving an archived workflow = %d, want 409", status)
	}
}

func TestAWorkflowWithProblemsIsRefusedNamingThem(t *testing.T) {
	alice := newBrowser(t)
	alice.signIn("alice")
	broken := strings.Replace(approvalOnly(uniqueName("broken"), "Go on?"), "limits: {maxSteps: 3, timeout: 1m}\n", "", 1)
	status, answer := alice.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": broken})
	problems, _ := answer["problems"].([]any)
	if status != http.StatusOK || answer["valid"] != false || len(problems) == 0 || !strings.Contains(fmt.Sprint(problems), "limits") {
		t.Fatalf("validating = %d %v", status, answer)
	}
	if status, answer := alice.api(http.MethodPost, "/workflows", map[string]any{"specYAML": broken}); status != http.StatusUnprocessableEntity {
		t.Errorf("saving a broken workflow = %d %v", status, answer)
	}
	if status, answer := alice.api(http.MethodGet, "/schemas/workflow.v1.json", nil); status != http.StatusOK || answer["$id"] == nil {
		t.Errorf("the schema = %d %v", status, answer)
	}
}
