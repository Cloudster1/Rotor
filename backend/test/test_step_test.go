//go:build integration

package test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A toolchain whose test tool writes JUnit with nothing to install, since the gate's network reaches no mirror: node's
// own test runner and its junit reporter (#138).
const (
	nodeToolchainImage = "node:26-alpine@sha256:0b36e8c136b94cd4fcf02188228e76c31ad5872eef3fec8cbd2eee500cfd9e80"
	nodeToolchainName  = "node-26"
)

// fixtureTests is a test module without dependencies: one passing and one failing test, each tagged with its case.
const fixtureTests = `import { test } from "node:test";
import assert from "node:assert/strict";
test("adds two numbers [verifies CP-T1]", () => assert.equal(1 + 1, 2));
test("subtracts [verifies CP-T2]", () => assert.equal(2 - 1, 2, "2 - 1 is not 2"));
`

// A test step runs the branch's tests in the toolchain and reports each case: the failing test is a result, the run
// succeeds, and the step pushes nothing.
func TestATestStepReportsEachCaseOfTheBranchsTests(t *testing.T) {
	h := newHarness(t)
	key, branch := "llm-key-"+uuid.NewString(), "rotor/cp-12-"+uuid.NewString()[:8]
	stub := workspaceTenant(t, key, nil)
	bob := signedIn(t, "bob")
	connectArmature(t, stub, bob)
	alice := signedIn(t, "alice")
	provider := addProvider(t, alice, stub, key)["name"].(string)
	setting := map[string]any{"images": []map[string]any{
		{"name": toolchainName, "reference": toolchainImage}, {"name": nodeToolchainName, "reference": nodeToolchainImage}},
		"packageMirror": "", "maxStepTime": "10m", "maxVolume": "256Mi"}
	if status, answer := alice.api(http.MethodPut, "/settings/workspaces", setting); status != http.StatusOK {
		t.Fatalf("allowing the toolchains = %d %v", status, answer)
	}
	workspace := func(access string) string {
		return `    workspace: {repository: repo, branch: "{{ inputs.branch }}", image: ` + nodeToolchainName + `, access: ` + access + "}\n"
	}
	tested := createWorkflow(t, bob, workspaceWorkflow(provider, `entry: branch
nodes:
  - id: branch
    type: tool
    tool: repo
    call: create_issue_branch
    arguments: {issueKey: CP-12, repository: acme/portal, name: "{{ inputs.branch }}"}
  - id: fixture
    type: code
`+workspace("write")+`    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          ctx.workspace.write_file("sum.test.mjs", '''`+strings.ReplaceAll(fixtureTests, "\n", "\n          ")+`''')
          return {}
  - id: test
    type: test
`+workspace("read")+`    command: [node, --test, --test-reporter=junit, --test-reporter-destination=junit.xml]
    reports: [junit.xml]
    timeout: 2m
`, "edges: [{from: branch, to: fixture}, {from: fixture, to: test}]\n"))

	run := startRun(t, bob, tested, map[string]any{"branch": branch}, http.StatusCreated)
	done := waitRun(t, bob, run["id"].(string), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done)
	}
	out, _ := done["output"].(map[string]any)["test"].(map[string]any)
	if out["passed"] != 1.0 || out["failed"] != 1.0 || out["skipped"] != 0.0 {
		t.Fatalf("the test step reported %v", out)
	}
	verified := map[string]string{}
	for _, c := range out["cases"].([]any) {
		c := c.(map[string]any)
		for _, k := range c["verifies"].([]any) {
			verified[k.(string)] = c["result"].(string)
		}
	}
	if verified["CP-T1"] != "passed" || verified["CP-T2"] != "failed" {
		t.Errorf("the cases verify %v", verified)
	}
	failures := out["failures"].([]any)
	if len(failures) != 1 || !strings.Contains(stringOf(failures[0].(map[string]any)["message"]), "2 - 1 is not 2") ||
		failures[0].(map[string]any)["file"] != "sum.test.mjs" {
		t.Errorf("the failures are %v", failures)
	}
	if _, ok := out["logTail"].(string); !ok {
		t.Errorf("the test step's output has no logTail: %v", out)
	}

	var kinds []string
	for _, e := range h.events(t, uuid.MustParse(run["id"].(string))) {
		if e.Node == "test" {
			kinds = append(kinds, e.Type)
		}
	}
	if !slices.Contains(kinds, "test.reported") || !slices.Contains(kinds, "workspace.command") || slices.Contains(kinds, "workspace.pushed") {
		t.Errorf("the test step's events are %v", kinds)
	}
}
