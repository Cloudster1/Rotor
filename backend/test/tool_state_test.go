//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The true state of a tool's client and of each grant, said alike by the picker, the run's start, Admin, Tools and
// Tool accounts (#266).

// connectionTo is the person's connection to one tool, so a stack with other tools' leftovers does not matter (#269).
func connectionTo(t *testing.T, b *browser, toolID uuid.UUID) map[string]any {
	t.Helper()
	status, answer := b.api(http.MethodGet, "/connections", nil)
	if status != http.StatusOK {
		t.Fatalf("listing the connections = %d %v", status, answer)
	}
	list, _ := answer["connections"].([]any)
	for _, item := range list {
		c, _ := item.(map[string]any)
		if tool, _ := c["tool"].(map[string]any); tool["id"] == toolID.String() {
			return c
		}
	}
	return nil
}

// refusedWith asserts an answer is a 409 with the code given, and answers its message.
func refusedWith(t *testing.T, what string, status int, answer map[string]any, code string) string {
	t.Helper()
	e, _ := answer["error"].(map[string]any)
	if status != http.StatusConflict || e["code"] != code {
		t.Fatalf("%s = %d %v, want 409 %s", what, status, answer, code)
	}
	message, _ := e["message"].(string)
	return message
}

func toolStatus(t *testing.T, b *browser, toolID uuid.UUID) string {
	t.Helper()
	status, answer := b.api(http.MethodGet, "/tools/"+toolID.String(), nil)
	if status != http.StatusOK {
		t.Fatalf("reading the tool = %d %v", status, answer)
	}
	s, _ := toolOf(t, answer)["status"].(string)
	return s
}

func TestTheCatalogAsksToConnectAgainWhenTheToolEndedTheGrant(t *testing.T) {
	newHarness(t)
	stub := newToolStub(t, nil)
	bob, _, toolID := connected(t, stub)
	stub.EndGrants("bob")

	status, answer := bob.api(http.MethodGet, "/tools/"+toolID.String()+"/catalog", nil)
	message := refusedWith(t, "the catalog after the tool ended the grant", status, answer, "reconnect")
	if !strings.Contains(message, "Connect your account again") {
		t.Errorf("the reconnect sentence %q does not say what to do", message)
	}
	if c := connectionTo(t, bob, toolID); c == nil || c["status"] != "reconnect" {
		t.Errorf("bob's card after the catalog = %v, want reconnect as the picker said", c)
	}
	alice := signedIn(t, "alice")
	if s := toolStatus(t, alice, toolID); s != "ready" {
		t.Errorf("the tool after one grant ended = %s, want ready: only bob connects again", s)
	}
}

