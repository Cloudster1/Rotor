//go:build integration

package test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	sealing "github.com/cloudster1/rotor/backend/internal/envelope"
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

// checkpointValues opens every value the run's checkpoints hold, as only the run's data key, which OpenBao's transit
// key wraps, can.
func (h *harness) checkpointValues(t *testing.T, runID uuid.UUID) (sealed [][]byte, opened []string) {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT 'c', checkpoint_ns || '|' || checkpoint_id, sealed_checkpoint FROM run_checkpoint WHERE run_id = $1
		UNION ALL SELECT 'm', checkpoint_ns || '|' || checkpoint_id, sealed_metadata FROM run_checkpoint WHERE run_id = $1
		UNION ALL SELECT 'w', checkpoint_ns || '|' || checkpoint_id || '|' || task_id || '|' || idx, sealed_value
			FROM run_checkpoint_write WHERE run_id = $1`, runID)
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]string{"c": sealing.ColumnCheckpoint, "m": sealing.ColumnMetadata, "w": sealing.ColumnWrite}
	type value struct {
		column, row string
		sealed      []byte
	}
	var values []value
	for rows.Next() {
		var v value
		if err := rows.Scan(&v.column, &v.row, &v.sealed); err != nil {
			t.Fatal(err)
		}
		values = append(values, v)
	}
	rows.Close()
	if len(values) == 0 {
		return nil, nil
	}
	box := h.runBox(t, runID)
	for _, v := range values {
		plain, err := box.Open(columns[v.column], v.row, v.sealed)
		if err != nil {
			t.Fatalf("open a checkpoint value: %v", err)
		}
		sealed, opened = append(sealed, v.sealed), append(opened, string(plain))
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
	// Sealed values open only with the run's key, as above, and lead with the format's version; random ciphertext
	// may spell any short word, so none is looked for.
	for i, s := range sealed {
		if len(s) == 0 || s[0] != 1 {
			t.Errorf("checkpoint value %d is not sealed: %s", i, describe(s))
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
