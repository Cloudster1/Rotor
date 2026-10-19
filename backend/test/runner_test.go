//go:build integration

package test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// conformanceWorkflow calls the tool as the run's person, shapes the answer in a code node and waits for approval.
func conformanceWorkflow(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: conformance}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - id: shape
    type: code
    output: {schema: {type: object, required: [who]}}
    source: |
      def run(state, ctx):
          print("shaping")
          return {"who": state["nodes"]["ask"]["user"]["name"]}
  - {id: confirm, type: approval, message: 'Go on as {{ nodes.shape.who }}?'}
edges:
  - {from: ask, to: shape}
  - {from: shape, to: confirm}
`
}

// sleeper waits in a code node, long enough for a test to break the link or freeze the sandbox meanwhile.
func sleeper(seconds string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: sleeper}
limits: {maxSteps: 10, timeout: 5m}
entry: wait
nodes:
  - id: wait
    type: code
    timeout: 2m
    output: {schema: {type: object}}
    source: |
      import time
      def run(state, ctx):
          print("waiting")
          time.sleep(` + seconds + `)
          return {"waited": True}
  - id: after
    type: code
    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          return {"done": True}
edges:
  - {from: wait, to: after}
`
}

func TestARunnerInASandboxCarriesARunAsItsPerson(t *testing.T) {
	h := newHarness(t)
	b, image := dockerBackend(t)
	product := "conformance-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	_, bobID, _ := connected(t, stub)

	runID := h.newRun(t, bobID, conformanceWorkflow(product), map[string]any{})
	bootstrap := h.bootstrap(t, runID)
	box := spawnRunner(t, b, image, runID, bootstrap)
	h.waitStatus(t, runID, "waiting", "failed", "succeeded")

	events := h.events(t, runID)
	contiguous(t, events)
	seen, at := map[string]int{}, map[string]int64{}
	for _, e := range events {
		seen[e.Type+":"+e.Node]++
		at[e.Type+":"+e.Node] = e.Seq
	}
	for _, want := range []string{"node.started:ask", "tool.called:ask", "node.finished:ask", "node.started:shape", "log:shape",
		"node.finished:shape", "node.started:confirm", "interrupt:confirm", "run.finished:"} {
		if seen[want] != 1 {
			t.Errorf("want one %s, got %d; events: %+v", want, seen[want], events)
		}
	}
	for _, node := range []string{"ask", "shape"} {
		if at["node.started:"+node] > at["node.finished:"+node] || at["node.finished:"+node] > at["node.started:shape"] && node == "ask" {
			t.Errorf("node %s is out of order: %+v", node, events)
		}
	}
	// The run waits, and then the approval it waits at opens (#31).
	if len(events) < 2 || events[len(events)-2].Type != "run.finished" || events[len(events)-2].Payload["status"] != "waiting" ||
		events[len(events)-1].Type != "approval.requested" || events[len(events)-1].Payload["message"] != "Go on as bob?" {
		t.Errorf("the last events = %+v, want run.finished waiting, then approval.requested", events[max(0, len(events)-2):])
	}
	for _, e := range events {
		switch {
		case e.Type == "tool.called" && (e.Payload["ok"] != true || !strings.Contains(e.Payload["resultSummary"].(string), `"name": "bob"`)):
			t.Errorf("the tool call = %v, want whoami answering bob", e.Payload)
		case e.Type == "interrupt" && e.Payload["message"] != "Go on as bob?":
			t.Errorf("the approval asks %q", e.Payload["message"])
		}
	}
	if status := waitExited(t, b, box); status.ExitCode != 0 {
		t.Errorf("the runner exited %d after its run waited", status.ExitCode)
	}
	if stats := stub.Stats(); stats["refreshes"] != float64(1) || stats["mcpCalls"] == float64(0) {
		t.Errorf("the tool saw %v; want one token minted for the run and calls with it", stats)
	}

	// The same bootstrap token again is refused, and the run hears nothing more.
	again := spawnRunner(t, b, image, runID, bootstrap)
	if status := waitExited(t, b, again); status.ExitCode != 2 {
		t.Errorf("a runner with a used bootstrap token exited %d, want 2 for refused", status.ExitCode)
	}
	if after := h.events(t, runID); len(after) != len(events) {
		t.Errorf("a refused runner added events: %+v", after[len(events):])
	}
}

func TestARunnerReconnectsAfterTheLinkDropsAndLosesNothing(t *testing.T) {
	h := newHarness(t)
	b, image := dockerBackend(t)
	person := h.addPerson(t, "member")
	runID := h.newRun(t, person, sleeper("20"), map[string]any{})
	box := spawnRunner(t, b, image, runID, h.bootstrap(t, runID))
	h.waitEvent(t, runID, "log", time.Minute)

	if err := b.Disconnect(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Second)
	if err := b.Reconnect(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	if status := h.waitStatus(t, runID, "succeeded", "failed"); status != "succeeded" {
		t.Fatalf("the run ended %s; events: %+v", status, h.events(t, runID))
	}
	events := h.events(t, runID)
	contiguous(t, events)
	if last := events[len(events)-1]; last.Type != "run.finished" || last.Payload["status"] != "succeeded" {
		t.Errorf("the last event = %+v", last)
	}
}

func TestARunnerThatStopsAnsweringIsLostAndPicksUpWhenItWakes(t *testing.T) {
	h := newHarness(t)
	b, image := dockerBackend(t)
	person := h.addPerson(t, "member")
	runID := h.newRun(t, person, sleeper("5"), map[string]any{})
	box := spawnRunner(t, b, image, runID, h.bootstrap(t, runID))
	h.waitEvent(t, runID, "log", time.Minute)

	if err := b.Pause(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	h.waitEvent(t, runID, "runner.lost", time.Minute)
	if err := b.Unpause(context.Background(), box); err != nil {
		t.Fatal(err)
	}
	if status := h.waitStatus(t, runID, "succeeded", "failed"); status != "succeeded" {
		t.Fatalf("after waking the run ended %s; events: %+v", status, h.events(t, runID))
	}
	contiguous(t, h.events(t, runID))
}
