//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
)

// featureRunWait is how long the feature workflow may take: two rounds of six workspace steps each.
const featureRunWait = 10 * time.Minute

// The shop's checkout as the fixture repository holds it: nothing implemented yet, and no tests.
const (
	checkoutStub = "export function pay(cart) {\n  throw new Error(\"not implemented\");\n}\n"
	checkoutV1   = "export function pay(cart) {\n  return { paid: cart.total };\n}\n\n" +
		"export function refund(payment, amount) {\n  return { refunded: amount };\n}\n"
	checkoutV2 = "export function pay(cart) {\n  return { paid: cart.total };\n}\n\n" +
		"export function refund(payment, amount) {\n  if (amount < 0) throw new RangeError(\"A refund is never negative.\");\n" +
		"  return { refunded: amount };\n}\n"
	checkoutTests = `import { test } from "node:test";
import assert from "node:assert/strict";
import { pay, refund } from "./checkout.mjs";
test("pays by card [verifies RTR-5] [implements RTR-2]", () => assert.deepEqual(pay({ total: 30 }), { paid: 30 }));
test("refunds a payment [verifies RTR-6] [implements RTR-3]", () => assert.deepEqual(refund({}, 10), { refunded: 10 }));
`
	checkoutTestsV2 = checkoutTests + `test("refuses a negative refund [verifies RTR-6]", () => assert.throws(() => refund({}, -1), RangeError));
`
)

// submit is a scripted model's structured answer.
func submit(output map[string]any) map[string]any {
	return map[string]any{"toolCalls": []map[string]any{{"name": "submit_output", "arguments": output}}}
}

func approves(summary string) map[string]any {
	return submit(map[string]any{"verdict": "approve", "summary": summary, "findings": []string{}})
}

// featureScripts is what each agent's own stub model answers, so parallel reviewers keep to their own scripts: the
// coder writes twice, and the code reviewer asks for a change once, after reading the code.
func featureScripts() map[string][]map[string]any {
	write := func(path, content string) map[string]any {
		return map[string]any{"name": "write_file", "arguments": map[string]any{"path": path, "content": content}}
	}
	return map[string][]map[string]any{
		"design": {submit(map[string]any{"specification": "Paying and refunding at checkout, each checked through the checkout module.",
			"cases": []map[string]any{
				{"key": "RTR-5", "title": "Pays a cart by card", "preconditions": "A cart of 30 euros.",
					"steps": []map[string]any{{"action": "Pay the cart by card.", "expected": "30 euros are paid."}}, "verifies": []string{"RTR-2"}},
				{"title": "Refunds a payment", "preconditions": "A paid cart.",
					"steps": []map[string]any{{"action": "Refund 10 euros.", "expected": "10 euros are refunded."}}, "verifies": []string{"RTR-3", "RTR-99"}},
			}})},
		"design-review": {submit(map[string]any{"verdict": "approve", "findings": []string{}})},
		"code": {
			{"toolCalls": []map[string]any{write("checkout.mjs", checkoutV1), write("checkout.test.mjs", checkoutTests)}},
			{"toolCalls": []map[string]any{{"name": "run_command", "arguments": map[string]any{"argv": []string{"node", "--test"}}}}},
			{"content": "Implemented paying and refunding, with a test for each test case."},
			{"toolCalls": []map[string]any{write("checkout.mjs", checkoutV2), write("checkout.test.mjs", checkoutTestsV2)}},
			{"content": "Refused negative refunds, with a test."},
		},
		"review-code": {
			{"toolCalls": []map[string]any{{"name": "read_file", "arguments": map[string]any{"path": "checkout.mjs"}}}},
			submit(map[string]any{"verdict": "changes", "summary": "Refunds take any amount.",
				"findings": []string{"checkout.mjs: refund accepts a negative amount; refuse it."}}),
			approves("The checkout is simple and tested."),
		},
		"review-security":    {approves("Nothing reaches a shell, a query or a secret.")},
		"review-tests":       {approves("Every test case is automated and passes.")},
		"review-conformance": {approves("Paying and refunding do what RTR-2 and RTR-3 state.")},
	}
}

// withModels gives each agent of a workflow the stub provider named for it, in place of the model it names.
func withModels(source string, providers map[string]string) string {
	node := regexp.MustCompile(`^  - id: (\S+)$`)
	lines := strings.Split(source, "\n")
	current := ""
	for i, line := range lines {
		if m := node.FindStringSubmatch(line); m != nil {
			current = m[1]
		}
		if strings.HasPrefix(line, "    model: ") && providers[current] != "" {
			lines[i] = "    model: { name: stub-model, provider: " + providers[current] + " }"
		}
	}
	return strings.Join(lines, "\n")
}

