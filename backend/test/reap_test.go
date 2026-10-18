//go:build integration

package test

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/runnerctl"
)

// envWorkerMetrics is the worker's /metrics, on the stack's network.
const envWorkerMetrics = "ROTOR_TEST_WORKER_METRICS_URL"

// askTwice calls the tool, waits for approval, and calls it again: the second call needs a token minted after
// the wait.
func askTwice(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("ask-twice") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - {id: confirm, type: approval, message: 'Ask again?'}
  - {id: again, type: tool, tool: t, call: whoami}
edges:
  - {from: ask, to: confirm}
  - {from: confirm, to: again}
`
}

// waitRevoked waits until every token the run was handed is revoked.
func waitRevoked(t *testing.T, h *harness, runID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(sandboxWait)
	for time.Now().Before(deadline) {
		var handed, unrevoked int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE revoked_at IS NULL)
			FROM run_token WHERE run_id = $1`, runID).Scan(&handed, &unrevoked); err != nil {
			t.Fatal(err)
		}
		if handed > 0 && unrevoked == 0 {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("the tokens of run %s were not revoked within %s", runID, sandboxWait)
}

// reaped reads how many sandboxes the worker counts as reaped for reason.
func reaped(t *testing.T, reason string) float64 {
	t.Helper()
	address := os.Getenv(envWorkerMetrics)
	if address == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envWorkerMetrics)
	}
	resp, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	want := `rotor_sandboxes_reaped_total{reason="` + reason + `"} `
	lines := bufio.NewScanner(resp.Body)
	for lines.Scan() {
		if value, ok := strings.CutPrefix(lines.Text(), want); ok {
			n, _ := strconv.ParseFloat(value, 64)
			return n
		}
	}
	return 0
}

func TestAWaitingRunKeepsNoSandboxNorTokenAndResumesWithFreshOnes(t *testing.T) {
	h := newHarness(t)
	b, _ := dockerBackend(t)
	product := "reap-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	live := stub.Stats()["liveAccessTokens"]
	before := reaped(t, "waiting")
	workflowID := createWorkflow(t, bob, askTwice(product))
	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	runID := uuid.MustParse(run["id"].(string))

	waitRun(t, bob, runID.String(), "waiting")
	waitNoSandbox(t, b, runID.String())
	waitRevoked(t, h, runID)
	if now := stub.Stats()["liveAccessTokens"]; now != live {
		t.Errorf("the tool holds %v live access tokens while the run waits, %v before it", now, live)
	}
	if !h.audited(t, "token.revoked", runID.String()) {
		t.Error("the audit log does not record the waiting run's token as revoked")
	}
	if after := reaped(t, "waiting"); after < before+1 {
		t.Errorf("the worker counts %v sandboxes reaped for waiting, %v before the run", after, before)
	}

	minted := stub.Stats()["refreshes"].(float64)
	if err := runnerctl.Resume(context.Background(), h.cluster, runID, []byte(`{"approved": true}`)); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, bob, runID.String(), "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("the resumed run ended %v; events %+v", done["status"], h.events(t, runID))
	}
	output, _ := done["output"].(map[string]any)
	again, _ := output["again"].(map[string]any)
	user, _ := again["user"].(map[string]any)
	if user["name"] != "bob" {
		t.Errorf("the call after the wait answered %v", output)
	}
	if now := stub.Stats()["refreshes"].(float64); now <= minted {
		t.Errorf("the resumed run called the tool without a fresh token: %v minted before, %v after", minted, now)
	}
}