func TestAToolThatForgotRotorIsSaidSoEverywhereAndRegisteringAgainAsksEveryoneToConnectAgain(t *testing.T) {
	h := newHarness(t)
	product := "forgot-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, bobID, toolID := connected(t, stub)
	workflowID := createWorkflow(t, bob, greeter(uniqueName("forgot"), product))
	stub.ForgetClients()

	// Nothing told Rotor yet: the start's own read of the tool finds out, and no run starts.
	answer := startRun(t, bob, workflowID, map[string]any{"greeting": "Hello"}, http.StatusConflict)
	message := refusedWith(t, "starting a run on a tool that forgot Rotor", http.StatusConflict, answer, "needs_registration")
	fields, _ := answer["error"].(map[string]any)["fields"].(map[string]any)
	if fields[product] != toolID.String() || !strings.Contains(message, "ask an administrator to register it again") {
		t.Errorf("the refused start = %v", answer)
	}
	if status, answer := bob.api(http.MethodGet, "/runs?workflow="+workflowID, nil); status != http.StatusOK || len(answer["runs"].([]any)) != 0 {
		t.Errorf("a refused start left runs: %v", answer)
	}
	alice := signedIn(t, "alice")
	if s := toolStatus(t, alice, toolID); s != "needs_registration" {
		t.Errorf("Admin, Tools after the refused start says %s, want needs_registration", s)
	}

	status, answer := bob.api(http.MethodGet, "/tools/"+toolID.String()+"/catalog", nil)
	message = refusedWith(t, "the catalog of a tool that forgot Rotor", status, answer, "needs_registration")
	if !strings.Contains(message, "administrator") || !strings.Contains(message, "Admin, Tools") {
		t.Errorf("the needs_registration sentence %q does not say who fixes it where", message)
	}

	status, answer = alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusOK || toolOf(t, answer)["status"] != "ready" {
		t.Fatalf("registering again = %d %v", status, answer)
	}
	if c := connectionTo(t, bob, toolID); c == nil || c["status"] != "reconnect" {
		t.Errorf("bob's card after registering again = %v, want reconnect", c)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/connections/" + bobID.String() + "/" + toolID.String()); err != nil || secret != nil {
		t.Errorf("the old client's refresh token is still in OpenBao: %v %v", secret, err)
	}
	var ended int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'connection.ended'
		AND target_id = $1 AND data->>'by' = 'registered_again'`, toolID.String()).Scan(&ended); err != nil || ended != 1 {
		t.Errorf("connection.ended by registered_again = %d %v, want 1", ended, err)
	}
	status, answer = bob.api(http.MethodGet, "/tools/"+toolID.String()+"/catalog", nil)
	refusedWith(t, "the catalog once registered again", status, answer, "reconnect")
	answer = startRun(t, bob, workflowID, map[string]any{"greeting": "Hello"}, http.StatusConflict)
	refusedWith(t, "starting a run once registered again", http.StatusConflict, answer, "connections_needed")
}

func TestCheckAgainNoticesAToolThatForgotRotorsClient(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	bob, _, toolID := connected(t, stub)
	alice := signedIn(t, "alice")
	check := "/tools/" + toolID.String() + "/check"

	before := stub.Stats()
	status, answer := alice.api(http.MethodPost, check, nil)
	if status != http.StatusOK || toolOf(t, answer)["status"] != "ready" {
		t.Fatalf("checking a tool that knows Rotor = %d %v", status, answer)
	}
	if after := stub.Stats(); after["liveGrants"] != before["liveGrants"] || after["reuseEvents"] != before["reuseEvents"] {
		t.Errorf("checking touched bob's grant: %v then %v", before, after)
	}
	if c := connectionTo(t, bob, toolID); c == nil || c["status"] != "connected" {
		t.Errorf("bob's card after a check = %v, want connected", c)
	}

	stub.ForgetClients()
	status, answer = alice.api(http.MethodPost, check, nil)
	if status != http.StatusOK || toolOf(t, answer)["status"] != "needs_registration" {
		t.Fatalf("checking a tool that forgot Rotor = %d %v, want needs_registration", status, answer)
	}
	if s := toolStatus(t, bob, toolID); s != "needs_registration" {
		t.Errorf("a member reads the tool as %s after the check", s)
	}
	var recorded string
	if err := h.super.QueryRow(context.Background(), `SELECT data->>'status' FROM audit_log WHERE action = 'tool.checked'
		AND target_id = $1 ORDER BY created_at DESC LIMIT 1`, toolID.String()).Scan(&recorded); err != nil || recorded != "needs_registration" {
		t.Errorf("tool.checked records status %q %v", recorded, err)
	}
	status, answer = bob.api(http.MethodGet, "/tools/"+toolID.String()+"/catalog", nil)
	refusedWith(t, "the catalog after the check", status, answer, "needs_registration")
}

// heldThenAsked waits for an approval before it asks the tool anything, so a grant can end while the run waits.
func heldThenAsked(tool string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("held") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 5m}
entry: hold
nodes:
  - {id: hold, type: approval, message: Go on}
  - {id: ask, type: tool, tool: t, call: whoami}
edges:
  - {from: hold, to: ask}
`
}

func TestARunWhoseGrantEndedSaysToConnectAgainWithoutTheToolsInternalAddress(t *testing.T) {
	h := newHarness(t)
	dockerBackend(t)
	product := "ended-" + uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product})
	bob, _, toolID := connected(t, stub)
	workflowID := createWorkflow(t, bob, heldThenAsked(product))

	run := startRun(t, bob, workflowID, map[string]any{}, http.StatusCreated)
	runID, _ := run["id"].(string)
	approvalID := approvalOf(t, bob, runID)
	// The tool ends the grant on its own while the run waits; Rotor learns it only when the run asks for a token.
	stub.EndGrants("bob")
	decide(t, bob, approvalID, map[string]any{"decision": "approve"}, http.StatusOK)
	done := waitRun(t, bob, runID, "succeeded", "failed", "cancelled")
	events := h.events(t, uuid.MustParse(runID))
	said, _ := json.Marshal(map[string]any{"run": done, "events": events})
	if done["status"] != "failed" {
		t.Fatalf("the run ended %v: %s", done["status"], said)
	}
	internal := os.Getenv(envToolStubInternal)
	if internal == "" || strings.Contains(string(said), internal) || strings.Contains(string(said), strings.TrimPrefix(internal, "http://")) {
		t.Errorf("the run names the tool's internal address %q: %s", internal, said)
	}
	if strings.Contains(string(said), "cannot be reached") || !strings.Contains(string(said), product+" ended your connection") {
		t.Errorf("the run does not say the grant ended: %s", said)
	}
	if c := connectionTo(t, bob, toolID); c == nil || c["status"] != "reconnect" {
		t.Errorf("bob's card after the run = %v, want reconnect", c)
	}
}