// seedFeature writes the checkout feature in Exciter as its requirements engineer would: a section of two
// requirements, a section for tests, and a test case of the first, which changed since it was linked.
func seedFeature(t *testing.T, exciter *toolStub, token string) {
	t.Helper()
	create := func(args map[string]any) map[string]any {
		return mcpCall(t, exciter, token, "create_requirement", args)["requirement"].(map[string]any)
	}
	feature := create(map[string]any{"projectKey": "RTR", "title": "Checkout", "kind": "section"})
	under := func(id any) map[string]any { return map[string]any{"parentId": id, "afterId": nil} }
	create(map[string]any{"projectKey": "RTR", "title": "Pay by card", "statementMarkdown": "The shop shall take payment by card.", "place": under(feature["id"])})
	create(map[string]any{"projectKey": "RTR", "title": "Refund", "statementMarkdown": "The shop shall refund a payment.", "place": under(feature["id"])})
	tests := create(map[string]any{"projectKey": "RTR", "title": "Checkout tests", "kind": "section"})
	types := mcpCall(t, exciter, token, "list_types", map[string]any{"projectKey": "RTR"})["types"].([]any)
	testType := types[1].(map[string]any)["id"]
	create(map[string]any{"projectKey": "RTR", "title": "Pays by card", "typeId": testType, "place": under(tests["id"]),
		"testCase": map[string]any{"preconditions": nil, "parameters": []any{}, "steps": []map[string]any{
			{"actionMarkdown": "Pay.", "expectedMarkdown": "Paid."}}}})
	mcpCall(t, exciter, token, "link_requirements", map[string]any{"reqKey": "RTR-5", "otherKey": "RTR-2", "typeKey": "verifies"})
	mcpCall(t, exciter, token, "update_requirement", map[string]any{"reqKey": "RTR-2", "baseRevision": 1,
		"statementMarkdown": "The shop shall take payment by card, in euros."})
}

// The feature workflow as it ships (#140): test cases designed and filed in Exciter, a coder who needs a second round
// after one review asks for changes, four reviews joined, a pull request, and the results recorded per test case.
func TestAFeatureIsImplementedThroughReviewedTestedCode(t *testing.T) {
	h := newHarness(t)
	alice := signedIn(t, "alice")
	providers := map[string]string{}
	for agent, script := range featureScripts() {
		key := "llm-key-" + uuid.NewString()
		providers[agent] = addProvider(t, alice, newToolStub(t, map[string]any{"llmKey": key, "llmScript": script}), key)["name"].(string)
	}
	setting := map[string]any{"images": []map[string]any{{"name": nodeToolchainName, "reference": nodeToolchainImage}},
		"packageMirror": "", "maxStepTime": "10m", "maxVolume": "256Mi"}
	if status, answer := alice.api(http.MethodPut, "/settings/workspaces", setting); status != http.StatusOK {
		t.Fatalf("allowing node's toolchain = %d %v", status, answer)
	}

	exciterName := "exciter-" + uuid.NewString()[:8]
	exciter := newToolStub(t, map[string]any{"product": exciterName,
		"testPlans": []map[string]any{{"project": "RTR", "title": "Checkout", "cases": []string{"RTR-5", "RTR-6"}}}})
	bob, bobID, exciterID := connected(t, exciter)
	token, err := controlPlane(t, h, nil).Mint(context.Background(), connections.MintRequest{UserID: bobID, ToolID: exciterID,
		Scopes: []string{"mcp"}, RunID: "seed-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	seedFeature(t, exciter, token.Value)
	armature := newToolStub(t, map[string]any{"product": "armature",
		"issues": map[string]any{"CP-12": map[string]any{"summary": "Pay and refund at checkout", "readers": []string{"bob"}}},
		"repositories": []map[string]any{{"project": "CP", "name": "acme/shop", "readers": []string{"bob"},
			"files": map[string]string{"checkout.mjs": checkoutStub}}}})
	connectArmature(t, armature, bob)

	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "implement-feature.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(example), "tool: exciter\n", "tool: "+exciterName+"\n")
	source = strings.Replace(source, "name: implement-feature\n", "name: "+uniqueName("implement-feature")+"\n", 1)
	workflowID := createWorkflow(t, bob, withModels(source, providers))
	run := startRun(t, bob, workflowID, map[string]any{"exciterProject": "RTR", "featureKey": "RTR-1", "testSection": "RTR-4",
		"armatureProject": "CP", "armatureIssue": "CP-12", "repository": "acme/shop"}, http.StatusCreated)
	runID := run["id"].(string)
	done := waitFeature(t, bob, runID)
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["error"])
	}
	output := done["output"].(map[string]any)
	events := h.events(t, uuid.MustParse(runID))

	checkTestCasesFiled(t, exciter)
	visits := map[string]int{}
	var pushed []string
	for _, e := range events {
		switch {
		case e.Type == "node.started":
			visits[e.Node]++
		case e.Type == "workspace.pushed":
			if e.Node != "code" {
				t.Errorf("%s pushed the branch; only the coder writes", e.Node)
			}
			pushed = append(pushed, stringOf(e.Payload["commit"]))
		case e.Type == "tool.called" && e.Payload["write"] == true && !slices.Contains([]string{"file-tests", "branch", "finish"}, e.Node):
			t.Errorf("%s wrote through %v.%v; only code nodes write", e.Node, e.Payload["binding"], e.Payload["tool"])
		}
	}
	for node, want := range map[string]int{"design": 1, "code": 2, "test": 2, "review-code": 2, "review-security": 2, "review-tests": 2,
		"review-conformance": 2, "verdict": 2, "finish": 1} {
		if visits[node] != want {
			t.Errorf("%s ran %d times, want %d (all: %v)", node, visits[node], want, visits)
		}
	}

	branch := output["branch"].(map[string]any)["name"].(string)
	shop := armature.Records()["repositories"].(map[string]any)["CP/acme/shop"].(map[string]any)
	if head := shop["refs"].(map[string]any)["refs/heads/"+branch]; len(pushed) != 2 || head != pushed[1] {
		t.Errorf("the coder pushed %v, and %s is at %v", pushed, branch, head)
	}
	if tokens := shop["pushedWith"].([]any); len(tokens) != 2 {
		t.Errorf("the branch was pushed %d times, want the coder's two", len(tokens))
	}

	test := output["test"].(map[string]any)
	verified := map[string][]string{}
	for _, c := range test["cases"].([]any) {
		for _, key := range c.(map[string]any)["verifies"].([]any) {
			verified[key.(string)] = append(verified[key.(string)], c.(map[string]any)["result"].(string))
		}
	}
	if test["passed"] != 3.0 || test["failed"] != 0.0 || len(verified["RTR-5"]) != 1 || len(verified["RTR-6"]) != 2 {
		t.Errorf("the last test run reported %v", test)
	}
	verdict := output["verdict"].(map[string]any)
	if verdict["verdict"] != "approve" || len(verdict["reviews"].([]any)) != 4 || len(verdict["findings"].([]any)) != 0 {
		t.Errorf("the joined verdict = %v", verdict)
	}

	pulls := shop["pullRequests"].([]any)
	if len(pulls) != 1 || pulls[0].(map[string]any)["sourceBranch"] != branch ||
		!strings.Contains(stringOf(pulls[0].(map[string]any)["body"]), "Security review: Nothing reaches a shell") {
		t.Fatalf("the pull requests are %v", pulls)
	}
	url := pulls[0].(map[string]any)["url"].(string)
	executions := exciter.Records()["testExecutions"].([]any)
	if len(executions) != 1 {
		t.Fatalf("Exciter holds the executions %v", executions)
	}
	execution := executions[0].(map[string]any)
	outcomes := map[string]any{}
	for _, c := range execution["cases"].([]any) {
		outcomes[c.(map[string]any)["case"].(map[string]any)["key"].(string)] = c.(map[string]any)["outcome"]
	}
	if execution["state"] != "closed" || outcomes["RTR-5"] != "passed" || outcomes["RTR-6"] != "passed" {
		t.Errorf("the execution = %v", execution)
	}
	comments, _ := armature.Records()["comments"].(map[string]any)["CP-12"].([]any)
	if len(comments) != 1 || !strings.Contains(stringOf(comments[0]), url) || !strings.Contains(stringOf(comments[0]), "Nothing is merged") {
		t.Errorf("CP-12 has the comments %v", comments)
	}
}

