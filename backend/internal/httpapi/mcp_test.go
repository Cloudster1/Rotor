package httpapi

// Adapted from Exciter at d6dd00c, backend/internal/httpapi/mcp_test.go.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The tools docs/api-contract-m10.md names: workflows and runs read, checked, started and cancelled, and approvals
// read, never decided.
var contractTools = []string{
	"cancel_run", "get_approval", "get_run", "get_workflow", "list_approvals", "list_runs", "list_workflows", "start_run",
	"validate_workflow", "whoami",
}

func TestTheToolsAreTheContractsEachNamedOnceAndExplained(t *testing.T) {
	var names []string
	seen := map[string]string{}
	for _, op := range operations {
		if op.tool == "" {
			if op.toolHelp != "" || op.destructive || op.openWorld {
				t.Errorf("%s %s says how a tool acts but is none", op.method, op.path)
			}
			continue
		}
		key := op.method + " " + op.path
		if prior, ok := seen[op.tool]; ok {
			t.Errorf("%s and %s share the tool name %s", prior, key, op.tool)
		}
		seen[op.tool] = key
		names = append(names, op.tool)
		switch {
		case !strings.HasSuffix(op.toolHelp, "."):
			t.Errorf("%s: the tool sentence %q does not end", key, op.toolHelp)
		case strings.ToLower(op.tool) != op.tool || strings.ContainsAny(op.tool, "- "):
			t.Errorf("%s: %q is not a snake_case tool name", key, op.tool)
		case op.pending || op.public || op.admin || op.root:
			t.Errorf("%s cannot be a tool: it is pending, public, an administrator's or outside the API", key)
		case op.method == http.MethodDelete:
			t.Errorf("%s: no tool removes anything", key)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, contractTools) {
		t.Errorf("the tools are %v, want the contract's %v", names, contractTools)
	}
}

func TestAToolRequiresItsPathAndReadsWhenItGets(t *testing.T) {
	for _, op := range operations {
		// readOnlyGrant knows a reading post by its path alone, so one cannot have path values.
		if op.reads && (op.method != http.MethodPost || len(pathParams(op.path)) > 0) {
			t.Errorf("%s %s reads, which only a post without path values may", op.method, op.path)
		}
	}
	for _, tool := range toolCatalog() {
		if tool.ReadOnly != (tool.Method == http.MethodGet || tool.Name == "validate_workflow") {
			t.Errorf("%s: readOnly %v for %s", tool.Name, tool.ReadOnly, tool.Method)
		}
		for _, name := range pathParams(tool.Path) {
			if _, ok := tool.Input.Properties[name]; !ok || !slices.Contains(tool.Input.Required, name) {
				t.Errorf("%s: the path value %s is not a required argument", tool.Name, name)
			}
		}
		encoded, err := json.Marshal(tool.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "#/components/") {
			t.Errorf("%s: the schema refers outside itself: %s", tool.Name, encoded)
		}
	}
	start, _ := toolByName("start_run")
	for _, field := range []string{"workflowID", "inputs", "idempotencyKey"} {
		if _, ok := start.Input.Properties[field]; !ok {
			t.Errorf("start_run does not take %s", field)
		}
	}
	if slices.Contains(start.Input.Required, "idempotencyKey") {
		t.Error("start_run requires an idempotency key, which the contract leaves optional")
	}
	runs, _ := toolByName("list_runs")
	if limit := runs.Input.Properties["limit"]; limit == nil || limit.Type != "integer" {
		t.Error("list_runs does not take limit as an integer")
	}
}

func TestAReadOnlyGrantIsOfferedOnlyWhatItMayCall(t *testing.T) {
	all, reading := listTools(false), listTools(true)
	if len(reading) == 0 || len(reading) >= len(all) {
		t.Fatalf("%d tools for a read grant of %d", len(reading), len(all))
	}
	var names []string
	for _, tool := range reading {
		names = append(names, tool["name"].(string))
		if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Errorf("a read grant is offered %s", tool["name"])
		}
	}
	if slices.Contains(names, "start_run") || slices.Contains(names, "cancel_run") || !slices.Contains(names, "validate_workflow") {
		t.Errorf("a read grant is offered %v", names)
	}
}

// assistant is a principal acting through a grant, as the OAuth service makes it.
func assistant(readOnly bool) *auth.Principal {
	return &auth.Principal{UserID: uuid.New(), Name: "Bob", Role: auth.RoleMember,
		Assistant: &auth.Assistant{GrantID: uuid.New(), Client: "Claude Code", ReadOnly: readOnly}}
}

