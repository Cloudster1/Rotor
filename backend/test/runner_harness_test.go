//go:build integration

package test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/docker"
)

// runWait bounds how long a conformance run may take before the test gives up on it.
const runWait = 3 * time.Minute

// runEvent is a row of run_event, as the tests read them.
type runEvent struct {
	Seq     int64
	Type    string
	Node    string
	Payload map[string]any
}

// manualLease keeps the stack's worker away from a run a test drives itself, by holding the run's lease.
const manualLease = "test"

// newRun makes a workflow of one version and a run of it for person, behind the policies' back, already starting
// and leased to the test, so the test starts its sandbox and the stack's worker leaves it alone.
func (h *harness) newRun(t *testing.T, person uuid.UUID, yaml string, inputs map[string]any) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var workflowID, runID uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO workflow (name, owner_id) VALUES ('conformance', $1) RETURNING id`, person).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash) VALUES ($1, 1, $2, '{}', 'test')`,
		workflowID, yaml); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(inputs)
	if err := h.super.QueryRow(ctx, `INSERT INTO run (workflow_id, version, user_id, inputs, status, claimed_by, claimed_until)
		VALUES ($1, 1, $2, $3, 'starting', $4, now() + interval '1 day') RETURNING id`,
		workflowID, person, encoded, manualLease).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM run WHERE id = $1`, runID)
		_, _ = h.super.Exec(context.Background(), `DELETE FROM workflow WHERE id = $1`, workflowID)
	})
	return runID
}

// runnerCA is the CA sandboxes trust for the runner protocol, as the api will hand it to them.
func runnerCA(t *testing.T) string {
	t.Helper()
	secret, err := rootBao(t).Logical().Read("pki/cert/ca")
	if err != nil || secret == nil {
		t.Fatalf("read the runner CA: %v", err)
	}
	ca, _ := secret.Data["certificate"].(string)
	return ca
}

// spawnRunner starts the runner image for a run with bootstrap, as the api will at StartRun.
func spawnRunner(t *testing.T, b *docker.Backend, image string, runID uuid.UUID, bootstrap string) sandbox.ID {
	t.Helper()
	id, err := b.Spawn(context.Background(), sandbox.Spec{RunID: runID, Image: image,
		Labels: map[string]string{sandbox.LabelStack: "test-runner"},
		Env: map[string]string{"ROTOR_CONTROL_PLANE_ADDR": config.RunnerHost + config.DefaultRunnerAddr,
			"ROTOR_RUNNER_BOOTSTRAP": bootstrap, "ROTOR_RUNNER_CA": runnerCA(t), "ROTOR_OPENBAO_ADDR": os.Getenv(envOpenBao)},
		Limits: sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs, PIDs: config.DefaultSandboxPIDs}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Destroy(context.Background(), id) })
	return id
}

func (h *harness) bootstrap(t *testing.T, runID uuid.UUID) string {
	t.Helper()
	token, err := runnerctl.Bootstrap(context.Background(), h.cluster, runID)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// waitStatus waits until the run is in one of the states given, and answers it.
func (h *harness) waitStatus(t *testing.T, runID uuid.UUID, states ...string) string {
	t.Helper()
	deadline := time.Now().Add(runWait)
	var status string
	for time.Now().Before(deadline) {
		if err := h.super.QueryRow(context.Background(), `SELECT status FROM run WHERE id = $1`, runID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		for _, want := range states {
			if status == want {
				return status
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("run %s is %s after %s, want one of %v; events: %+v", runID, status, runWait, states, h.events(t, runID))
	return ""
}

// waitEvent waits for the run's first event of a type.
func (h *harness) waitEvent(t *testing.T, runID uuid.UUID, kind string, within time.Duration) runEvent {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, e := range h.events(t, runID) {
			if e.Type == kind {
				return e
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no %s event within %s; events: %+v", kind, within, h.events(t, runID))
	return runEvent{}
}

func (h *harness) events(t *testing.T, runID uuid.UUID) []runEvent {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `SELECT seq, type, node, payload FROM run_event WHERE run_id = $1 ORDER BY seq`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []runEvent
	for rows.Next() {
		var e runEvent
		var payload []byte
		if err := rows.Scan(&e.Seq, &e.Type, &e.Node, &payload); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(payload, &e.Payload)
		out = append(out, e)
	}
	return out
}

// contiguous fails unless the events are numbered 1, 2, 3 with no gap and no node started twice.
func contiguous(t *testing.T, events []runEvent) {
	t.Helper()
	started := map[string]bool{}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("event %d has seq %d: %+v", i, e.Seq, events)
		}
		if e.Type == "node.started" {
			if started[e.Node] {
				t.Fatalf("node %s started twice: %+v", e.Node, events)
			}
			started[e.Node] = true
		}
	}
}
