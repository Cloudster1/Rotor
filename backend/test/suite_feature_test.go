//go:build integration && suite

package test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"
)

// designHalf are the nodes of the feature workflow that reach no repository, which the suite can run: its Armature
// starts without Gitea, so no repository is there to check out.
var designHalf = []string{"gather", "design", "design-review", "design-verdict", "design-decide", "file-tests"}

// testDesignHalf cuts the feature workflow to its test design, with the bindings those nodes use and each agent on
// the stub provider named for it.
func testDesignHalf(t *testing.T, providers map[string]string) string {
	t.Helper()
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "implement-feature.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(withModels(string(example), providers)), &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	keep := func(seq *yaml.Node, ok func(*yaml.Node) bool) {
		var kept []*yaml.Node
		for _, item := range seq.Content {
			if ok(item) {
				kept = append(kept, item)
			}
		}
		seq.Content = kept
	}
	field := func(m *yaml.Node, key string) *yaml.Node {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == key {
				return m.Content[i+1]
			}
		}
		t.Fatalf("the example has no %s", key)
		return nil
	}
	keep(field(root, "nodes"), func(n *yaml.Node) bool { return slices.Contains(designHalf, field(n, "id").Value) })
	keep(field(root, "edges"), func(e *yaml.Node) bool {
		return slices.Contains(designHalf, field(e, "from").Value) && slices.Contains(designHalf, field(e, "to").Value)
	})
	tools := field(root, "tools")
	var bindings []*yaml.Node
	for i := 0; i+1 < len(tools.Content); i += 2 {
		if slices.Contains([]string{"exciter-read", "exciter-write", "armature-read"}, tools.Content[i].Value) {
			bindings = append(bindings, tools.Content[i], tools.Content[i+1])
		}
	}
	tools.Content = bindings
	field(field(root, "metadata"), "name").Value = uniqueName("feature-tests")
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// seedRealFeature writes a feature in the real Exciter: a section of two requirements, a section for tests, and a
// test case of the first, which changed since it was linked; it answers the keys in that order.
func seedRealFeature(t *testing.T, alice *browser, base, project string) []string {
	t.Helper()
	_, listed := alice.atExciter(t, base, http.MethodGet, "/projects/"+project+"/types", nil)
	testType := ""
	for _, ty := range listed["types"].([]any) {
		if ty.(map[string]any)["role"] == "testCase" {
			testType = stringOf(ty.(map[string]any)["id"])
		}
	}
	if testType == "" {
		status, made := alice.atExciter(t, base, http.MethodPost, "/projects/"+project+"/types", map[string]any{"name": "Test case", "role": "testCase",
			"icon": "flask", "colour": "#4f6bed"})
		if status != http.StatusCreated {
			t.Fatalf("giving %s a test case type = %d %v", project, status, made)
		}
		testType = stringOf(made["type"].(map[string]any)["id"])
	}
	var keys []string
	create := func(body map[string]any) map[string]any {
		status, made := alice.atExciter(t, base, http.MethodPost, "/projects/"+project+"/requirements", body)
		if status != http.StatusCreated {
			t.Fatalf("writing %v in %s = %d %v", body["title"], project, status, made)
		}
		r := made["requirement"].(map[string]any)
		keys = append(keys, stringOf(r["key"]))
		return r
	}
	under := func(r map[string]any) map[string]any { return map[string]any{"parentId": r["id"], "afterId": nil} }
	feature := create(map[string]any{"title": "Checkout", "kind": "section"})
	pay := create(map[string]any{"title": "Pay by card", "statementMarkdown": "The shop shall take payment by card.", "place": under(feature)})
	create(map[string]any{"title": "Refund", "statementMarkdown": "The shop shall refund a payment.", "place": under(feature)})
	tests := create(map[string]any{"title": "Checkout tests", "kind": "section"})
	tc := create(map[string]any{"title": "Pays by card", "typeId": testType, "place": under(tests), "testCase": map[string]any{
		"preconditions": nil, "parameters": []any{}, "steps": []map[string]any{{"actionMarkdown": "Pay.", "expectedMarkdown": "Paid."}}}})
	if status, linked := alice.atExciter(t, base, http.MethodPost, "/requirements/"+stringOf(tc["key"])+"/links",
		map[string]any{"otherKey": pay["key"], "typeKey": "verifies"}); status != http.StatusCreated {
		t.Fatalf("linking the test case = %d %v", status, linked)
	}
	if status, changed := alice.atExciter(t, base, http.MethodPatch, "/requirements/"+stringOf(pay["key"]), map[string]any{
		"baseRevision": pay["revision"], "statementMarkdown": "The shop shall take payment by card, in euros."}); status != http.StatusOK {
		t.Fatalf("changing %v = %d %v", pay["key"], status, changed)
	}
	return keys
}

