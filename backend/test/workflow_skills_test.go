//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// skilled is one agent node naming the skills given as name@version, on the provider's stub model when one is given.
func skilled(name, provider string, pins ...string) string {
	model := ""
	if provider != "" {
		model = "model: {name: stub-model, provider: " + provider + "}\n"
	}
	var skills strings.Builder
	for _, pin := range pins {
		skill, version, _ := strings.Cut(pin, "@")
		fmt.Fprintf(&skills, "      - {name: %s, version: %s}\n", skill, version)
	}
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + name + `}
` + model + `limits: {maxSteps: 5, timeout: 5m}
entry: think
nodes:
  - id: think
    type: agent
    prompt: Think it through.
    skills:
` + skills.String()
}

// ownWorkflow saves source as b's new workflow and removes it and its runs after the test, before the people they
// name go.
func (h *harness) ownWorkflow(t *testing.T, b *browser, source string) string {
	t.Helper()
	id := createWorkflow(t, b, source)
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := h.super.Exec(ctx, `DELETE FROM run WHERE workflow_id = $1`, id); err != nil {
			t.Errorf("remove the workflow's runs: %v", err)
		}
		if _, err := h.super.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, id); err != nil {
			t.Errorf("remove the workflow: %v", err)
		}
	})
	return id
}

// validated is what POST /workflows/validate answers b for a spec: each problem as "path code", and the messages.
func validated(t *testing.T, b *browser, source string) (problems, messages []string) {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows/validate", map[string]any{"specYAML": source})
	if status != http.StatusOK {
		t.Fatalf("validating = %d %v", status, answer)
	}
	items, _ := answer["problems"].([]any)
	for _, item := range items {
		p := item.(map[string]any)
		problems = append(problems, fmt.Sprintf("%v %v", p["path"], p["code"]))
		messages = append(messages, fmt.Sprint(p["message"]))
	}
	return problems, messages
}

// refusedSave saves source as a new workflow as b and answers the message the refusal places at path.
func refusedSave(t *testing.T, b *browser, source, path string) string {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/workflows", map[string]any{"specYAML": source, "comment": "first"})
	refusal, _ := answer["error"].(map[string]any)
	fields, _ := refusal["fields"].(map[string]any)
	message, _ := fields[path].(string)
	if status != http.StatusUnprocessableEntity || message == "" {
		t.Fatalf("saving %s = %d %v, want 422 at %s", source, status, answer, path)
	}
	return message
}

func TestASaveChecksEachSkillAsTheSaver(t *testing.T) {
	h := newHarness(t)
	owner, author := h.addPerson(t, "member"), h.addPerson(t, "member")
	ob, ab := h.browserFor(t, owner), h.browserFor(t, author)
	theirs := skillName("theirs")
	theirsID := h.createSkill(t, ob, theirs)
	mine := skillName("mine")
	mineID := h.createSkill(t, ab, mine)
	missing := skillName("missing")

	// A skill nobody has and a skill the saver may not read answer alike, save for the name.
	unknown := refusedSave(t, ab, skilled(uniqueName("unknown"), "", missing+"@1"), "/nodes/0/skills/0")
	unreadable := refusedSave(t, ab, skilled(uniqueName("unreadable"), "", theirs+"@1"), "/nodes/0/skills/0")
	if strings.ReplaceAll(unknown, missing, "X") != strings.ReplaceAll(unreadable, theirs, "X") {
		t.Errorf("an unreadable skill reads %q, a missing one %q", unreadable, unknown)
	}
	for source, want := range map[string]string{
		skilled("v", "", missing+"@1"): "/nodes/0/skills/0 skill_unknown",
		skilled("v", "", theirs+"@1"):  "/nodes/0/skills/0 skill_unknown",
		skilled("v", "", mine+"@2"):    "/nodes/0/skills/0/version skill_version_unknown",
	} {
		if problems, _ := validated(t, ab, source); !slices.Equal(problems, []string{want}) {
			t.Errorf("the editor's check of %s found %v, want %s", source, problems, want)
		}
	}
	refusedSave(t, ab, skilled(uniqueName("version"), "", mine+"@2"), "/nodes/0/skills/0/version")

	// Shared with the saver, the skill saves; a built-in one saves for everyone, at the versions it ships.
	if status, answer := ob.api(http.MethodPost, "/skills/"+theirsID+"/grants", map[string]any{"role": "skill_reader", "userId": author.String()}); status != http.StatusCreated {
		t.Fatalf("sharing the skill = %d %v", status, answer)
	}
	shipped := skillName("shipped")
	h.builtinSkill(t, shipped, 3)
	saved := h.ownWorkflow(t, ab, skilled(uniqueName("saved"), "", mine+"@1", theirs+"@1", shipped+"@3"))
	if saved == "" {
		t.Fatal("a workflow naming skills its saver reads was not saved")
	}
	if problems, _ := validated(t, ab, skilled("v", "", shipped+"@1")); !slices.Equal(problems, []string{"/nodes/0/skills/0/version skill_version_unknown"}) {
		t.Errorf("a built-in skill's missing version found %v", problems)
	}

	// An archived skill takes no new pins, but the version saved before still starts (TestARunNeedsItsPersonToReadEverySkill).
	if status, _ := ab.api(http.MethodDelete, "/skills/"+mineID, nil); status != http.StatusNoContent {
		t.Fatalf("archiving = %d", status)
	}
	refusedSave(t, ab, skilled(uniqueName("archived"), "", mine+"@1"), "/nodes/0/skills/0")
	if problems, _ := validated(t, ab, skilled("v", "", mine+"@1")); !slices.Equal(problems, []string{"/nodes/0/skills/0 skill_archived"}) {
		t.Errorf("an archived skill found %v", problems)
	}
}

