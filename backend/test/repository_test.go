//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// repositoryStub is an Armature tenant whose project CP connects acme/portal, which bob sees and whose issue CP-12
// he may edit, and acme/hidden, which only alice sees (#137).
func repositoryStub(t *testing.T) *toolStub {
	t.Helper()
	return newToolStub(t, map[string]any{"product": "armature",
		"issues": map[string]any{"CP-12": map[string]any{"summary": "Sign in with a passkey", "readers": []string{"bob"}}},
		"repositories": []map[string]any{
			{"project": "CP", "name": "acme/portal", "readers": []string{"bob"}},
			{"project": "CP", "name": "acme/hidden", "readers": []string{"alice"}},
		}})
}

// repositoryWorkflow binds a repository with access, named fixed in CP or by the inputs, and runs source as its one node.
func repositoryWorkflow(access, name, source string) string {
	indented := "      " + strings.ReplaceAll(strings.TrimSpace(source), "\n", "\n      ")
	project := "CP"
	if strings.Contains(name, "{{") {
		project = "{{ inputs.project }}"
	}
	calls := "[list_repositories, get_issue_development]"
	if access == "write" {
		calls = "[list_repositories, create_issue_branch, open_pull_request]"
	}
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("repository") + `}
inputs: {type: object, properties: {project: {type: string}, repository: {type: string}}}
tools:
  repo:
    tool: armature
    scopes: [read, issue.write]
    access: ` + access + `
    repository: {project: "` + project + `", name: "` + name + `"}
    allowTools: ` + calls + `
limits: {maxSteps: 10, timeout: 5m}
entry: work
nodes:
  - id: work
    type: code
    tools: [repo]
    output: {schema: {type: object}}
    source: |
` + indented + "\n"
}

const listing = `def run(state, ctx):
    return ctx.call_tool("repo", "list_repositories", {"projectKey": "CP"})`

// A repository the person does not see is refused when saving, and when a run's inputs name it.
func TestARepositoryBindingIsCheckedAsThePersonSeesIt(t *testing.T) {
	stub := repositoryStub(t)
	bob := signedIn(t, "bob")
	connectArmature(t, stub, bob)

	status, answer := bob.api(http.MethodPost, "/workflows", map[string]any{"specYAML": repositoryWorkflow("read", "acme/hidden", listing)})
	refusal, _ := answer["error"].(map[string]any)
	fields, _ := refusal["fields"].(map[string]any)
	if status != http.StatusUnprocessableEntity || !strings.Contains(stringOf(fields["/tools/repo/repository"]), "acme/hidden") {
		t.Errorf("saving a repository bob does not see = %d %v", status, answer)
	}
	createWorkflow(t, bob, repositoryWorkflow("read", "acme/portal", listing))
	templated := createWorkflow(t, bob, repositoryWorkflow("read", "{{ inputs.repository }}", listing))
	refused := startRun(t, bob, templated, map[string]any{"project": "CP", "repository": "acme/hidden"}, http.StatusUnprocessableEntity)
	if refusal, _ := refused["error"].(map[string]any); refusal["code"] != "inputs_invalid" ||
		!strings.Contains(stringOf(refusal["fields"]), "acme/hidden") {
		t.Errorf("a run naming a repository bob does not see = %v", refused)
	}
}

// token asks the control plane for a binding's token as the runner does, and opens it at OpenBao.
func (f *fakeRunner) token(binding string) string {
	f.t.Helper()
	if err := f.stream.Send(&runnerpb.ConnectRequest{Message: &runnerpb.ConnectRequest_TokenRequest{TokenRequest: &runnerpb.TokenRequest{Binding: binding}}}); err != nil {
		f.t.Fatal(err)
	}
	grant := f.await(func(m *runnerpb.ConnectResponse) bool { return m.GetTokenGrant() != nil }, "the token grant").GetTokenGrant()
	if grant.Error != "" {
		f.t.Fatalf("no token for %s: %s", binding, grant.Error)
	}
	opened, err := unwrap(f.t, grant.GetWrappingToken())
	if err != nil {
		f.t.Fatal(err)
	}
	token, _ := opened["access_token"].(string)
	return token
}

// event sends an event numbered seq, as a runner reports what a node did, and waits for its acknowledgement.
func (f *fakeRunner) event(seq uint64, kind string, payload map[string]any) {
	f.t.Helper()
	encoded, _ := json.Marshal(payload)
	if err := f.stream.Send(&runnerpb.ConnectRequest{Seq: seq, Message: &runnerpb.ConnectRequest_Event{Event: &runnerpb.Event{
		Type: kind, NodeId: "work", PayloadJson: string(encoded)}}}); err != nil {
		f.t.Fatal(err)
	}
	f.await(func(m *runnerpb.ConnectResponse) bool { return m.Ack >= seq && m.Message == nil }, "the acknowledgement")
}

// repository is the binding's repository as StartRun resolved it.
func (f *fakeRunner) repository(binding string) (*runnerpb.Repository, []string) {
	f.t.Helper()
	for _, b := range f.start.GetTools() {
		if b.Binding == binding && b.Repository != nil {
			return b.Repository, b.Scopes
		}
	}
	f.t.Fatalf("StartRun resolved no repository for %s: %v", binding, f.start.GetTools())
	return nil, nil
}

