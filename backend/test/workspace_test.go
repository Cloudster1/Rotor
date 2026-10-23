//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// workspaceTenant is an Armature tenant whose project CP connects acme/portal, which bob sees and whose issue CP-12
// he may edit, and whose model answers from script (#136).
func workspaceTenant(t *testing.T, key string, script []map[string]any) *toolStub {
	t.Helper()
	return newToolStub(t, map[string]any{"product": "armature", "llmKey": key, "llmScript": script,
		"issues":       map[string]any{"CP-12": map[string]any{"summary": "Sign in with a passkey", "readers": []string{"bob"}}},
		"repositories": []map[string]any{{"project": "CP", "name": "acme/portal", "readers": []string{"bob"}}}})
}

// allowToolchain lets workspaces use the busybox image the sandbox tests run, as an administrator does.
func allowToolchain(t *testing.T, alice *browser) {
	t.Helper()
	setting := map[string]any{"images": []map[string]any{{"name": toolchainName, "reference": toolchainImage}},
		"packageMirror": "", "maxStepTime": "10m", "maxVolume": "256Mi"}
	if status, answer := alice.api(http.MethodPut, "/settings/workspaces", setting); status != http.StatusOK {
		t.Fatalf("allowing the toolchain = %d %v", status, answer)
	}
}

// workspaceWorkflow binds acme/portal with write and runs nodes, the first of which may make the branch it names.
func workspaceWorkflow(provider, nodes, edges string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("workspace") + `}
inputs: {type: object, required: [branch], properties: {branch: {type: string}}}
tools:
  repo:
    tool: armature
    scopes: [read, issue.write]
    access: write
    repository: {project: CP, name: acme/portal}
    allowTools: [create_issue_branch]
model: {name: stub-model, provider: ` + provider + `}
limits: {maxSteps: 20, timeout: 5m}
` + nodes + edges
}

// portal is acme/portal as the stub's records show it after the runs.
func portal(stub *toolStub) map[string]any {
	return stub.Records()["repositories"].(map[string]any)["CP/acme/portal"].(map[string]any)
}

func branchHead(stub *toolStub, branch string) string {
	head, _ := portal(stub)["refs"].(map[string]any)["refs/heads/"+branch].(string)
	return head
}

// A workspace step works on the run's branch through the stub's git stand-in: a writing agent's files and commands
// are pushed with the run's token, a reading one's and a failed one's are not, and the token is stored nowhere.
func TestAWorkspaceStepPushesWhatAWritingAgentDidAndNothingElse(t *testing.T) {
	h := newHarness(t)
	key, branch := "llm-key-"+uuid.NewString(), "rotor/cp-12-"+uuid.NewString()[:8]
	stub := workspaceTenant(t, key, []map[string]any{
		{"toolCalls": []map[string]any{{"name": "write_file", "arguments": map[string]any{"path": "docs/passkeys.md", "content": "Passkeys first.\n"}}}},
		{"toolCalls": []map[string]any{{"name": "run_command", "arguments": map[string]any{"argv": []string{"cat", "docs/passkeys.md"}}}}},
		{"content": "The notes are written."},
		{"toolCalls": []map[string]any{{"name": "run_command", "arguments": map[string]any{"argv": []string{"touch", "by-a-reader.txt"}}}}},
		{"toolCalls": []map[string]any{{"name": "write_file", "arguments": map[string]any{"path": "by-a-reader.txt", "content": "x"}}}},
		{"content": "I read it."},
	})
	bob := signedIn(t, "bob")
	connectArmature(t, stub, bob)
	alice := signedIn(t, "alice")
	provider := addProvider(t, alice, stub, key)["name"].(string)
	allowToolchain(t, alice)
	workspace := func(access string) string {
		return `    workspace: {repository: repo, branch: "{{ inputs.branch }}", image: ` + toolchainName + `, access: ` + access + "}\n"
	}

	writer := createWorkflow(t, bob, workspaceWorkflow(provider, `entry: branch
nodes:
  - id: branch
    type: tool
    tool: repo
    call: create_issue_branch
    arguments: {issueKey: CP-12, repository: acme/portal, name: "{{ inputs.branch }}"}
  - id: code
    type: agent
`+workspace("write")+`    prompt: Write down that passkeys come first, and show the file.
`, "edges: [{from: branch, to: code}]\n"))
	run := startRun(t, bob, writer, map[string]any{"branch": branch}, http.StatusCreated)
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the writing run ended %v: %v", done["status"], done)
	}
	pushed := branchHead(stub, branch)
	var commit string
	shown := false
	for _, e := range h.events(t, uuid.MustParse(run["id"].(string))) {
		switch e.Type {
		case "workspace.pushed":
			commit, _ = e.Payload["commit"].(string)
		case "workspace.command":
			shown = shown || strings.Contains(stringOf(e.Payload["outputTail"]), "Passkeys first.")
		}
	}
	if pushed == "" || commit != pushed || !shown {
		t.Fatalf("the branch is at %q, the run pushed %q, and the command showed the file: %v", pushed, commit, shown)
	}

	reader := createWorkflow(t, bob, workspaceWorkflow(provider, `entry: code
nodes:
  - id: code
    type: agent
`+workspace("read")+`    prompt: Read the notes.
`, ""))
	run = startRun(t, bob, reader, map[string]any{"branch": branch}, http.StatusCreated)
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the reading run ended %v: %v", done["status"], done["error"])
	}
	failing := createWorkflow(t, bob, workspaceWorkflow(provider, `entry: code
nodes:
  - id: code
    type: code
`+workspace("write")+`    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          ctx.workspace.write_file("half.txt", "half done")
          raise RuntimeError("the build broke")
`, ""))
	run = startRun(t, bob, failing, map[string]any{"branch": branch}, http.StatusCreated)
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "failed" {
		t.Fatalf("the failing run ended %v", done["status"])
	}
	if now := branchHead(stub, branch); now != pushed {
		t.Errorf("a reading or failed step moved the branch from %s to %s", pushed, now)
	}

	tokens, _ := portal(stub)["pushedWith"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("the branch was pushed %d times, want once", len(tokens))
	}
	if where := h.find(t, tokens[0].(string)); where != "" {
		t.Errorf("the run's repository token is stored in %s", where)
	}
	encoded, _ := json.Marshal(h.events(t, uuid.MustParse(run["id"].(string))))
	if strings.Contains(string(encoded), tokens[0].(string)) {
		t.Error("a run's events carry its repository token")
	}
}

// A workflow naming an image no administrator allows is refused when it is saved.
func TestAWorkspaceImageNoAdministratorAllowsIsRefused(t *testing.T) {
	stub := workspaceTenant(t, "llm-key-"+uuid.NewString(), nil)
	bob := signedIn(t, "bob")
	connectArmature(t, stub, bob)
	source := workspaceWorkflow("anything", `entry: code
nodes:
  - id: code
    type: code
    workspace: {repository: repo, branch: main, image: not-allowed-anywhere}
    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          return {}
`, "")
	status, answer := bob.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": source})
	problems, _ := answer["problems"].([]any)
	found := false
	for _, p := range problems {
		found = found || p.(map[string]any)["code"] == "image_not_allowed"
	}
	if status != http.StatusOK || !found {
		t.Errorf("validating a workflow with an image nobody allows = %d %v", status, answer)
	}
}
