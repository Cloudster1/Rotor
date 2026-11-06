//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
)

// testDesign is the designer's draft: RTR-5 rewritten for the changed RTR-2, and a new case for RTR-3.
func testDesign(title string) map[string]any {
	return submit(map[string]any{"specification": "System level, by scenario and boundary values. " + title, "cases": []map[string]any{
		{"key": "RTR-5", "title": "Pays a cart by card", "preconditions": "A cart of one item.",
			"steps": []map[string]any{{"action": "Pay by card.", "expected": "The order is paid in euros."}}, "verifies": []string{"RTR-2"}},
		{"title": "Refunds a paid order", "preconditions": "A paid order.",
			"steps": []map[string]any{{"action": "Refund it.", "expected": "The payment is refunded."}}, "verifies": []string{"RTR-3"}}}})
}

// refundDesign is the designer's draft once RTR-3 changed: its case rewritten.
func refundDesign() map[string]any {
	return submit(map[string]any{"specification": "Refunds by state transition.", "cases": []map[string]any{
		{"key": "RTR-6", "title": "Refunds a paid order in part", "preconditions": "A paid order.",
			"steps": []map[string]any{{"action": "Refund half.", "expected": "Half the payment is refunded."}}, "verifies": []string{"RTR-3"}}}})
}

func changes(finding string) map[string]any {
	return submit(map[string]any{"verdict": "changes", "findings": []string{finding}})
}

// The test specification workflow as it ships (#213): an approved second draft filed with its links, the suspect
// link it settled cleared, an unchanged feature left alone, and a person's decision after the third draft.
func TestAFeaturesTestCasesAreDesignedReviewedAndFiled(t *testing.T) {
	h := newHarness(t)
	alice := signedIn(t, "alice")
	scripts := map[string][]map[string]any{
		"design": {testDesign("First draft."), testDesign("Second draft."), refundDesign(), refundDesign(), refundDesign()},
		"design-review": {changes("Name the currency in the expected result."), submit(map[string]any{"verdict": "approve", "findings": []string{}}),
			changes("Say how much is refunded."), changes("Say how much is refunded."), changes("Say how much is refunded.")},
	}
	providers, models := map[string]string{}, map[string]*toolStub{}
	for agent, script := range scripts {
		key := "llm-key-" + uuid.NewString()
		models[agent] = newToolStub(t, map[string]any{"llmKey": key, "llmScript": script})
		providers[agent] = addProvider(t, alice, models[agent], key)["name"].(string)
	}

	exciterName := "exciter-" + uuid.NewString()[:8]
	exciter := newToolStub(t, map[string]any{"product": exciterName})
	bob, bobID, exciterID := connected(t, exciter)
	token, err := controlPlane(t, h, nil).Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: exciterID,
		Scopes: []string{"mcp"}, RunID: "seed-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	seedFeature(t, exciter, token.Value)

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "requirement-to-test-spec.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(example), "tool: exciter\n", "tool: "+exciterName+"\n")
	source = strings.Replace(source, "name: requirement-to-test-spec\n", "name: "+uniqueName("requirement-to-test-spec")+"\n", 1)
	workflowID := createWorkflow(t, bob, withModels(source, providers))
	inputs := map[string]any{"exciterProject": "RTR", "featureKey": "RTR-1", "testSection": "RTR-4"}
	start := func() string {
		t.Helper()
		return startRun(t, bob, workflowID, inputs, http.StatusCreated)["id"].(string)
	}
	succeeded := func(runID string) map[string]any {
		t.Helper()
		done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
		if done["status"] != "succeeded" {
			t.Fatalf("the run ended %v: %v; events %+v", done["status"], done, h.events(t, uuid.MustParse(runID)))
		}
		return done["output"].(map[string]any)
	}
	suspectOf := func(reqKey string) []any {
		var suspect []any
		for _, l := range mcpCall(t, exciter, token.Value, "list_links", map[string]any{"reqKey": reqKey})["links"].([]any) {
			s, _ := l.(map[string]any)["suspect"].([]any)
			suspect = append(suspect, s...)
		}
		return suspect
	}
	if len(suspectOf("RTR-2")) == 0 {
		t.Fatal("the seed left RTR-2's test case current; it must start suspect")
	}

	// First run: the reviewer asks for one change, approves the second draft, and the cases are filed.
	output := succeeded(start())
	checkTestCasesFiled(t, exciter)
	for _, agent := range []string{"design", "design-review"} {
		offeredShippedSkill(t, models[agent], 0, "test-engineering")
	}
	if suspect := suspectOf("RTR-2"); len(suspect) != 0 {
		t.Errorf("RTR-2's verifies link is still suspect: %v", suspect)
	}
	filed := output["file"].(map[string]any)
	if cleared, _ := filed["cleared"].([]any); len(filed["cases"].([]any)) != 2 || len(cleared) != 1 || len(cleared[0].(map[string]any)["suspect"].([]any)) != 0 {
		t.Errorf("the run filed %v, want two cases and one suspect link cleared", filed)
	}
	for _, r := range filed["reviews"].([]any) {
		if r.(map[string]any)["outcome"] != "requested" {
			t.Errorf("a case's review was not requested: %v", r)
		}
	}
	records := exciter.Records()
	if spec := stringOf(records["requirementComments"]); !strings.Contains(spec, "Test specification from Rotor") || !strings.Contains(spec, "Second draft.") {
		t.Errorf("RTR-1 has the comments %v, want the approved specification", records["requirementComments"])
	}
	if reviews, _ := records["reviews"].([]any); len(reviews) != 2 {
		t.Errorf("reviews were asked for %v, want the two cases", reviews)
	}

	// Second run: nothing changed, so nothing is designed or filed.
	output = succeeded(start())
	if why := stringOf(output["done"]); !strings.Contains(why, "latest revision") {
		t.Errorf("an unchanged feature answered %v", output)
	}

	// RTR-3 changes; the reviewer never approves, so after the third draft a person files the last one.
	mcpCall(t, exciter, token.Value, "update_requirement", map[string]any{"reqKey": "RTR-3", "baseRevision": 1,
		"statementMarkdown": "The shop shall refund a payment in whole or in part."})
	runID := start()
	decide(t, bob, approvalOf(t, bob, runID), map[string]any{"decision": "approve", "comment": "File it as it is."}, http.StatusOK)
	succeeded(runID)
	var refund map[string]any
	for _, r := range exciter.Records()["requirements"].([]any) {
		if r.(map[string]any)["key"] == "RTR-6" {
			refund = r.(map[string]any)
		}
	}
	if refund == nil || refund["title"] != "Refunds a paid order in part" {
		t.Errorf("RTR-6 = %v, want the last draft filed after the person approved", refund)
	}
	if suspect := suspectOf("RTR-3"); len(suspect) != 0 {
		t.Errorf("RTR-3's verifies link is still suspect: %v", suspect)
	}
}
