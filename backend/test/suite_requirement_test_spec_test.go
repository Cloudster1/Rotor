//go:build integration && suite

package test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// #213 against the real Exciter: what changed is read, the reviewed cases filed under the test section and linked
// verifies, the suspect link settled, the specification commented on the feature and the cases sent to review.
func TestSuiteAFeaturesTestCasesAreDesignedReviewedAndFiledInExciter(t *testing.T) {
	exciterBase, _ := exciter(t)
	alice := suiteBrowser(t, "alice", exciterBase)
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": exciterBase})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	exciterID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+exciterID, nil) })
	alice.connectExciter(t, exciterBase, exciterID, "alice")
	project := exciterProject(t, alice, exciterBase)
	approvedByAlice(t, alice, exciterBase, project)
	keys := seedRealFeature(t, alice, exciterBase, project)
	feature, pay, refund, section, existing := keys[0], keys[1], keys[2], keys[3], keys[4]

	scripts := map[string][]map[string]any{
		"design": {submit(map[string]any{"specification": "Paying and refunding at checkout, by boundary values.", "cases": []map[string]any{
			{"key": existing, "title": "Pays a cart by card", "preconditions": "A cart of 30 euros.",
				"steps": []map[string]any{{"action": "Pay the cart by card.", "expected": "30 euros are paid."}}, "verifies": []string{pay}},
			{"title": "Refunds a payment", "preconditions": "A paid cart.",
				"steps": []map[string]any{{"action": "Refund 10 euros.", "expected": "10 euros are refunded."}}, "verifies": []string{refund}},
		}})},
		"design-review": {submit(map[string]any{"verdict": "approve", "findings": []string{}})},
	}
	providers := map[string]string{}
	for agent, script := range scripts {
		key := "llm-key-" + uuid.NewString()
		providers[agent] = addProvider(t, alice, newToolStub(t, map[string]any{"llmKey": key, "llmScript": script}), key)["name"].(string)
	}
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "requirement-to-test-spec.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "name: requirement-to-test-spec\n", "name: "+uniqueName("requirement-to-test-spec")+"\n", 1)
	workflowID := createWorkflow(t, alice, withModels(source, providers))
	run := startRun(t, alice, workflowID, map[string]any{"exciterProject": project, "featureKey": feature, "testSection": section}, http.StatusCreated)
	done := waitRun(t, alice, run["id"].(string), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["error"])
	}

	filed := done["output"].(map[string]any)["file"].(map[string]any)
	if cases, _ := filed["cases"].([]any); len(cases) != 2 || filed["cleared"] != 1.0 {
		t.Fatalf("the run filed %v, want two cases and one suspect link cleared", filed)
	}
	_, sec := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+section, nil)
	for _, f := range filed["cases"].([]any) {
		key := stringOf(f.(map[string]any)["key"])
		_, got := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+key, nil)
		if r := got["requirement"].(map[string]any); r["testCase"] == nil || r["parentId"] != sec["requirement"].(map[string]any)["id"] {
			t.Errorf("%s = %v, want a test case under %s", key, r, section)
		}
		if _, reviews := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+key+"/reviews", nil); reviews["total"] == float64(0) {
			t.Errorf("nobody was asked to review %s: %v", key, reviews)
		}
	}
	_, links := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+pay+"/links", nil)
	if read := stringOf(links); !strings.Contains(read, "verifies") || (strings.Contains(read, "reviewed=") && strings.Contains(read, "current=")) {
		t.Errorf("%s is linked as %v, want verifies and no suspect end left", pay, links)
	}
	if _, comments := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+feature+"/comments", nil); !strings.Contains(stringOf(comments), "Test specification from Rotor") {
		t.Errorf("%s has the comments %v, want the test specification", feature, comments)
	}
}
