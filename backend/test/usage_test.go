//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

func TestAResentUsageCountsOnceAndARunPastItsTokensIsStopped(t *testing.T) {
	h := newHarness(t)
	person := h.addPerson(t, "member")
	runID := h.newRun(t, person, `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: counted}
limits: {maxSteps: 5, timeout: 2m, maxTokens: 100}
entry: ask
nodes:
  - {id: ask, type: approval, message: 'Go on?'}
`, map[string]any{})
	runner := connectRunner(t, h, runID)
	runner.usage(1, 30, 20)
	runner.usage(1, 30, 20)
	var tokens int64
	if err := h.super.QueryRow(context.Background(), `SELECT input_tokens + output_tokens FROM run_usage WHERE run_id = $1`, runID).Scan(&tokens); err != nil || tokens != 50 {
		t.Fatalf("after a resent usage the run counts %d tokens (%v), want 50", tokens, err)
	}

	// 110 tokens pass the run's 100: the run ends with the reason, and its runner is told to stop.
	runner.usage(2, 40, 20)
	runner.await(func(m *runnerpb.ConnectResponse) bool { return m.GetCancel() != nil }, "a Cancel")
	var status, reason string
	var day int64
	if err := h.super.QueryRow(context.Background(), `SELECT r.status, r.reason, d.input_tokens + d.output_tokens FROM run r
		JOIN usage_daily d ON d.user_id = r.user_id WHERE r.id = $1`, runID).Scan(&status, &reason, &day); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || reason != "max_tokens" || day != 110 {
		t.Errorf("the run is %s (%s) with %d tokens for the day, want failed with max_tokens and 110", status, reason, day)
	}
}

func TestARunPastItsCostOrItsTokensAcrossSandboxesIsStoppedWithTheReason(t *testing.T) {
	key, product := "llm-key-"+uuid.NewString(), "cost-"+uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key,
		"llmScript": []map[string]any{{"content": "Thought.", "promptTokens": 600, "completionTokens": 0}}})
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	priced := stubProvider(t, alice, stub, key, 1000)
	unpriced := stubProvider(t, alice, stub, key, 0)

	// 600 tokens at 1000 dollars a million cost 0.60; a bound of 0.50 stops the run at its first call.
	costly := createWorkflow(t, bob, thinker(uniqueName("costly"), priced, ", maxCost: 0.5", ""))
	run := startRun(t, bob, costly, map[string]any{}, http.StatusCreated)
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "failed" || done["reason"] != "max_cost" {
		t.Errorf("a run past its cost ended %v (%v)", done["status"], done["reason"])
	} else if usage := done["usage"].(map[string]any); usage["inputTokens"] != float64(600) || usage["cost"] != 0.6 {
		t.Errorf("the run's usage reads %v", usage)
	}
	cheap := createWorkflow(t, bob, thinker(uniqueName("cheap"), unpriced, ", maxCost: 0.5", ""))
	if answer := startRun(t, bob, cheap, map[string]any{}, http.StatusUnprocessableEntity); errorCode(answer) != "unpriced_model" {
		t.Errorf("a cost bound on an unpriced model = %v", answer)
	}

	// Each sandbox uses 600 tokens, under the runner's own count of 1000; the control plane counts both.
	twice := createWorkflow(t, bob, `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: `+uniqueName("twice")+`}
model: {name: stub-model, provider: `+unpriced+`}
limits: {maxSteps: 10, timeout: 5m, maxTokens: 1000}
entry: first
nodes:
  - {id: first, type: agent, prompt: 'Think.'}
  - {id: wait, type: approval, message: 'Again?'}
  - {id: think, type: agent, prompt: 'Think again.'}
edges:
  - {from: first, to: wait}
  - {from: wait, to: think}
`)
	run = startRun(t, bob, twice, map[string]any{}, http.StatusCreated)
	approve(t, bob, run["id"].(string))
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "failed" || done["reason"] != "max_tokens" {
		t.Errorf("a run past its tokens across two sandboxes ended %v (%v)", done["status"], done["reason"])
	}
}

