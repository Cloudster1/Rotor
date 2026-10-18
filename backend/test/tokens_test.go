//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
)

// slowCaller works a while in a code node, then calls the tool, so a test can end the person's access meanwhile.
func slowCaller(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("slow") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: wait
nodes:
  - id: wait
    type: code
    output: {schema: {type: object}}
    source: |
      import time
      def run(state, ctx):
          print("waiting")
          time.sleep(8)
          return {}
  - {id: ask, type: tool, tool: t, call: whoami}
edges:
  - {from: wait, to: ask}
`
}

// unwrap opens a wrapping token at OpenBao, as a sandbox does, with no credential but the token itself.
func unwrap(t *testing.T, token string) (map[string]any, error) {
	t.Helper()
	client := rootBao(t)
	clone, err := client.Clone()
	if err != nil {
		t.Fatal(err)
	}
	clone.SetToken(token)
	secret, err := clone.Logical().Unwrap("")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, errors.New("OpenBao unwrapped nothing")
	}
	return secret.Data, nil
}

func TestARunsTokenArrivesWrappedOpensOnceAndIsRevokedWithTheRun(t *testing.T) {
	h := newHarness(t)
	product := "wrap-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	_, bobID, toolID := connected(t, stub)
	runID := h.newRun(t, bobID, conformanceWorkflow(product), map[string]any{})
	plane := controlPlane(t, h, nil)
	real, _ := newVault(t, os.Getenv(envOpenBao), config.DefaultOpenBaoRole)
	plane.Runs = real
	ctx := context.Background()

	delivered, err := plane.Deliver(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, RunID: runID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(delivered.WrappingToken, "stub_") {
		t.Fatalf("the delivery is the token itself: %.12s", delivered.WrappingToken)
	}
	opened, err := unwrap(t, delivered.WrappingToken)
	if err != nil {
		t.Fatalf("unwrap the delivery: %v", err)
	}
	token, _ := opened["access_token"].(string)
	if status, who := whoami(t, stub, token); status != http.StatusOK || who != "bob" {
		t.Fatalf("the unwrapped token acts as %q (%d)", who, status)
	}
	if _, err := unwrap(t, delivered.WrappingToken); err == nil {
		t.Error("the wrapping token opened a second time")
	}
	if where := h.find(t, token); where != "" {
		t.Errorf("the token is stored in %s", where)
	}

	if err := plane.RevokeRun(ctx, runID); err != nil {
		t.Fatal(err)
	}
	if status, _ := whoami(t, stub, token); status != http.StatusUnauthorized {
		t.Errorf("the run's token still works after the run: %d", status)
	}
	var unrevoked int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM run_token WHERE run_id = $1 AND revoked_at IS NULL`, runID).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Errorf("%d of the run's tokens are left unrevoked (%v)", unrevoked, err)
	}
	if secret, err := rootBao(t).Logical().List("rotor/metadata/runs/" + runID.String() + "/tokens"); err != nil || (secret != nil && len(secret.Data["keys"].([]any)) > 0) {
		t.Errorf("OpenBao still holds the run's tokens: %v %v", secret, err)
	}
	if !h.audited(t, "token.revoked", runID.String()) {
		t.Error("the audit log does not record token.revoked")
	}

	// A run that ended, and a person switched off, get no token at all.
	if _, err := h.super.Exec(ctx, `UPDATE run SET status = 'running' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, bobID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, bobID)
	})
	if _, err := plane.Deliver(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, RunID: runID.String()}); !errors.Is(err, connections.ErrSwitchedOff) {
		t.Errorf("a switched-off person's run got %v", err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = true WHERE id = $1`, bobID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `UPDATE run SET status = 'failed' WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := plane.Deliver(ctx, connections.MintRequest{UserID: bobID, ToolID: toolID, RunID: runID.String()}); !errors.Is(err, connections.ErrRunNotLive) {
		t.Errorf("an ended run got %v", err)
	}
}

func TestATokenNeverReachesARunsEventsOrCheckpointsAndEndsWithTheRun(t *testing.T) {
	h := newHarness(t)
	product := "secret-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	live := stub.Stats()["liveAccessTokens"]
	workflowID := createWorkflow(t, bob, resumable(product))
	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	runID := uuid.MustParse(run["id"].(string))
	waitRun(t, bob, runID.String(), "waiting", "failed")
	cancelled := waitCancel(t, bob, runID.String())
	if cancelled["status"] != "cancelled" {
		t.Fatalf("cancelling = %v", cancelled)
	}

	for _, e := range h.events(t, runID) {
		text := stringOf(e.Payload)
		for _, secret := range []string{"stub_oat_", "stub_ort_", "Bearer "} {
			if strings.Contains(text, secret) {
				t.Errorf("event %s holds %q", e.Type, secret)
			}
		}
	}
	_, opened := h.checkpointValues(t, runID)
	for _, value := range opened {
		if strings.Contains(value, "stub_oat_") {
			t.Error("a checkpoint holds the run's token")
		}
	}
	waitTornDown(t, h, runID)
	if now := stub.Stats()["liveAccessTokens"]; now != live {
		t.Errorf("the tool holds %v live access tokens after the run, %v before it", now, live)
	}
}

// waitCancel cancels a run as b and answers it.
func waitCancel(t *testing.T, b *browser, runID string) map[string]any {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/runs/"+runID+"/cancel", nil)
	if status != http.StatusOK {
		t.Fatalf("cancelling run %s = %d %v", runID, status, answer)
	}
	return answer
}

// waitTornDown waits until the worker cleaned up after the run, its tokens revoked.
func waitTornDown(t *testing.T, h *harness, runID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(sandboxWait)
	for time.Now().Before(deadline) {
		var torn bool
		if err := h.super.QueryRow(context.Background(), `SELECT torn_down_at IS NOT NULL FROM run WHERE id = $1`, runID).Scan(&torn); err != nil {
			t.Fatal(err)
		}
		if torn {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("the worker did not tear run %s down within %s", runID, sandboxWait)
}

func TestASwitchedOffPersonsRunGetsNoNewTokenAndSaysSo(t *testing.T) {
	h := newHarness(t)
	product := "off-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, bobID, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, slowCaller(product))
	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	runID := uuid.MustParse(run["id"].(string))
	h.waitEvent(t, runID, "log", sandboxWait)
	before := stub.Stats()["refreshes"]
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = false WHERE id = $1`, bobID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, bobID)
	})
	if status := h.waitStatus(t, runID, "failed", "succeeded"); status != "failed" {
		t.Fatalf("a switched-off person's run ended %s", status)
	}
	events := h.events(t, runID)
	last := events[len(events)-1]
	if !strings.Contains(stringOf(last.Payload), "switched off") {
		t.Errorf("the run ended without saying why: %+v", last.Payload)
	}
	if after := stub.Stats()["refreshes"]; after != before {
		t.Errorf("the tool minted %v tokens after the person was switched off", after.(float64)-before.(float64))
	}
}