// checkTestCasesFiled insists the designer's cases are in Exciter: RTR-5 revised, RTR-6 new under the test section,
// each linked verifies to its requirement once, and nothing linked to a key the feature does not hold.
func checkTestCasesFiled(t *testing.T, exciter *toolStub) {
	t.Helper()
	records := exciter.Records()
	byKey := map[string]map[string]any{}
	for _, r := range records["requirements"].([]any) {
		byKey[r.(map[string]any)["key"].(string)] = r.(map[string]any)
	}
	revised, made, section := byKey["RTR-5"], byKey["RTR-6"], byKey["RTR-4"]
	if revised == nil || revised["revision"] != 2.0 || revised["title"] != "Pays a cart by card" {
		t.Errorf("RTR-5 = %v, want it revised by the designer", revised)
	}
	if made == nil || made["parentId"] != section["id"] || made["testCase"] == nil || made["type"] == nil {
		t.Fatalf("RTR-6 = %v, want a test case under RTR-4", made)
	}
	var links []string
	for _, l := range records["requirementLinks"].([]any) {
		l := l.(map[string]any)
		links = append(links, fmt.Sprintf("%v %v %v", l["source"], l["type"], l["target"]))
	}
	slices.Sort(links)
	if want := []string{"RTR-5 verifies RTR-2", "RTR-6 verifies RTR-3"}; !slices.Equal(links, want) {
		t.Errorf("the links are %v, want %v", links, want)
	}
}

// waitFeature waits for the run to end, longer than a single step's run does.
func waitFeature(t *testing.T, b *browser, runID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(featureRunWait)
	for time.Now().Before(deadline) {
		status, run := b.api(http.MethodGet, "/runs/"+runID, nil)
		if status != http.StatusOK {
			t.Fatalf("reading run %s = %d %v", runID, status, run)
		}
		if run["status"] == "succeeded" || run["status"] == "failed" || run["status"] == "waiting" {
			return run
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("run %s did not end within %s", runID, featureRunWait)
	return nil
}
