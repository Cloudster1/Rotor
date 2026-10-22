//go:build integration && suite

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// thinkingDelay holds the first draft back, long enough for the test to stop the worker while the run thinks.
const thinkingDelay = 8 * time.Second

// suiteBrowser signs username in to Rotor and to the suite's Exciter, both through Rotor's Keycloak; it reaches the
// suite's Armature too, where signInToArmature signs them in.
func suiteBrowser(t *testing.T, username, base string) *browser {
	t.Helper()
	b := newBrowser(t)
	b.signIn(username)
	b.client.Transport = oidc.HTTPClient(map[string]string{
		os.Getenv(envKeycloakPublic): os.Getenv(envKeycloak), os.Getenv(envApp): apiURL(t), base: os.Getenv(envExciterInternal),
		os.Getenv(envArmature): os.Getenv(envArmatureInternal),
	}).Transport
	b.follow(t, base+"/api/v1/auth/oidc/start?next=/", username)
	return b
}

// atExciter calls the suite's Exciter as the browser's Exciter session.
func (b *browser) atExciter(t *testing.T, base, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, _ := http.NewRequest(method, base+"/api/v1"+path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp, text := b.do(req)
	out := map[string]any{}
	_ = json.Unmarshal([]byte(text), &out)
	return resp.StatusCode, out
}

// connectExciter connects the browser's person to the real Exciter, consenting there as themselves.
func (b *browser) connectExciter(t *testing.T, base, toolID, username string) {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/tools/"+toolID+"/connection", nil)
	authorize, _ := answer["authorizeURL"].(string)
	if status != http.StatusOK || authorize == "" {
		t.Fatalf("%s starting the connection = %d %v", username, status, answer)
	}
	request := b.follow(t, authorize, username).Query().Get("request")
	status, decided := b.atExciter(t, base, http.MethodPost, "/oauth/requests/"+request, map[string]any{"allow": true})
	redirect, _ := decided["redirectTo"].(string)
	if status != http.StatusOK || redirect == "" {
		t.Fatalf("%s allowing Rotor at Exciter = %d %v", username, status, decided)
	}
	if landed := b.landing(t, redirect); !strings.Contains(landed, "connected="+toolID) {
		t.Fatalf("%s's connection landed on %q", username, landed)
	}
}

// exciterProject makes a project as alice in which she edits and dave may only read, and answers its key.
func exciterProject(t *testing.T, alice *browser, base string) string {
	t.Helper()
	key := "RTR" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:5])
	if status, answer := alice.atExciter(t, base, http.MethodPost, "/projects", map[string]any{"key": key, "name": "Rotor end to end " + key}); status != http.StatusCreated {
		t.Fatalf("creating the Exciter project = %d %v", status, answer)
	}
	status, listed := alice.atExciter(t, base, http.MethodGet, "/users", nil)
	members, _ := listed["members"].([]any)
	ids := map[string]string{}
	for _, m := range members {
		member, _ := m.(map[string]any)
		email, _ := member["email"].(string)
		ids[email], _ = member["userId"].(string)
	}
	if status != http.StatusOK || ids["alice@suite.test"] == "" || ids["dave@suite.test"] == "" {
		t.Fatalf("Exciter's members = %d %v; make suite-up lets dave in", status, listed)
	}
	grants := map[string]any{"grants": []map[string]any{
		{"subject": map[string]any{"type": "user", "id": ids["alice@suite.test"]}, "role": "admin"},
		{"subject": map[string]any{"type": "user", "id": ids["dave@suite.test"]}, "role": "viewer"},
	}}
	if status, answer := alice.atExciter(t, base, http.MethodPut, "/projects/"+key+"/grants", grants); status != http.StatusOK {
		t.Fatalf("granting the project = %d %v", status, answer)
	}
	return key
}

// heldToken is the access token OpenBao keeps for a run until the worker revokes it.
func heldToken(t *testing.T, runID string) string {
	t.Helper()
	bao := rootBao(t)
	listed, err := bao.Logical().List("rotor/metadata/runs/" + runID + "/tokens")
	if err != nil || listed == nil {
		t.Fatalf("OpenBao holds no token for run %s: %v", runID, err)
	}
	keys, _ := listed.Data["keys"].([]any)
	secret, err := bao.Logical().Read(fmt.Sprintf("rotor/data/runs/%s/tokens/%v", runID, keys[0]))
	if err != nil || secret == nil {
		t.Fatalf("read the run's token: %v", err)
	}
	data, _ := secret.Data["data"].(map[string]any)
	token, _ := data["token"].(string)
	return token
}

// whoamiAtExciter answers the status Exciter's MCP endpoint gives a call of whoami with token.
func whoamiAtExciter(t *testing.T, base, token string) int {
	t.Helper()
	call, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "whoami", "arguments": map[string]any{}}})
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/mcp", bytes.NewReader(call))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	_, client := exciter(t)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// draftTurn is the stub model's answer to the drafting agent: its output, in the agent's schema.
func draftTurn(title string, delay time.Duration) map[string]any {
	return map[string]any{"delayMs": delay.Milliseconds(), "toolCalls": []map[string]any{{"name": "submit_output",
		"arguments": map[string]any{"title": title, "statement": "The system shall export the requirements of a project as CSV."}}}}
}

