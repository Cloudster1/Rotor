//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// queuedWait is how long a run stays queued with the worker stopped before the test believes nothing takes it up.
const queuedWait = 3 * time.Second

// checkpointed carries the tool's answer through a checkpoint, then works long enough for a test to kill it.
func checkpointed(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("checkpointed") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - id: work
    type: code
    timeout: 2m
    output: {schema: {type: object}}
    source: |
      import time
      def run(state, ctx):
          print("working")
          time.sleep(20)
          return {"by": state["nodes"]["ask"]["user"]["name"]}
edges:
  - {from: ask, to: work}
`
}

func TestTheRunStateMachineRefusesMovesItDoesNotKnow(t *testing.T) {
	h := newHarness(t)
	person := h.addPerson(t, "member")
	runID := h.newRun(t, person, sleeper("1"), map[string]any{})
	ctx := context.Background()
	admin := func(statement string) error {
		return h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, statement, runID)
			return err
		})
	}
	if err := admin(`UPDATE run SET status = 'succeeded' WHERE id = $1`); err == nil {
		t.Error("a starting run moved straight to succeeded")
	}
	if err := admin(`UPDATE run SET status = 'running' WHERE id = $1`); err != nil {
		t.Fatalf("starting to running = %v", err)
	}
	if err := admin(`UPDATE run SET status = 'failed' WHERE id = $1`); err != nil {
		t.Fatalf("running to failed = %v", err)
	}
	for _, back := range []string{"running", "queued", "succeeded", "waiting"} {
		if err := admin(`UPDATE run SET status = '` + back + `' WHERE id = $1`); err == nil {
			t.Errorf("a failed run moved on to %s", back)
		}
	}
	var finished *time.Time
	if err := h.super.QueryRow(ctx, `SELECT finished_at FROM run WHERE id = $1`, runID).Scan(&finished); err != nil || finished == nil {
		t.Errorf("a failed run has no finished_at (%v)", err)
	}

	// A person may only add their own queued runs, and change none.
	as := func(statement string, args ...any) error {
		return h.cluster.Write(db.WithUser(ctx, person), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, statement, args...)
			return err
		})
	}
	permissionDenied(t, "a person moving their run as rotor_app", as(`UPDATE run SET status = 'cancelled' WHERE id = $1`, runID))
	var workflowID uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT workflow_id FROM run WHERE id = $1`, runID).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if err := as(`INSERT INTO run (workflow_id, version, user_id, status) VALUES ($1, 1, $2, 'running')`, workflowID, person); err == nil {
		t.Error("a person added a run that is already running")
	}
	other := h.addPerson(t, "member")
	if err := as(`INSERT INTO run (workflow_id, version, user_id) VALUES ($1, 1, $2)`, workflowID, other); err == nil {
		t.Error("a person added a run as someone else")
	}
}