func TestTheEndpointSpeaksJSONRPC(t *testing.T) {
	s := &Server{}
	call := func(body string, p *auth.Principal) (int, rpcResponse) {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
		if p != nil {
			req = req.WithContext(auth.WithPrincipal(req.Context(), p))
		}
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		var resp rpcResponse
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("not a JSON-RPC answer: %s", rec.Body.String())
			}
		}
		return rec.Code, resp
	}

	status, resp := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	if status != http.StatusOK || resp.Error != nil || !strings.Contains(string(resp.Result), MCPProtocolVersions[0]) ||
		!strings.Contains(string(resp.Result), `"rotor"`) || !strings.Contains(string(resp.Result), `"instructions"`) ||
		strings.Contains(string(resp.Result), `"resources"`) {
		t.Fatalf("initialize: %d %+v %s", status, resp, resp.Result)
	}
	if string(resp.ID) != "1" {
		t.Errorf("the id %s was not echoed", resp.ID)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`, assistant(false))
	if !strings.Contains(string(resp.Result), `"get_run"`) || !strings.Contains(string(resp.Result), `"start_run"`) {
		t.Fatalf("tools/list: %s", resp.Result)
	}
	reader := assistant(true)
	_, resp = call(`{"jsonrpc":"2.0","id":"b","method":"tools/list"}`, reader)
	if !strings.Contains(string(resp.Result), `"get_run"`) || strings.Contains(string(resp.Result), `"start_run"`) {
		t.Errorf("a read grant is offered: %s", resp.Result)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"start_run","arguments":{}}}`, reader)
	if resp.Error != nil || !strings.Contains(string(resp.Result), `"isError":true`) || !strings.Contains(string(resp.Result), "read only") {
		t.Errorf("a read grant calling a writing tool: %+v %s", resp.Error, resp.Result)
	}
	if status, _ = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); status != http.StatusAccepted {
		t.Errorf("a notification answers %d", status)
	}
	for body, code := range map[string]int{
		`{"jsonrpc":"2.0","id":2,"method":"nothing/here"}`:                                              rpcMethodNotFound,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`:                                            rpcMethodNotFound,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"no_such"}}`:                    rpcInvalidParams,
		`[{"jsonrpc":"2.0","id":4,"method":"ping"}]`:                                                    rpcInvalidRequest,
		`{"id":5,"method":"ping"}`:                                                                      rpcInvalidRequest,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_run"}}`:                    rpcInvalidParams,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"arguments":"no"}}`:                    rpcInvalidParams,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"whoami","arguments":{"x":1}}}`: rpcInvalidParams,
	} {
		if _, resp = call(body, assistant(false)); resp.Error == nil || resp.Error.Code != code {
			t.Errorf("%s answers %+v, want %d", body, resp.Error, code)
		}
	}
	if status, _ = call(`not json`, nil); status != http.StatusBadRequest {
		t.Errorf("malformed JSON answers %d", status)
	}
}