func TestARunNeedsItsPersonToReadEverySkill(t *testing.T) {
	h := newHarness(t)
	key := "llm-key-" + uuid.NewString()
	stub := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		{"content": "Thought.", "promptTokens": 1, "completionTokens": 0},
		{"content": "Thought again.", "promptTokens": 1, "completionTokens": 0}}})
	provider := stubProvider(t, signedIn(t, "alice"), stub, key, 0)
	owner, runner := h.addPerson(t, "member"), h.addPerson(t, "member")
	ob, rb := h.browserFor(t, owner), h.browserFor(t, runner)
	name := skillName("private")
	skillID := h.createSkill(t, ob, name)
	workflowID := h.ownWorkflow(t, ob, skilled(uniqueName("private"), provider, name+"@1"))
	if status, answer := ob.api(http.MethodPost, "/workflows/"+workflowID+"/grants", map[string]any{"role": "runner", "userId": runner.String()}); status != http.StatusCreated {
		t.Fatalf("sharing the workflow = %d %v", status, answer)
	}

	answer := startRun(t, rb, workflowID, map[string]any{}, http.StatusUnprocessableEntity)
	refusal, _ := answer["error"].(map[string]any)
	if errorCode(answer) != "skill_unreadable" || !strings.Contains(fmt.Sprint(refusal["message"]), name) {
		t.Fatalf("a run by someone who may not read the skill = %v", answer)
	}

	if status, answer := ob.api(http.MethodPost, "/skills/"+skillID+"/grants", map[string]any{"role": "skill_reader", "userId": runner.String()}); status != http.StatusCreated {
		t.Fatalf("sharing the skill = %d %v", status, answer)
	}
	run := startRun(t, rb, workflowID, map[string]any{}, http.StatusCreated)
	if done := waitRun(t, rb, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run once the skill is shared ended %v (%v)", done["status"], done["reason"])
	}

	// Archiving the skill keeps the pinned version starting. The run is waited for, so no sandbox outlives the test.
	if status, _ := ob.api(http.MethodDelete, "/skills/"+skillID, nil); status != http.StatusNoContent {
		t.Fatalf("archiving = %d", status)
	}
	run = startRun(t, ob, workflowID, map[string]any{}, http.StatusCreated)
	if done := waitRun(t, ob, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the run of an archived skill's pin ended %v (%v)", done["status"], done["reason"])
	}
}

func TestATriggerWhoseOwnerMayNotReadASkillIsTurnedOff(t *testing.T) {
	h := newHarness(t)
	author, owner := h.addPerson(t, "member"), h.addPerson(t, "member")
	ab, ob := h.browserFor(t, author), h.browserFor(t, owner)
	name := skillName("lent")
	skillID := h.createSkill(t, ab, name)
	status, answer := ab.api(http.MethodPost, "/skills/"+skillID+"/grants", map[string]any{"role": "skill_reader", "userId": owner.String()})
	if status != http.StatusCreated {
		t.Fatalf("sharing the skill = %d %v", status, answer)
	}
	grant := idOf(t, answer, "grant")
	workflowID := h.ownWorkflow(t, ob, skilled(uniqueName("lent"), "", name+"@1"))
	trigger := addTrigger(t, ob, workflowID, schedule("yearly", yearly, false), http.StatusCreated)
	id := uuid.MustParse(trigger["id"].(string))

	if status, _ := ab.api(http.MethodDelete, "/skills/"+skillID+"/grants/"+grant, nil); status != http.StatusNoContent {
		t.Fatalf("revoking = %d", status)
	}
	fireAt(t, h, id, nextRun(t, trigger).Add(time.Second))
	refused := lastFire(t, ob, id.String(), "refused", "started", "failed")
	disabled, _ := refused["disabled"].(map[string]any)
	if refused["lastFire"].(map[string]any)["outcome"] != "refused" || disabled["reason"] != "skill_unreadable" ||
		!strings.Contains(fmt.Sprint(disabled["message"]), name) {
		t.Fatalf("the firing once the skill is no longer shared reads %v", refused)
	}
	var runs int
	if err := h.super.QueryRow(t.Context(), `SELECT count(*) FROM run WHERE trigger_id = $1`, id).Scan(&runs); err != nil || runs != 0 {
		t.Errorf("the trigger started %d runs (%v)", runs, err)
	}
	if status, answer := ob.api(http.MethodPost, "/triggers/"+id.String()+"/enable", nil); status != http.StatusUnprocessableEntity || errorCode(answer) != "skill_unreadable" {
		t.Errorf("turning it on again = %d %v, want 422 skill_unreadable", status, answer)
	}
}
