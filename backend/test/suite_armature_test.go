//go:build integration && suite

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/oidc"
)

// What make suite-up's seed made in Armature (backend/cmd/suite-seed): its administrator, who signs in with a
// password, and the project alice and bob work in and dave only reads.
const (
	armatureAdminEmail, armatureAdminPassword = "admin@armature.test", "armature admin password"
	armatureProject                           = "CP"
)

// armatureBrowser signs username in to Rotor and to the suite's Armature, both through Rotor's Keycloak.
func armatureBrowser(t *testing.T, username, base string) *browser {
	t.Helper()
	b := newBrowser(t)
	b.signIn(username)
	b.client.Transport = oidc.HTTPClient(map[string]string{
		os.Getenv(envKeycloakPublic): os.Getenv(envKeycloak), os.Getenv(envApp): apiURL(t), base: os.Getenv(envArmatureInternal),
	}).Transport
	b.signInToArmature(t, base, username)
	return b
}

// signInToArmature signs the browser's person in to the suite's Armature, through Rotor's Keycloak.
func (b *browser) signInToArmature(t *testing.T, base, username string) {
	t.Helper()
	b.follow(t, base+"/api/v1/auth/oidc/start?next=/", username)
	if status, me := b.atArmature(t, base, http.MethodGet, "/auth/me", nil); status != http.StatusOK {
		t.Fatalf("%s signing in to Armature through Keycloak = %d %v", username, status, me)
	}
}

// atArmature calls the suite's Armature as the browser's Armature session; its API is shaped as Exciter's.
func (b *browser) atArmature(t *testing.T, base, method, path string, body any) (int, map[string]any) {
	t.Helper()
	return b.atExciter(t, base, method, path, body)
}

// armatureAdmin is Armature's administrator, signed in with the password the seed gave them.
func armatureAdmin(t *testing.T, base string) *browser {
	t.Helper()
	b := newBrowser(t)
	b.client.Transport = oidc.HTTPClient(map[string]string{base: os.Getenv(envArmatureInternal)}).Transport
	if status, answer := b.atArmature(t, base, http.MethodPost, "/auth/login", map[string]any{
		"email": armatureAdminEmail, "password": armatureAdminPassword}); status != http.StatusOK {
		t.Fatalf("signing in to Armature as its administrator = %d %v; make suite-up seeds them", status, answer)
	}
	return b
}

// allowRotor allows every pending client called Rotor, as an Armature administrator does under Connected apps.
func allowRotor(t *testing.T, admin *browser, base string) {
	t.Helper()
	status, listed := admin.atArmature(t, base, http.MethodGet, "/oauth/clients", nil)
	clients, _ := listed["clients"].([]any)
	allowed := 0
	for _, c := range clients {
		client, _ := c.(map[string]any)
		if client["name"] != "Rotor" || client["status"] != "pending" {
			continue
		}
		if status, answer := admin.atArmature(t, base, http.MethodPost, "/oauth/clients/"+stringOf(client["id"])+"/approve", nil); status != http.StatusOK {
			t.Fatalf("allowing Rotor at Armature = %d %v", status, answer)
		}
		allowed++
	}
	if status != http.StatusOK || allowed == 0 {
		t.Fatalf("Armature's connected apps = %d %v, with no Rotor waiting", status, listed)
	}
}

// addArmature adds the suite's Armature to Rotor as an administrator, which Armature holds pending until allowed.
func addArmature(t *testing.T, alice *browser, base string) string {
	t.Helper()
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": base})
	tool := toolOf(t, answer)
	id, _ := tool["id"].(string)
	if status != http.StatusCreated || tool["name"] != "armature" || tool["status"] != "awaiting_approval" || tool["mcpURL"] != base+"/api/v1/mcp" {
		t.Fatalf("adding the real Armature = %d %v", status, answer)
	}
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })
	allowRotor(t, armatureAdmin(t, base), base)
	return id
}

