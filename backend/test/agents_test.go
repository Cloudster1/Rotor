//go:build integration

package test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// specialists is a writer on the workflow's model and a reviewer on its own, with its own role and steps (#133).
func specialists(name, workflowProvider, reviewerProvider string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + name + `}
model: {name: stub-model, provider: ` + workflowProvider + `}
limits: {maxSteps: 10, timeout: 5m, maxCost: 5}
entry: write
nodes:
  - {id: write, type: agent, prompt: 'Write the notes.'}
  - id: review
    type: agent
    model: {name: stub-model, provider: ` + reviewerProvider + `}
    role: You are a security reviewer.
    maxSteps: 5
    prompt: 'Review the notes.'
edges:
  - {from: write, to: review}
`
}

func TestTwoAgentsRunOnTheirOwnModelsAndEachCallCountsAtItsModelsPrice(t *testing.T) {
	writerKey, reviewerKey := "llm-key-"+uuid.NewString(), "llm-key-"+uuid.NewString()
	writer := newToolStub(t, map[string]any{"llmKey": writerKey,
		"llmScript": []map[string]any{{"content": "Notes.", "promptTokens": 100, "completionTokens": 0}}})
	reviewer := newToolStub(t, map[string]any{"llmKey": reviewerKey,
		"llmScript": []map[string]any{{"content": "Approved.", "promptTokens": 300, "completionTokens": 0}}})
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	cheap := stubProvider(t, alice, writer, writerKey, 1000)
	dear := stubProvider(t, alice, reviewer, reviewerKey, 2000)

	// An agent's own model is checked when the workflow is saved.
	status, answer := bob.api(http.MethodPost, "/workflows", map[string]any{
		"specYAML": specialists(uniqueName("refused"), cheap, "nowhere-"+uuid.NewString()[:8]), "comment": "first"})
	refusal, _ := answer["error"].(map[string]any)
	if fields, _ := refusal["fields"].(map[string]any); status != http.StatusUnprocessableEntity ||
		!strings.Contains(fmt.Sprint(fields["/nodes/1/model"]), "No administrator allows the model") {
		t.Fatalf("saving an agent on a model no administrator allows = %d %v", status, answer)
	}

	// 100 tokens at 1000 dollars a million and 300 at 2000 cost 0.10 and 0.60, each on its own provider's key.
	workflowID := createWorkflow(t, bob, specialists(uniqueName("specialists"), cheap, dear))
	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	done := waitRun(t, bob, run["id"].(string), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v (%v)", done["status"], done["reason"])
	}
	usage := done["usage"].(map[string]any)
	if usage["inputTokens"] != float64(400) || usage["cost"] != 0.7 {
		t.Errorf("the run's usage reads %v, want 400 tokens costing 0.70", usage)
	}
	want := []string{"review " + dear + " stub-model 300 0.6", "write " + cheap + " stub-model 100 0.1"}
	agents, _ := usage["agents"].([]any)
	var got []string
	for _, a := range agents {
		agent := a.(map[string]any)
		got = append(got, fmt.Sprintf("%v %v %v %v %v", agent["node"], agent["provider"], agent["model"], agent["inputTokens"], agent["cost"]))
	}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("the agents' usage reads %v, want %v", got, want)
	}
	for name, stub := range map[string]*toolStub{"writer": writer, "reviewer": reviewer} {
		if calls := stub.Stats()["llmCalls"]; calls != float64(1) {
			t.Errorf("the %s's model was called %v times, want once", name, calls)
		}
	}

	// A model taken away after the save is refused when a run starts.
	_, listed := alice.api(http.MethodGet, "/providers", nil)
	for _, p := range listed["providers"].([]any) {
		if provider := p.(map[string]any); provider["name"] == dear {
			alice.api(http.MethodDelete, "/providers/"+provider["id"].(string), nil)
		}
	}
	if answer := startRun(t, bob, workflowID, map[string]any{}, http.StatusUnprocessableEntity); errorCode(answer) != "model_not_allowed" {
		t.Errorf("starting a run whose agent's model is gone = %v", answer)
	}
}
