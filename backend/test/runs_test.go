//go:build integration

package test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// concurrentStarts is how many requests with one idempotency key arrive at once.
const concurrentStarts = 5

func TestAPersonStartsARunThatTheWorkerCarriesToItsEnd(t *testing.T) {
	h := newHarness(t)
	b, _ := dockerBackend(t)
	product := "greet-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, _ := connected(t, stub)
	workflowID := createWorkflow(t, bob, greeter(uniqueName("greet"), product))

	status, answer := bob.api(http.MethodGet, "/workflows/"+workflowID, nil)
	version, _ := answer["version"].(map[string]any)
	inputs, _ := version["inputs"].(map[string]any)
	bindings, _ := version["bindings"].([]any)
	if status != http.StatusOK || inputs["type"] != "object" || len(bindings) != 1 {
		t.Fatalf("the workflow's version offers inputs %v and bindings %v", version["inputs"], version["bindings"])
	}

	run := startRun(t, bob, workflowID, map[string]any{"greeting": "Hello"}, http.StatusCreated)
	runID, _ := run["id"].(string)
	if run["status"] != "queued" || run["ended"] != false {
		t.Errorf("a new run = %v", run)
	}
	done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
	if done["status"] != "succeeded" {
		t.Fatalf("the run ended %v: %v; events %+v", done["status"], done, h.events(t, uuid.MustParse(runID)))
	}
	output, _ := done["output"].(map[string]any)
	greet, _ := output["greet"].(map[string]any)
	if greet["text"] != "Hello, bob" || done["ended"] != true || done["startedAt"] == nil || done["finishedAt"] == nil {
		t.Errorf("the finished run = %v", done)
	}
	waitNoSandbox(t, b, runID)
	var credentials, torn int
	if err := h.super.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM runner_credential WHERE run_id = $1),
		(SELECT count(*) FROM run WHERE id = $1 AND torn_down_at IS NOT NULL)`, runID).Scan(&credentials, &torn); err != nil {
		t.Fatal(err)
	}
	if credentials != 0 || torn != 1 {
		t.Errorf("after the run, %d credentials are left and torn down is %d", credentials, torn)
	}
	if !h.audited(t, "run.started", runID) {
		t.Error("the audit log does not record run.started for the run")
	}

	status, answer = bob.api(http.MethodGet, "/runs?mine=true&status=succeeded&workflow="+workflowID, nil)
	listed, _ := answer["runs"].([]any)
	if status != http.StatusOK || len(listed) != 1 {
		t.Errorf("bob's succeeded runs of the workflow = %d %v", status, answer)
	}
}

func TestAStartIsRefusedForInputsTheSchemaRefusesAndToolsNotConnected(t *testing.T) {
	newHarness(t)
	product := "refuse-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, toolID := connected(t, stub)
	workflowID := createWorkflow(t, bob, greeter(uniqueName("refuse"), product))
	share(t, bob, workflowID)

	answer := startRun(t, bob, workflowID, map[string]any{"greeting": ""}, http.StatusUnprocessableEntity)
	refusal, _ := answer["error"].(map[string]any)
	fields, _ := refusal["fields"].(map[string]any)
	if refusal["code"] != "inputs_invalid" || fields["inputs/greeting"] == nil {
		t.Errorf("an empty greeting = %v", answer)
	}
	answer = startRun(t, bob, workflowID, map[string]any{}, http.StatusUnprocessableEntity)
	if refusal, _ = answer["error"].(map[string]any); refusal["code"] != "inputs_invalid" {
		t.Errorf("no greeting at all = %v", answer)
	}

	// carol sees the shared workflow but is not connected to the tool it binds.
	carol := newBrowser(t)
	carol.signIn("carol")
	answer = startRun(t, carol, workflowID, map[string]any{"greeting": "Hi"}, http.StatusConflict)
	refusal, _ = answer["error"].(map[string]any)
	fields, _ = refusal["fields"].(map[string]any)
	if refusal["code"] != "connections_needed" || fields[product] != toolID.String() {
		t.Errorf("carol's start without a connection = %v", answer)
	}
	if status, answer := carol.api(http.MethodGet, "/runs?workflow="+workflowID, nil); status != http.StatusOK || len(answer["runs"].([]any)) != 0 {
		t.Errorf("a refused start left runs: %v", answer)
	}
}

// audited says whether the audit log holds an entry of action about target.
func (h *harness) audited(t *testing.T, action, target string) bool {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1 AND target_id = $2`, action, target).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// share shares a workflow with everyone, as its owner.
func share(t *testing.T, owner *browser, workflowID string) {
	t.Helper()
	if status, answer := owner.api(http.MethodPut, "/workflows/"+workflowID+"/sharing", map[string]any{"sharing": "everyone", "groups": []string{}}); status != http.StatusOK {
		t.Fatalf("sharing the workflow = %d %v", status, answer)
	}
}