// connectToArmature connects the browser's person to the real Armature, allowing Rotor there as themselves; the
// consent page is the web's, so the browser makes the two calls it makes.
func (b *browser) connectToArmature(t *testing.T, base, toolID, username string) {
	t.Helper()
	status, answer := b.api(http.MethodPost, "/tools/"+toolID+"/connection", nil)
	authorize, _ := url.Parse(stringOf(answer["authorizeURL"]))
	if status != http.StatusOK || authorize == nil || authorize.RawQuery == "" {
		t.Fatalf("%s starting the connection = %d %v", username, status, answer)
	}
	status, asked := b.atArmature(t, base, http.MethodGet, "/oauth/consent?"+authorize.RawQuery, nil)
	redirect, _ := asked["redirectTo"].(string)
	if status == http.StatusOK && redirect == "" {
		status, asked = b.atArmature(t, base, http.MethodPost, "/oauth/consent", map[string]any{"query": authorize.RawQuery, "decision": "allow"})
		redirect, _ = asked["redirectTo"].(string)
	}
	if status != http.StatusOK || redirect == "" {
		t.Fatalf("%s allowing Rotor at Armature = %d %v", username, status, asked)
	}
	if landed := b.landing(t, redirect); !strings.Contains(landed, "connected="+toolID) {
		t.Fatalf("%s's connection landed on %q", username, landed)
	}
}

// fileIssue files an issue of the type named in the seed's project as the browser's person, and answers its key.
func fileIssue(t *testing.T, b *browser, base, typeName, summary, description string) string {
	t.Helper()
	_, types := b.atArmature(t, base, http.MethodGet, "/issue-types", nil)
	typeID := ""
	listing, _ := types["issueTypes"].([]any)
	for _, it := range listing {
		if kind, _ := it.(map[string]any); kind["name"] == typeName {
			typeID, _ = kind["id"].(string)
		}
	}
	status, made := b.atArmature(t, base, http.MethodPost, "/projects/"+armatureProject+"/issues", map[string]any{
		"summary": summary, "typeId": typeID, "descriptionMarkdown": description})
	issue, _ := made["issue"].(map[string]any)
	key, _ := issue["key"].(string)
	if status != http.StatusCreated || key == "" {
		t.Fatalf("filing a %s in %s = %d %v", typeName, armatureProject, status, made)
	}
	return key
}

// issueComments are the comments on an issue, oldest first, as the browser's person reads them.
func issueComments(t *testing.T, b *browser, base, key string) []map[string]any {
	t.Helper()
	status, listed := b.atArmature(t, base, http.MethodGet, "/issues/"+key+"/comments", nil)
	if status != http.StatusOK {
		t.Fatalf("reading the comments on %s = %d %v", key, status, listed)
	}
	var out []map[string]any
	listing, _ := listed["comments"].([]any)
	for _, c := range listing {
		comment, _ := c.(map[string]any)
		out = append(out, comment)
	}
	return out
}