func TestAToolCallBecomesTheHTTPCallItStandsFor(t *testing.T) {
	outer := httptest.NewRequest(http.MethodPost, mcpPath, nil)
	outer.Header.Set("Authorization", "Bearer rotor_oat_x")
	outer.Header.Set(idempotencyHeader, "outer")
	outer.Header.Set("Last-Event-ID", "7")
	outer.Header.Set("X-Forwarded-For", "10.0.0.1")
	outer = outer.WithContext(context.WithValue(outer.Context(), ctxRequestID, "req-1"))

	list, _ := toolByName("list_runs")
	inner, err := list.request(outer, map[string]json.RawMessage{
		"status": json.RawMessage(`["running","waiting"]`), "limit": json.RawMessage(`5`), "mine": json.RawMessage(`true`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.Method != http.MethodGet || inner.URL.Path != APIPrefix+"/runs" {
		t.Errorf("inner call is %s %s", inner.Method, inner.URL.Path)
	}
	if q := inner.URL.Query(); q.Get("limit") != "5" || q.Get("mine") != "true" || len(q["status"]) != 2 {
		t.Errorf("query = %s", inner.URL.RawQuery)
	}
	if inner.Header.Get("Authorization") != "Bearer rotor_oat_x" || inner.Header.Get("X-Request-Id") != "req-1" {
		t.Errorf("the credential or the request id did not travel: %v", inner.Header)
	}
	for _, kept := range []string{idempotencyHeader, "Last-Event-ID", "X-Forwarded-For"} {
		if inner.Header.Get(kept) != "" {
			t.Errorf("the outer %s reached the inner call", kept)
		}
	}
	if !inProcessCall(inner.Context()) {
		t.Error("the inner call is not marked as made in this process")
	}

	get, _ := toolByName("get_run")
	if _, err := get.request(outer, nil); err == nil {
		t.Error("a missing path value was accepted")
	}
	if _, err := get.request(outer, map[string]json.RawMessage{"runID": json.RawMessage(`"x"`), "extra": json.RawMessage(`1`)}); err == nil {
		t.Error("an argument a GET cannot carry was accepted")
	}
	// A path value stays one segment, so it can never climb to another route.
	inner, _ = get.request(outer, map[string]json.RawMessage{"runID": json.RawMessage(`"../approvals/x/decision"`)})
	if strings.Count(inner.URL.Path, "/") != strings.Count(APIPrefix+"/runs/x", "/") {
		t.Errorf("a path value with slashes reads %s", inner.URL.Path)
	}

	start, _ := toolByName("start_run")
	workflow := uuid.NewString()
	inner, err = start.request(outer, map[string]json.RawMessage{"workflowID": json.RawMessage(`"` + workflow + `"`),
		"inputs": json.RawMessage(`{"topic":"x"}`), "idempotencyKey": json.RawMessage(`"run-1"`)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(inner.Body)
	if inner.Method != http.MethodPost || inner.URL.Path != APIPrefix+"/workflows/"+workflow+"/runs" ||
		string(body) != `{"inputs":{"topic":"x"}}` || inner.Header.Get(idempotencyHeader) != "run-1" {
		t.Errorf("start call: %s %s %s %v", inner.Method, inner.URL.Path, body, inner.Header)
	}
}

func TestAResultTooLargeIsRefusedAndSaysHowToAskForLess(t *testing.T) {
	rec := &capture{header: http.Header{}}
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write([]byte(`{"text":"` + strings.Repeat("a", config.MCPMaxToolResultBytes) + `"}`))
	if got := rec.toolResult(); !got.IsError || got.StructuredContent != nil || got.Content[0].Text != tooLarge {
		t.Errorf("a long result answered %+v", got.Content)
	}
	if !strings.Contains(tooLarge, "limit") {
		t.Errorf("%q does not say how to ask for less", tooLarge)
	}
	fits := &capture{header: http.Header{}}
	_, _ = fits.Write([]byte(`{"run":{"id":"r"}}`))
	if got := fits.toolResult(); got.IsError || got.StructuredContent == nil || got.Content[0].Text != `{"run":{"id":"r"}}` {
		t.Errorf("an answer that fits is structured and its text the same JSON: %+v", got)
	}
	refused := &capture{header: http.Header{}}
	refused.WriteHeader(http.StatusNotFound)
	_, _ = refused.Write([]byte(`{"error":{"code":"not_found","message":"There is no such run."}}`))
	if r := refused.toolResult(); !r.IsError || r.Content[0].Text != "There is no such run." {
		t.Errorf("a refusal reads %+v", r)
	}
}

// A client asks in the newest version it speaks and goes on in whatever is answered; a header naming a version
// neither side can speak is refused.
func TestTheVersionIsNegotiatedAndTheHeaderHeldToIt(t *testing.T) {
	s := &Server{}
	call := func(asked, header string) (int, string) {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + asked + `"}}`
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
		if header != "" {
			req.Header.Set(mcpVersionHeader, header)
		}
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		var resp rpcResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(resp.Result, &result)
		return rec.Code, result.ProtocolVersion
	}
	for asked, answered := range map[string]string{
		"2025-06-18": "2025-06-18", "2025-11-25": "2025-11-25", "2024-11-05": MCPProtocolVersions[0], "": MCPProtocolVersions[0],
	} {
		if _, got := call(asked, ""); got != answered {
			t.Errorf("asked for %q, answered %q, want %q", asked, got, answered)
		}
	}
	for header, status := range map[string]int{
		"2025-06-18": http.StatusOK, "2025-11-25": http.StatusOK, "2026-07-28": http.StatusOK, "2024-11-05": http.StatusBadRequest,
		"nonsense": http.StatusBadRequest,
	} {
		if got, _ := call("2025-06-18", header); got != status {
			t.Errorf("the header %q answered %d, want %d", header, got, status)
		}
	}
}

// What a tool answers is described as the protocol asks, an object standing alone, and every hint is said so that
// a client never takes a write for destructive by default.
func TestEveryToolDescribesWhatItAnswersAndHowItActs(t *testing.T) {
	rows := map[string]operation{}
	for _, op := range operations {
		if op.tool != "" {
			rows[op.tool] = op
		}
	}
	for _, tool := range listTools(false) {
		name := tool["name"].(string)
		op := rows[name]
		if tool["title"] == "" || tool["title"] == name {
			t.Errorf("%s has no title in words: %v", name, tool["title"])
		}
		hints := tool["annotations"].(map[string]any)
		if hints["idempotentHint"] != (op.method == http.MethodPut || op.method == http.MethodPatch) || hints["destructiveHint"] != op.destructive ||
			hints["openWorldHint"] != op.openWorld {
			t.Errorf("%s hints %v for %s", name, hints, op.method)
		}
		output, described := tool["outputSchema"].(*openapi.Schema)
		if !described || output.Type != "object" || len(output.Properties) == 0 {
			t.Errorf("%s: the output is not an object: %+v", name, output)
			continue
		}
		encoded, _ := json.Marshal(output)
		if strings.Contains(string(encoded), "#/components/") {
			t.Errorf("%s: the output schema refers outside itself", name)
		}
	}
	if start, _ := toolByName("start_run"); start.Title != "Start run" || !start.OpenWorld || !start.Destructive {
		t.Errorf("start_run is %q, open world %v, destructive %v", start.Title, start.OpenWorld, start.Destructive)
	}
}

// A page of another site that rebinds a name to a local Rotor reaches MCP with a bearer as well as with a cookie.
func TestMCPRefusesAForeignOriginWhateverTheCredential(t *testing.T) {
	h := newServer(t).Routes(nil)
	for origin, refused := range map[string]bool{"http://evil.test": true, "http://app.test": false, "": false} {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Authorization", "Bearer x")
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, body := serve(t, h, req)
		if got := resp.StatusCode == http.StatusForbidden && errorOf(t, body)["code"] == "cross_site"; got != refused {
			t.Errorf("origin %q: %d %v", origin, resp.StatusCode, body)
		}
	}
	resp, body := serve(t, h, httptest.NewRequest(http.MethodGet, mcpPath, nil))
	if resp.StatusCode != http.StatusMethodNotAllowed || errorOf(t, body)["code"] != "method_not_allowed" {
		t.Errorf("GET %s = %d %v, want 405: there is no stream to open", mcpPath, resp.StatusCode, body)
	}
}

// An approval is a person's own judgement, so no tool stands for deciding one however the table grows
// (docs/decisions.md).
func TestNoToolDecidesAnApproval(t *testing.T) {
	found := false
	for _, op := range operations {
		if op.handler != "handleDecideApproval" {
			continue
		}
		found = true
		if op.tool != "" {
			t.Errorf("%s %s is the tool %s; an approval is decided by a person in the browser", op.method, op.path, op.tool)
		}
	}
	if !found {
		t.Fatal("the decision operation is not in the table; hold whatever decides an approval now instead")
	}
	for _, tool := range toolCatalog() {
		lower := strings.ToLower(tool.Name)
		if strings.HasSuffix(tool.Path, "/decision") || strings.Contains(lower, "decide") || strings.Contains(lower, "approve") ||
			strings.Contains(lower, "reject") {
			t.Errorf("%s stands for %s", tool.Name, tool.Path)
		}
	}
}

// What a person does at a keyboard is no tool: consenting, connecting, ending a grant, signing out and administering.
func TestKeyboardOnlyRowsHaveNoTool(t *testing.T) {
	keyboard := map[string]bool{
		"handleDecideApproval": true, "handleConnect": true, "handleDisconnect": true, "handleLogout": true,
		"handleDecideOAuthRequest": true, "handleEndOAuthGrant": true, "handleEndAnybodysOAuthGrant": true, "handleRunEvents": true,
	}
	for _, op := range operations {
		if op.tool != "" && (keyboard[op.handler] || op.admin) {
			t.Errorf("%s %s is the tool %s", op.method, op.path, op.tool)
		}
	}
}