func TestAnIdempotencyKeyStartsOneRunEvenWhenSentAtOnce(t *testing.T) {
	newHarness(t)
	alice := newBrowser(t)
	alice.signIn("alice")
	first := createWorkflow(t, alice, approvalOnly(uniqueName("once"), "Go on?"))
	other := createWorkflow(t, alice, approvalOnly(uniqueName("other"), "Go on?"))
	key := map[string]string{"Idempotency-Key": "start-" + uuid.NewString()}

	var mu sync.Mutex
	statuses, ids := map[int]int{}, map[string]bool{}
	var wg sync.WaitGroup
	for range concurrentStarts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, answer := alice.apiWith(http.MethodPost, "/workflows/"+first+"/runs", map[string]any{"inputs": map[string]any{}}, key)
			mu.Lock()
			defer mu.Unlock()
			statuses[status]++
			if id, ok := answer["id"].(string); ok {
				ids[id] = true
			}
		}()
	}
	wg.Wait()
	if statuses[http.StatusCreated] != 1 || statuses[http.StatusOK] != concurrentStarts-1 || len(ids) != 1 {
		t.Errorf("%d starts with one key answered %v for runs %v; want one 201 and the rest 200 for one run", concurrentStarts, statuses, ids)
	}
	status, answer := alice.apiWith(http.MethodPost, "/workflows/"+other+"/runs", map[string]any{"inputs": map[string]any{}}, key)
	if refusal, _ := answer["error"].(map[string]any); status != http.StatusUnprocessableEntity || refusal["fields"] == nil {
		t.Errorf("the key on another workflow = %d %v", status, answer)
	}
	for id := range ids {
		alice.api(http.MethodPost, "/runs/"+id+"/cancel", nil)
	}
}

func TestARunIsSeenAndCancelledByItsPersonTheOwnerAndAdministratorsAlone(t *testing.T) {
	h := newHarness(t)
	b, _ := dockerBackend(t)
	alice, bob, carol := newBrowser(t), newBrowser(t), newBrowser(t)
	alice.signIn("alice")
	bob.signIn("bob")
	carol.signIn("carol")
	// bob owns the workflow, carol runs it, alice administers Rotor and dave is a stranger to both.
	workflowID := createWorkflow(t, bob, sleeper("60"))
	share(t, bob, workflowID)
	run := startRun(t, carol, workflowID, map[string]any{}, http.StatusCreated)
	runID, _ := run["id"].(string)
	waitRun(t, carol, runID, "running")
	h.waitEvent(t, uuid.MustParse(runID), "log", sandboxWait)

	stranger := newBrowser(t)
	stranger.signIn("dave")
	if status, _ := stranger.api(http.MethodGet, "/runs/"+runID, nil); status != http.StatusNotFound {
		t.Errorf("dave reads carol's run: %d, want 404", status)
	}
	if status, _ := stranger.api(http.MethodPost, "/runs/"+runID+"/cancel", nil); status != http.StatusNotFound {
		t.Errorf("dave cancels carol's run: %d, want 404", status)
	}
	if status, answer := alice.api(http.MethodGet, "/runs?workflow="+workflowID, nil); status != http.StatusOK || len(answer["runs"].([]any)) != 1 {
		t.Errorf("alice, an administrator, lists %v", answer)
	}
	// bob owns the workflow, so he may cancel carol's run of it.
	status, cancelled := bob.api(http.MethodPost, "/runs/"+runID+"/cancel", nil)
	if status != http.StatusOK || cancelled["status"] != "cancelled" || cancelled["ended"] != true {
		t.Fatalf("the owner cancelling = %d %v", status, cancelled)
	}
	if status, again := carol.api(http.MethodPost, "/runs/"+runID+"/cancel", nil); status != http.StatusConflict {
		t.Errorf("cancelling an ended run = %d %v, want 409", status, again)
	}
	waitNoSandbox(t, b, runID)
	events := h.events(t, uuid.MustParse(runID))
	last := events[len(events)-1]
	if last.Type != "run.finished" || last.Payload["status"] != "cancelled" {
		t.Errorf("the last event = %+v, want run.finished cancelled", last)
	}
	for _, e := range events {
		if e.Type == "node.finished" && e.Node == "wait" {
			t.Errorf("the cancelled node finished anyway: %+v", e)
		}
	}
	if !h.audited(t, "run.cancelled", runID) {
		t.Error("the audit log does not record run.cancelled")
	}
}

func TestConnectingFromAWorkflowComesBackToIt(t *testing.T) {
	newHarness(t)
	stub := newToolStub(t, nil)
	toolID := addTool(t, stub)
	bob := newBrowser(t)
	bob.signIn("bob")
	landed := bob.landing(t, bob.consentFrom(t, toolID, "bob", "allow", "/workflows/abc"))
	if landed != "/workflows/abc?connected="+toolID {
		t.Errorf("connecting from a workflow landed on %q", landed)
	}
	stub2 := newToolStub(t, nil)
	other := addTool(t, stub2)
	if landed := bob.landing(t, bob.consentFrom(t, other, "bob", "allow", "https://evil.example/")); landed != "/connections?connected="+other {
		t.Errorf("a foreign next landed on %q, want the Connections page", landed)
	}
}