// meAtArmature answers what Armature's /auth/me says of a bearer token: the status, the person and the app.
func meAtArmature(t *testing.T, base, token string) (status int, email, app string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, client := armature(t)
	b := &browser{t: t, client: client}
	resp, text := b.do(req)
	var me struct {
		Principal struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"principal"`
		Token *struct {
			App *struct {
				Name string `json:"name"`
			} `json:"app"`
		} `json:"token"`
	}
	if err := json.Unmarshal([]byte(text), &me); err == nil && me.Token != nil && me.Token.App != nil {
		app = me.Token.App.Name
	}
	return resp.StatusCode, me.Principal.User.Email, app
}

// commentTurn is the stub model's answer to the commenting agent: the comment, in its output schema.
func commentTurn(text string, delay time.Duration) map[string]any {
	return map[string]any{"delayMs": delay.Milliseconds(), "toolCalls": []map[string]any{{"name": "submit_output",
		"arguments": map[string]any{"text": text}}}}
}

// commentWorkflow has the model write a comment, then posts it on an Armature issue as the person who runs it.
func commentWorkflow(provider string) string {
	return fmt.Sprintf(`apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: %s}
inputs: {type: object, required: [issueKey], properties: {issueKey: {type: string}}}
tools:
  armature:
    tool: armature
    scopes: [comment.write]
    access: write
    allowTools: [add_comment]
model: {name: stub-model, provider: %s}
limits: {maxSteps: 5, timeout: 5m, maxTokens: 20000}
entry: draft
nodes:
  - id: draft
    type: agent
    prompt: Write a short comment that says the issue {{ inputs.issueKey }} was checked.
    output: {schema: {type: object, required: [text], properties: {text: {type: string}}}}
  - id: comment
    type: tool
    tool: armature
    call: add_comment
    arguments: {issueKey: "{{ inputs.issueKey }}", bodyMarkdown: "{{ nodes.draft.text }}"}
edges:
  - {from: draft, to: comment}
`, uniqueName("armature-comment"), provider)
}

// The deferred half of #45: alice's run comments at the real Armature as alice through Rotor, with a token of its own
// revoked at its end while her grant lives on, and Armature refuses dave, who only reads.
func TestSuiteARunWritesToArmatureAsThePersonWhoStartedIt(t *testing.T) {
	h := newHarness(t)
	sandboxes, _ := dockerBackend(t)
	base, _ := armature(t)
	key := "llm-key-" + uuid.NewString()
	said := "Checked by Rotor " + uuid.NewString()[:8]
	llm := newToolStub(t, map[string]any{"llmKey": key, "llmScript": []map[string]any{
		commentTurn(said, thinkingDelay), commentTurn("Dave was here.", 0)}})

	alice := armatureBrowser(t, "alice", base)
	provider := addProvider(t, alice, llm, key)
	toolID := addArmature(t, alice, base)
	alice.connectToArmature(t, base, toolID, "alice")
	if _, answer := alice.api(http.MethodGet, "/tools/"+toolID, nil); toolOf(t, answer)["status"] != "ready" {
		t.Errorf("Armature after alice connected = %v, want ready", answer)
	}
	issue := fileIssue(t, alice, base, "Bug", "Checked by a run", "A run comments here.")
	workflowID := createWorkflow(t, alice, commentWorkflow(stringOf(provider["name"])))
	share(t, alice, workflowID)
	inputs := map[string]any{"issueKey": issue}

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
	if status, email, app := meAtArmature(t, base, token); status != http.StatusOK || email != "alice@suite.test" || app != "Rotor" {
		t.Fatalf("alice's run's token at Armature = %d, as %q through %q; want alice through Rotor", status, email, app)
	}
	startWorker()
	waitTornDown(t, h, uuid.MustParse(aliceRun))
	waitNoSandbox(t, sandboxes, aliceRun)
	if status, _, _ := meAtArmature(t, base, token); status != http.StatusUnauthorized {
		t.Errorf("Armature still accepts alice's run's token after the run: %d", status)
	}

	comments := issueComments(t, alice, base, issue)
	last := comments[len(comments)-1]
	author, _ := last["author"].(map[string]any)
	if author["email"] != "alice@suite.test" || !strings.Contains(stringOf(last["body"]), said) {
		t.Errorf("the last comment on %s is %v", issue, last)
	}
	// Her grant outlives the run's token: Armature still lists Rotor among her apps, and the next run can mint.
	_, tokens := alice.atArmature(t, base, http.MethodGet, "/tokens", nil)
	if !strings.Contains(stringOf(tokens["tokens"]), "Rotor") {
		t.Errorf("alice's Armature tokens after the run are %v, without Rotor's grant", tokens)
	}
	_, me := alice.me()
	aliceID, _ := me["user"].(map[string]any)["id"].(string)
	plane := controlPlaneReaching(t, h, nil, map[string]string{base: os.Getenv(envArmatureInternal)})
	next, err := plane.Mint(context.Background(), connections.MintRequest{UserID: uuid.MustParse(aliceID), ToolID: uuid.MustParse(toolID),
		Scopes: []string{"read"}, RunID: "suite-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatalf("minting from alice's grant after the run: %v", err)
	}
	if status, email, _ := meAtArmature(t, base, next.Value); status != http.StatusOK || email != "alice@suite.test" {
		t.Errorf("a token from alice's grant after the run = %d as %q", status, email)
	}
	_ = plane.Revoke(context.Background(), uuid.MustParse(toolID), next.Value)

	// dave reads the project and may not comment: Armature refuses his run's write, and the run says so.
	dave := armatureBrowser(t, "dave", base)
	dave.connectToArmature(t, base, toolID, "dave")
	run = startRun(t, dave, workflowID, inputs, http.StatusCreated)
	daveRun, _ := run["id"].(string)
	refused := waitRun(t, dave, daveRun, "succeeded", "failed")
	if refused["status"] != "failed" || refused["reason"] != "tool_error" {
		t.Errorf("dave's run = %v, want tool_error with Armature's refusal; events %+v", refused, h.events(t, uuid.MustParse(daveRun)))
	}
	waitTornDown(t, h, uuid.MustParse(daveRun))
	waitNoSandbox(t, sandboxes, daveRun)
	if after := issueComments(t, alice, base, issue); len(after) != len(comments) {
		t.Errorf("%s has %d comments after dave's run, want alice's %d", issue, len(after), len(comments))
	}
	var unrevoked int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM run_token WHERE run_id = ANY($1) AND revoked_at IS NULL`,
		[]uuid.UUID{uuid.MustParse(aliceRun), uuid.MustParse(daveRun)}).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Errorf("%d tokens of the two runs are left unrevoked (%v)", unrevoked, err)
	}
}