func TestAPersonPastTodaysQuotaIsStoppedAndStartsNoMore(t *testing.T) {
	key, product := "llm-key-"+uuid.NewString(), "quota-"+uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key,
		"llmScript": []map[string]any{{"content": "Thought.", "promptTokens": 1500, "completionTokens": 0}}})
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	provider := stubProvider(t, alice, stub, key, 1000)
	workflowID := createWorkflow(t, bob, thinker(uniqueName("quota"), provider, "", ""))

	for _, c := range []struct{ field, reason string }{{"dailyTokens", "daily_tokens"}, {"dailyCost", "daily_cost"}} {
		_, mine := bob.api(http.MethodGet, "/usage", nil)
		today := mine["today"].(map[string]any)
		limits := map[string]any{"dailyTokens": nil, "dailyCost": nil, "concurrentRuns": nil}
		if c.field == "dailyTokens" {
			limits[c.field] = int64(today["inputTokens"].(float64)+today["outputTokens"].(float64)) + 1000
		} else {
			limits[c.field] = today["cost"].(float64) + 1
		}
		setLimits(t, alice, limits)
		run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
		if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["reason"] != c.reason {
			t.Errorf("a run past the %s ended %v (%v)", c.field, done["status"], done["reason"])
		}
		if answer := startRun(t, bob, workflowID, map[string]any{}, http.StatusUnprocessableEntity); errorCode(answer) != "quota_reached" {
			t.Errorf("starting past the %s = %v", c.field, answer)
		}
	}
}

// napper is a workflow whose one code node takes a while, so two of its runs overlap.
func napper(name, limits string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + name + `}
limits: {maxSteps: 5, timeout: 5m` + limits + `}
entry: nap
nodes:
  - id: nap
    type: code
    output: {schema: {type: object}}
    source: |
      import time
      def run(state, ctx):
          time.sleep(6)
          return {}
`
}

// queuedWhileTheOtherRuns starts two runs and checks the second waits queued until the first ends, then runs.
func queuedWhileTheOtherRuns(t *testing.T, b *browser, workflowID string) {
	t.Helper()
	first := startRun(t, b, workflowID, map[string]any{}, http.StatusCreated)["id"].(string)
	waitRun(t, b, first, "running")
	second := startRun(t, b, workflowID, map[string]any{}, http.StatusCreated)["id"].(string)
	time.Sleep(3 * time.Second)
	if _, run := b.api(http.MethodGet, "/runs/"+second, nil); run["status"] != "queued" {
		t.Errorf("the second run is %v while the first runs, want queued", run["status"])
	}
	if done := waitRun(t, b, first, "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the first run ended %v", done["status"])
	}
	if done := waitRun(t, b, second, "succeeded", "failed"); done["status"] != "succeeded" {
		t.Errorf("the second run ended %v once the first was done", done["status"])
	}
}

func TestAWorkflowsAndAPersonsConcurrentRunsWaitQueued(t *testing.T) {
	alice, bob := signedIn(t, "alice"), signedIn(t, "bob")
	queuedWhileTheOtherRuns(t, bob, createWorkflow(t, bob, napper(uniqueName("one-at-a-time"), ", maxConcurrent: 1")))
	setLimits(t, alice, map[string]any{"dailyTokens": nil, "dailyCost": nil, "concurrentRuns": 1})
	queuedWhileTheOtherRuns(t, bob, createWorkflow(t, bob, napper(uniqueName("persons"), "")))
}

func TestARunCallingItsToolsTooOftenIsStopped(t *testing.T) {
	product := "rate-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: `+uniqueName("chatty")+`}
tools:
  t: {tool: `+product+`, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m, toolCallsPerMinute: 2}
entry: one
nodes:
  - {id: one, type: tool, tool: t, call: whoami}
  - {id: two, type: tool, tool: t, call: whoami}
  - {id: three, type: tool, tool: t, call: whoami}
edges:
  - {from: one, to: two}
  - {from: two, to: three}
`)
	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	if done := waitRun(t, bob, run["id"].(string), "succeeded", "failed"); done["status"] != "failed" || done["reason"] != "tool_rate" {
		t.Errorf("a run past its tool calls per minute ended %v (%v)", done["status"], done["reason"])
	}
}