// gitAs runs the real git client in dir with the token as a bearer, as the runner's own git will.
func gitAs(t *testing.T, dir, token string, args ...string) (string, bool) {
	t.Helper()
	full := append([]string{"-c", "http.extraHeader=Authorization: Bearer " + token}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Bob", "GIT_AUTHOR_EMAIL=bob@suite.test", "GIT_COMMITTER_NAME=Bob", "GIT_COMMITTER_EMAIL=bob@suite.test"}
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// A run's repository binding is the git proxy's address and the run's own token for it: with access write it
// clones and pushes the issue's branch, with read it clones and cannot push, and the token is stored nowhere.
func TestARunClonesAndPushesItsRepositoryWithItsOwnToken(t *testing.T) {
	h := newHarness(t)
	stub := repositoryStub(t)
	bob := signedIn(t, "bob")
	connectArmature(t, stub, bob)
	_, me := bob.me()
	bobID := uuid.MustParse(me["user"].(map[string]any)["id"].(string))
	inputs := map[string]any{"project": "CP", "repository": "acme/portal"}
	work := t.TempDir()

	writer := connectRunner(t, h, h.newRun(t, bobID, repositoryWorkflow("write", "{{ inputs.repository }}", listing), inputs))
	repository, scopes := writer.repository("repo")
	if !strings.HasSuffix(repository.CloneUrl, "/t/"+stub.name+"/api/v1/git/CP/acme/portal.git") || repository.Name != "acme/portal" ||
		!slices.Contains(scopes, "git.write") {
		t.Fatalf("the repository binding resolved to %v with %v", repository, scopes)
	}
	token := writer.token("repo")
	made := mcpCall(t, stub, token, "create_issue_branch", map[string]any{"issueKey": "CP-12", "repository": "acme/portal"})
	branch := made["branch"].(map[string]any)["name"].(string)
	if out, ok := gitAs(t, work, token, "clone", "-q", "--depth", "1", "--branch", branch, repository.CloneUrl, "write"); !ok {
		t.Fatalf("cloning with the run's token failed: %s", out)
	}
	dir := filepath.Join(work, "write")
	if err := os.WriteFile(filepath.Join(dir, "passkey.go"), []byte("package passkey\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "passkey.go"}, {"commit", "-q", "-m", "Sign in with a passkey"}, {"push", "-q", "origin", branch}} {
		if out, ok := gitAs(t, dir, token, args...); !ok {
			t.Fatalf("git %v with the run's token failed: %s", args, out)
		}
	}
	head, _ := gitAs(t, dir, token, "rev-parse", "HEAD")
	pushed := stub.Records()["repositories"].(map[string]any)["CP/acme/portal"].(map[string]any)["refs"].(map[string]any)["refs/heads/"+branch]
	if pushed != strings.TrimSpace(head) {
		t.Errorf("the issue's branch is at %v, want the pushed %s", pushed, head)
	}
	writer.event(1, "log", map[string]any{"text": "pushed with Authorization: Bearer " + token})

	reader := connectRunner(t, h, h.newRun(t, bobID, repositoryWorkflow("read", "{{ inputs.repository }}", listing), inputs))
	if _, scopes := reader.repository("repo"); slices.Contains(scopes, "git.write") {
		t.Errorf("a reading repository binding asks for %v", scopes)
	}
	reading := reader.token("repo")
	if out, ok := gitAs(t, work, reading, "clone", "-q", "--branch", branch, repository.CloneUrl, "read"); !ok {
		t.Fatalf("cloning with a reading run's token failed: %s", out)
	}
	dir = filepath.Join(work, "read")
	if err := os.WriteFile(filepath.Join(dir, "more.go"), []byte("package passkey\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitAs(t, dir, reading, "add", "more.go")
	gitAs(t, dir, reading, "commit", "-q", "-m", "More")
	if out, ok := gitAs(t, dir, reading, "push", "origin", branch); ok || !strings.Contains(out, "not allowed to push to issue branches") {
		t.Errorf("a reading run's push = %v %s", ok, out)
	}
	reader.event(1, "log", map[string]any{"text": "cloned https://rotor:" + reading + "@armature.example/x.git"})

	for _, secret := range []string{token, reading} {
		if where := h.find(t, secret); where != "" {
			t.Errorf("a run's repository token is stored in %s", where)
		}
	}
}

// A real run's code node makes the issue's branch and opens its pull request through the repository binding, as
// the person who started it.
func TestARunOpensThePullRequestOfItsIssuesBranch(t *testing.T) {
	stub := repositoryStub(t)
	bob := signedIn(t, "bob")
	connectArmature(t, stub, bob)
	workflow := createWorkflow(t, bob, repositoryWorkflow("write", "{{ inputs.repository }}", `def run(state, ctx):
    made = ctx.call_tool("repo", "create_issue_branch", {"issueKey": "CP-12", "repository": state["inputs"]["repository"]})
    branch = made["branch"]["name"]
    opened = ctx.call_tool("repo", "open_pull_request", {"issueKey": "CP-12", "repository": state["inputs"]["repository"],
        "branch": branch, "title": "Sign in with a passkey", "draft": True})
    return {"branch": branch, "number": opened["pullRequest"]["number"]}`))
	run := startRun(t, bob, workflow, map[string]any{"project": "CP", "repository": "acme/portal"}, http.StatusCreated)
	done := waitRun(t, bob, run["id"].(string), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v", done["status"], done["error"])
	}
	portal := stub.Records()["repositories"].(map[string]any)["CP/acme/portal"].(map[string]any)
	pulls, _ := portal["pullRequests"].([]any)
	if len(pulls) != 1 || pulls[0].(map[string]any)["authorName"] != "bob" || pulls[0].(map[string]any)["draft"] != true {
		t.Errorf("the repository holds the pull requests %v", pulls)
	}
}