func TestARunWaitsForAStoppedWorkerAndOneWhoseWorkerDiedIsTakenUp(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	workflowID := createWorkflow(t, alice, sleeper("1"))

	start := stopWorker(t)
	run := startRun(t, alice, workflowID, map[string]any{}, http.StatusCreated)
	runID, _ := run["id"].(string)
	// A run leased by a worker that died stays put until the lease runs out.
	if _, err := h.super.Exec(context.Background(), `UPDATE run SET claimed_by = 'gone', claimed_until = now() + interval '3 seconds' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(queuedWait)
	if _, now := alice.api(http.MethodGet, "/runs/"+runID, nil); now["status"] != "queued" {
		t.Fatalf("with the worker stopped the run is %v", now["status"])
	}
	start()
	if done := waitRun(t, alice, runID, "succeeded", "failed"); done["status"] != "succeeded" {
		t.Errorf("after the worker came back the run ended %v: %v", done["status"], done)
	}
}

func TestALostSandboxIsReplacedAndTheRunResumesFromItsCheckpoint(t *testing.T) {
	h := newHarness(t)
	b, _ := dockerBackend(t)
	product := "lost-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, checkpointed(product))
	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	runID := uuid.MustParse(run["id"].(string))
	h.waitEvent(t, runID, "log", sandboxWait)

	var first string
	if err := h.super.QueryRow(context.Background(), `SELECT sandbox_id FROM run WHERE id = $1`, runID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := b.Destroy(context.Background(), sandbox.ID(first)); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, bob, runID.String(), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v after its sandbox was lost; events %+v", done["status"], h.events(t, runID))
	}
	count := map[string]int{}
	replaced := false
	for _, e := range h.events(t, runID) {
		count[e.Type+":"+e.Node]++
		if e.Type == "runner.lost" && e.Payload["replaced"] == true {
			replaced = true
		}
	}
	if count["node.started:ask"] != 1 || count["tool.called:ask"] != 1 || count["node.started:work"] != 2 || !replaced {
		t.Errorf("the replacement repeated or skipped work, or said nothing: %v (replaced %v)", count, replaced)
	}
	var attempts int
	if err := h.super.QueryRow(context.Background(), `SELECT attempts FROM run WHERE id = $1`, runID).Scan(&attempts); err != nil || attempts != 2 {
		t.Errorf("the run was started in %d sandboxes (%v), want 2", attempts, err)
	}
}

func TestARunPastItsDeadlineIsEndedAndLosesItsSandbox(t *testing.T) {
	h := newHarness(t)
	b, _ := dockerBackend(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	workflowID := createWorkflow(t, alice, sleeper("60"))
	run := startRun(t, alice, workflowID, map[string]any{}, http.StatusCreated)
	runID, _ := run["id"].(string)
	h.waitEvent(t, uuid.MustParse(runID), "log", sandboxWait)
	// The runner keeps its own timeout; the worker's deadline is for a runner that cannot.
	if _, err := h.super.Exec(context.Background(), `UPDATE run SET deadline_at = now() - interval '1 second' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, alice, runID, "failed", "succeeded")
	if done["status"] != "failed" || done["reason"] != runs.ReasonTimeout {
		t.Errorf("a run past its deadline = %v", done)
	}
	waitNoSandbox(t, b, runID)
}

func TestASandboxThatNeverSaysHelloIsReplacedUntilTheRunFails(t *testing.T) {
	h := newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	workflowID := createWorkflow(t, alice, sleeper("1"))
	stopWorker(t)
	run := startRun(t, alice, workflowID, map[string]any{}, http.StatusCreated)
	runID := uuid.MustParse(run["id"].(string))

	fake := newFakeSandboxes()
	worker := runs.NewWorker(h.cluster, fake, runs.Launch{Image: "never-starts", Dial: "nowhere:1", Stack: "test-fake",
		CA: func(context.Context) (string, error) { return "a CA", nil }}, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	for attempt := 1; attempt <= config.MaxRunAttempts; attempt++ {
		if err := worker.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if fake.count() != attempt {
			t.Fatalf("after pass %d the worker started %d sandboxes", attempt, fake.count())
		}
		// The sandbox says nothing for longer than it may.
		if _, err := h.super.Exec(ctx, `UPDATE run SET sandbox_started_at = now() - interval '1 hour' WHERE id = $1`, runID); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.Once(ctx); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, alice, runID.String(), "failed")
	if done["reason"] != runs.ReasonSandboxStart || fake.count() != config.MaxRunAttempts {
		t.Errorf("after %d silent sandboxes the run = %v", fake.count(), done)
	}
	if err := worker.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if left, _ := fake.List(ctx, nil); len(left) != 0 {
		t.Errorf("sandboxes outlived the failed run: %+v", left)
	}
	spec := fake.spawned[0]
	if spec.Env["ROTOR_RUNNER_BOOTSTRAP"] == "" || spec.Env["ROTOR_CONTROL_PLANE_ADDR"] != "nowhere:1" || spec.Env["ROTOR_RUNNER_CA"] != "a CA" {
		t.Errorf("a sandbox started with %v", spec.Env)
	}
}
