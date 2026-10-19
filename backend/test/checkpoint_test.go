//go:build integration

package test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// resumable stops at an approval after calling the tool, and finishes in a code node once approved.
func resumable(tool string) string {
	return strings.Replace(conformanceWorkflow(tool), `  - {id: confirm, type: approval, message: 'Go on as {{ nodes.shape.who }}?'}`,
		`  - {id: confirm, type: approval, message: 'Go on as {{ nodes.shape.who }}?'}
  - id: finish
    type: code
    output: {schema: {type: object, required: [by]}}
    source: |
      def run(state, ctx):
          return {"by": state["nodes"]["shape"]["who"], "approved": state["nodes"]["confirm"]["approved"]}`, 1) +
		"  - {from: confirm, to: finish}\n"
}

// decrypted opens every value the run's checkpoints hold, as only OpenBao's transit key can.
func (h *harness) checkpointValues(t *testing.T, runID uuid.UUID) (sealed, opened []string) {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `SELECT checkpoint FROM run_checkpoint WHERE run_id = $1
		UNION ALL SELECT metadata FROM run_checkpoint WHERE run_id = $1
		UNION ALL SELECT value FROM run_checkpoint_write WHERE run_id = $1`, runID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		sealed = append(sealed, s)
	}
	rows.Close()
	root := rootBao(t)
	for _, s := range sealed {
		answer, err := root.Logical().Write("transit/decrypt/rotor-checkpoints", map[string]any{"ciphertext": s})
		if err != nil {
			t.Fatalf("decrypt a checkpoint value: %v", err)
		}
		plain, _ := base64.StdEncoding.DecodeString(answer.Data["plaintext"].(string))
		opened = append(opened, string(plain))
	}
	return sealed, opened
}

func TestAWaitingRunResumesInAFreshSandboxFromItsCheckpoint(t *testing.T) {
	h := newHarness(t)
	b, image := dockerBackend(t)
	product := "resume-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, bobID, _ := connected(t, stub)
	runID := h.newRun(t, bobID, resumable(product), map[string]any{})

	first := spawnRunner(t, b, image, runID, h.bootstrap(t, runID))
	h.waitStatus(t, runID, "waiting", "failed")
	// The sandbox is destroyed while the run waits, as an idle timeout will destroy it.
	if err := b.Destroy(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	approve(t, bob, runID.String())
	spawnRunner(t, b, image, runID, h.bootstrap(t, runID))
	if status := h.waitStatus(t, runID, "succeeded", "failed"); status != "succeeded" {
		t.Fatalf("the resumed run ended %s; events: %+v", status, h.events(t, runID))
	}

	events := h.events(t, runID)
	count := map[string]int{}
	for _, e := range events {
		count[e.Type+":"+e.Node]++
	}
	if count["node.started:ask"] != 1 || count["node.started:shape"] != 1 || count["node.started:finish"] != 1 || count["run.finished:"] != 2 {
		t.Errorf("the resumed run repeated or skipped work: %v", count)
	}
	last := events[len(events)-1]
	output, _ := last.Payload["output"].(map[string]any)
	finish, _ := output["finish"].(map[string]any)
	if last.Payload["status"] != "succeeded" || finish["by"] != "bob" || finish["approved"] != true {
		t.Errorf("the run finished with %+v", last.Payload)
	}
	if stats := stub.Stats(); stats["refreshes"] != float64(1) {
		t.Errorf("the tool minted %v tokens; the resumed run must not call it again", stats["refreshes"])
	}

	// The checkpoints are ciphertext at rest, and no token was ever in them.
	sealed, opened := h.checkpointValues(t, runID)
	if len(sealed) == 0 {
		t.Fatal("the run kept no checkpoint")
	}
	for i, s := range sealed {
		if !strings.HasPrefix(s, "vault:v1:") || strings.Contains(s, "bob") {
			t.Errorf("checkpoint value %d is not sealed: %.60s", i, s)
		}
	}
	joined := strings.Join(opened, "\n")
	if !strings.Contains(joined, "bob") {
		t.Error("the opened checkpoints do not hold the run's state; the check below would prove nothing")
	}
	for _, secret := range []string{"stub_oat_", "stub_ort_", "stub_ocs_", "Bearer "} {
		if strings.Contains(joined, secret) {
			t.Errorf("a checkpoint holds %q", secret)
		}
	}
}

func TestACheckpointLargerThanARunMayKeepFailsTheRun(t *testing.T) {
	h := newHarness(t)
	b, image := dockerBackend(t)
	person := h.addPerson(t, "member")
	runID := h.newRun(t, person, sleeper("1"), map[string]any{"blob": strings.Repeat("x", 5<<20)})
	spawnRunner(t, b, image, runID, h.bootstrap(t, runID))
	if status := h.waitStatus(t, runID, "succeeded", "failed"); status != "failed" {
		t.Fatalf("a run with a 5 MB state ended %s", status)
	}
	events := h.events(t, runID)
	if last := events[len(events)-1]; last.Payload["reason"] != "checkpoint_refused" {
		t.Errorf("the run failed with %+v, want checkpoint_refused", last.Payload)
	}
}