// The MVP against the real Exciter (#25): alice's run writes as alice, dave's is refused as a viewer, and neither
// run leaves a sandbox or a working token behind.
func TestSuiteAWorkflowWritesToExciterAsThePersonWhoStartedIt(t *testing.T) {
	h := newHarness(t)
	sandboxes, _ := dockerBackend(t)
	base, _ := exciter(t)
	key := "llm-key-" + uuid.NewString()
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		draftTurn("Export as CSV", thinkingDelay), draftTurn("Export as CSV", 0)}})

	alice := suiteBrowser(t, "alice", base)
	provider := addProvider(t, alice, llm, key)
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": base})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adding the real Exciter = %d %v", status, answer)
	}
	toolID, _ := toolOf(t, answer)["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+toolID, nil) })
	alice.connectExciter(t, base, toolID, "alice")
	project := exciterProject(t, alice, base)

	// The example as it ships, thinking with the stub model: sandboxes reach no provider of the internet yet.
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "draft-requirements.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(example), "limits:", fmt.Sprintf("model: {name: stub-model, provider: %s}\nlimits:", provider["name"]), 1)
	workflowID := createWorkflow(t, alice, source)
	share(t, alice, workflowID)
	inputs := map[string]any{"projectKey": project, "topic": "exporting requirements"}

	run := startRun(t, alice, workflowID, inputs, http.StatusCreated)
	aliceRun, _ := run["id"].(string)
	waitRun(t, alice, aliceRun, "running")
	// With the worker stopped nothing tears the run down, so the test can try its token first.
	startWorker := stopWorker(t)
	done := waitRun(t, alice, aliceRun, "succeeded", "failed")
	if done["status"] != "succeeded" {
		t.Fatalf("alice's run ended %v: %v; events %+v", done["status"], done, h.events(t, uuid.MustParse(aliceRun)))
	}
	token := heldToken(t, aliceRun)
	if status := whoamiAtExciter(t, base, token); status != http.StatusOK {
		t.Fatalf("Exciter refused alice's run's token while it was held: %d", status)
	}
	startWorker()
	waitTornDown(t, h, uuid.MustParse(aliceRun))
	waitNoSandbox(t, sandboxes, aliceRun)
	if status := whoamiAtExciter(t, base, token); status != http.StatusUnauthorized {
		t.Errorf("Exciter still accepts alice's run's token after the run: %d", status)
	}

	output, _ := done["output"].(map[string]any)
	filed, _ := output["file"].(map[string]any)
	if inner, ok := filed["requirement"].(map[string]any); ok {
		filed = inner
	}
	reqKey, _ := filed["key"].(string)
	status, history := alice.atExciter(t, base, http.MethodGet, "/requirements/"+reqKey+"/revisions", nil)
	revisions, _ := history["revisions"].([]any)
	if status != http.StatusOK || len(revisions) == 0 {
		t.Fatalf("the requirement's revisions = %d %v (output %v)", status, history, output)
	}
	first, _ := revisions[len(revisions)-1].(map[string]any)
	via, _ := first["via"].(map[string]any)
	if via["name"] != "Rotor" || !strings.Contains(strings.ToLower(stringOf(first["authorName"])), "alice") {
		t.Errorf("the revision was written by %v through %v, want alice through Rotor", first["authorName"], first["via"])
	}

	// dave sees the project but may not change it: Exciter refuses his run's write, and the run says so.
	dave := suiteBrowser(t, "dave", base)
	dave.connectExciter(t, base, toolID, "dave")
	run = startRun(t, dave, workflowID, inputs, http.StatusCreated)
	daveRun, _ := run["id"].(string)
	refused := waitRun(t, dave, daveRun, "succeeded", "failed")
	events := h.events(t, uuid.MustParse(daveRun))
	last := events[len(events)-1]
	if refused["status"] != "failed" || refused["reason"] != "tool_error" || !strings.Contains(stringOf(last.Payload), "not change") {
		t.Errorf("dave's run = %v, last event %+v; want tool_error with Exciter's refusal", refused, last.Payload)
	}
	waitTornDown(t, h, uuid.MustParse(daveRun))
	waitNoSandbox(t, sandboxes, daveRun)
	if status, grid := alice.atExciter(t, base, http.MethodGet, "/projects/"+project+"/requirements", nil); status != http.StatusOK || grid["total"] != float64(1) {
		t.Errorf("the project holds %v requirements after both runs, want alice's alone (%d)", grid["total"], status)
	}
	var unrevoked int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM run_token WHERE run_id = ANY($1) AND revoked_at IS NULL`,
		[]uuid.UUID{uuid.MustParse(aliceRun), uuid.MustParse(daveRun)}).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Errorf("%d tokens of the two runs are left unrevoked (%v)", unrevoked, err)
	}
}
