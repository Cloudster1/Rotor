//go:build integration

package test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// readsSkill is one agent node on the provider's stub model with one skill pinned, and steps for a few reads.
func readsSkill(name, provider, skill string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + name + `}
model: {name: stub-model, provider: ` + provider + `}
limits: {maxSteps: 12, timeout: 5m}
entry: think
nodes:
  - id: think
    type: agent
    prompt: Think it through.
    skills:
      - {name: ` + skill + `, version: 1}
`
}

// readSkillTurn is a scripted model answer calling read_skill with the arguments given.
func readSkillTurn(arguments map[string]any) map[string]any {
	return map[string]any{"toolCalls": []map[string]any{{"name": "read_skill", "arguments": arguments}},
		"promptTokens": 1, "completionTokens": 0}
}

// chatRequest is what the stub model recorded of its i-th request: the system message, the tools offered and the
// tool messages, in order.
func chatRequest(t *testing.T, stub *toolStub, i int) (system string, tools, answers []string) {
	t.Helper()
	requests, _ := stub.Stats()["llmRequests"].([]any)
	if i >= len(requests) {
		t.Fatalf("the stub model recorded %d requests, want more than %d", len(requests), i)
	}
	req := requests[i].(map[string]any)
	for _, m := range req["messages"].([]any) {
		message := m.(map[string]any)
		switch content := fmt.Sprint(message["content"]); message["role"] {
		case "system":
			system = content
		case "tool":
			answers = append(answers, content)
		}
	}
	for _, name := range req["tools"].([]any) {
		tools = append(tools, fmt.Sprint(name))
	}
	return system, tools, answers
}

func TestAnAgentSeesItsSkillAndReadsItsFilesDuringARun(t *testing.T) {
	h := newHarness(t)
	owner := h.addPerson(t, "member")
	ob := h.browserFor(t, owner)
	name := skillName("guided")
	h.createSkill(t, ob, name)
	key := "llm-key-" + uuid.NewString()
	stub := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		readSkillTurn(map[string]any{"skill": name}),
		readSkillTurn(map[string]any{"skill": name, "path": "references/guide.md"}),
		{"content": "Done.", "promptTokens": 1, "completionTokens": 0}}})
	provider := stubProvider(t, signedIn(t, "alice"), stub, key, 0)
	workflowID := h.ownWorkflow(t, ob, readsSkill(uniqueName("guided"), provider, name))

	run := startRun(t, ob, workflowID, map[string]any{}, http.StatusCreated)
	if done := waitRun(t, ob, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run ended %v (%v)", done["status"], done["reason"])
	}

	// The model first saw the skill's name, version and description, and was offered read_skill.
	system, tools, _ := chatRequest(t, stub, 0)
	if !strings.Contains(system, "- "+name+" (1): A skill the suite made.") || !slices.Contains(tools, "read_skill") {
		t.Errorf("the first request's instructions are %q and its tools %v", system, tools)
	}
	// Each read answered the file unfenced, as the workflow's own instruction.
	_, _, answers := chatRequest(t, stub, 2)
	if len(answers) != 2 || !strings.Contains(answers[0], "Do it well.") || !strings.Contains(answers[1], "A guide.") ||
		strings.Contains(answers[0], "<untrusted") {
		t.Errorf("read_skill answered %q", answers)
	}

	skillMD := skillFiles(name, "Do it well.")[1]["content"].(string)
	var reads []string
	for _, e := range h.events(t, uuid.MustParse(run["id"].(string))) {
		if e.Type == "skill.read" {
			p := e.Payload
			reads = append(reads, fmt.Sprintf("%s %v %v %v %v %v %v", e.Node, p["node"], p["skill"], p["version"], p["path"], p["offset"], p["bytes"]))
			if _, has := p["content"]; has {
				t.Errorf("a skill.read event carries content: %v", p)
			}
		}
	}
	want := []string{
		fmt.Sprintf("think think %s 1 SKILL.md 0 %d", name, len(skillMD)),
		fmt.Sprintf("think think %s 1 references/guide.md 0 %d", name, len("A guide.\n")),
	}
	if !slices.Equal(reads, want) {
		t.Errorf("the skill.read events are %q, want %q", reads, want)
	}
}

func TestASkillReadAfterItsGrantIsRevokedMidRunIsRefused(t *testing.T) {
	h := newHarness(t)
	owner, runner := h.addPerson(t, "member"), h.addPerson(t, "member")
	ob, rb := h.browserFor(t, owner), h.browserFor(t, runner)
	name := skillName("revoked")
	skillID := h.createSkill(t, ob, name)
	key := "llm-key-" + uuid.NewString()
	// The first answer is held back, so the grant is revoked while the agent thinks, after its run started.
	think := readSkillTurn(map[string]any{"skill": name})
	think["delayMs"] = (5 * time.Second).Milliseconds()
	stub := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		think, {"content": "Done without it.", "promptTokens": 1, "completionTokens": 0}}})
	provider := stubProvider(t, signedIn(t, "alice"), stub, key, 0)
	workflowID := h.ownWorkflow(t, ob, readsSkill(uniqueName("revoked"), provider, name))
	if status, answer := ob.api(http.MethodPost, "/workflows/"+workflowID+"/grants", map[string]any{"role": "runner", "userId": runner.String()}); status != http.StatusCreated {
		t.Fatalf("sharing the workflow = %d %v", status, answer)
	}
	status, answer := ob.api(http.MethodPost, "/skills/"+skillID+"/grants", map[string]any{"role": "skill_reader", "userId": runner.String()})
	if status != http.StatusCreated {
		t.Fatalf("sharing the skill = %d %v", status, answer)
	}
	grant := idOf(t, answer, "grant")

	run := startRun(t, rb, workflowID, map[string]any{}, http.StatusCreated)
	runID := uuid.MustParse(run["id"].(string))
	h.waitEvent(t, runID, "node.started", runWait)
	if status, _ := ob.api(http.MethodDelete, "/skills/"+skillID+"/grants/"+grant, nil); status != http.StatusNoContent {
		t.Fatalf("revoking the skill = %d", status)
	}
	if done := waitRun(t, rb, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run ended %v (%v)", done["status"], done["reason"])
	}

	system, _, _ := chatRequest(t, stub, 0)
	_, _, answers := chatRequest(t, stub, 1)
	if !strings.Contains(system, name) || len(answers) != 1 ||
		!strings.Contains(answers[0], "may no longer read the skill "+name) || strings.Contains(answers[0], "Do it well.") {
		t.Errorf("the read after revoking answered %q", answers)
	}
	for _, e := range h.events(t, runID) {
		if e.Type == "skill.read" {
			t.Errorf("a refused read recorded %v", e.Payload)
		}
	}
}