// #140's test design against the real tools: what changed is read and the cases filed under the test section, linked
// verifies. The repository half needs a Gitea the suite's Armature does not start yet; the stub's git runs it.
func TestSuiteAFeaturesTestCasesAreDesignedAndFiledInExciter(t *testing.T) {
	base, _ := armature(t)
	exciterBase, _ := exciter(t)
	alice := suiteBrowser(t, "alice", exciterBase)
	alice.signInToArmature(t, base, "alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": exciterBase})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	exciterID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+exciterID, nil) })
	alice.connectExciter(t, exciterBase, exciterID, "alice")
	armatureID := addArmature(t, alice, base)
	alice.connectToArmature(t, base, armatureID, "alice")
	project := exciterProject(t, alice, exciterBase)
	keys := seedRealFeature(t, alice, exciterBase, project)
	feature, pay, refund, section, existing := keys[0], keys[1], keys[2], keys[3], keys[4]
	issue := fileIssue(t, alice, base, "Story", "Pay and refund "+uuid.NewString()[:8], "As a shopper I want to pay and get refunds.")

	scripts := map[string][]map[string]any{
		"design": {submit(map[string]any{"specification": "Paying and refunding at checkout.", "cases": []map[string]any{
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
	workflowID := createWorkflow(t, alice, testDesignHalf(t, providers))
	run := startRun(t, alice, workflowID, map[string]any{"exciterProject": project, "featureKey": feature, "testSection": section,
		"armatureProject": armatureProject, "armatureIssue": issue, "repository": "acme/shop"}, http.StatusCreated)
	done := waitRun(t, alice, run["id"].(string), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["error"])
	}
	output := done["output"].(map[string]any)
	changed := map[string]any{}
	for _, r := range output["gather"].(map[string]any)["requirements"].([]any) {
		changed[stringOf(r.(map[string]any)["key"])] = r.(map[string]any)["changed"]
	}
	if len(changed) != 2 || changed[pay] != true || changed[refund] != true {
		t.Errorf("gather read the requirements as changed %v, want both: %s moved on, %s has no test case", changed, pay, refund)
	}
	filed := output["file-tests"].(map[string]any)["cases"].([]any)
	if len(filed) != 2 {
		t.Fatalf("the run filed %v", filed)
	}
	_, sec := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+section, nil)
	for _, f := range filed {
		key := stringOf(f.(map[string]any)["key"])
		_, got := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+key, nil)
		r := got["requirement"].(map[string]any)
		if r["testCase"] == nil || r["parentId"] != sec["requirement"].(map[string]any)["id"] {
			t.Errorf("%s = %v, want a test case under %s", key, r, section)
		}
		_, links := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+key+"/links", nil)
		if !strings.Contains(stringOf(links), "verifies") {
			t.Errorf("%s is linked as %v, want verifies", key, links)
		}
	}
	if _, got := alice.atExciter(t, exciterBase, http.MethodGet, "/requirements/"+existing, nil); got["requirement"].(map[string]any)["title"] != "Pays a cart by card" {
		t.Errorf("%s was not revised: %v", existing, got)
	}
}
