package toolstub

import (
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// armatureOperations are the HTTP operations of Armature's document the tools the stub answers stand for, with the
// status each answers, since Armature's MCP endpoint runs a tool as its operation and answers the body (#129).
var armatureOperations = map[string]struct{ method, path, status string }{
	WhoamiTool:          {"get", "/auth/me", "200"},
	GetIssueTool:        {"get", "/issues/{issueKey}", "200"},
	ListCommentsTool:    {"get", "/issues/{issueKey}/comments", "200"},
	AddCommentTool:      {"post", "/issues/{issueKey}/comments", "201"},
	ListRemoteLinksTool: {"get", "/issues/{issueKey}/remote-links", "200"},
	AddRemoteLinkTool:   {"post", "/issues/{issueKey}/remote-links", "201"},
}

// notArmatureAPI are the stub's routes an Armature tenant serves that are no operation of Armature's API: its
// authorize page is its web client's, and the path-inserted metadata is Exciter's, which an Armature tenant refuses.
var notArmatureAPI = map[string]bool{"GET /oauth/authorize": true, "GET /.well-known/oauth-protected-resource/api/v1/mcp": true}

// armatureAnswer compiles the schema Armature's document gives the answer of the operation a tool stands for.
func armatureAnswer(t *testing.T, tool string) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(toolsDir + "armature/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("armature.json", doc); err != nil {
		t.Fatal(err)
	}
	op := armatureOperations[tool]
	pointer := strings.Join([]string{"paths", strings.ReplaceAll(op.path, "/", "~1"), op.method, "responses", op.status,
		"content", "application~1json", "schema"}, "/")
	schema, err := compiler.Compile("armature.json#/" + url.PathEscape(pointer))
	if err != nil {
		t.Fatalf("Armature's document has no answer for %s %s: %v", op.method, op.path, err)
	}
	return schema
}

// The tools the stub answers are the operations Armature's document has, with the arguments the recorded list gives
// them, so the snapshot and the document were taken at the same commit.
func TestArmatureToolsStandForArmatureOperations(t *testing.T) {
	doc := loadDocument(t, "armature")
	tools := listed(t, SnapshotArmature)
	for name, op := range armatureOperations {
		operation := doc.Paths[op.path][op.method]
		i := slices.IndexFunc(tools, func(tool map[string]any) bool { return tool["name"] == name })
		if operation == nil || i < 0 {
			t.Errorf("%s stands for %s %s, which Armature's document or its tools/list lacks", name, op.method, op.path)
			continue
		}
		if readOnlyTool(tools[i]) != (op.method == "get") {
			t.Errorf("%s's readOnlyHint does not follow its method %s, as Armature's does", name, op.method)
		}
		input, _ := tools[i]["inputSchema"].(map[string]any)
		properties, _ := input["properties"].(map[string]any)
		if operation.RequestBody != nil {
			body, _ := doc.Resolve(operation.RequestBody.Content["application/json"].Schema)
			for field := range body.Properties {
				if _, ok := properties[field]; !ok {
					t.Errorf("%s takes no %s, which its operation's body has: record the list at the document's commit", name, field)
				}
			}
		}
	}
}

// An Armature tenant serves its routes under the names Armature's document has them.
func TestEveryRouteOfAnArmatureTenantIsAnArmatureOperation(t *testing.T) {
	doc := loadDocument(t, "armature")
	router, _ := newTestStub(t).Public().(chi.Routes)
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path := documentPath(method, route)
		if path == "" || strings.HasPrefix(path, llmPath+"/") || path == LangSmithPath || notArmatureAPI[method+" "+path] {
			return nil
		}
		if item, ok := doc.Paths[path]; !ok || item[strings.ToLower(method)] == nil {
			t.Errorf("an Armature tenant serves %s %s, which Armature's document does not have as %s", method, route, path)
		}
		return nil
	})
}

// rpc sends one JSON-RPC request and answers the whole envelope, error included.
func (c *contract) rpc(tenant, bearer, method string, params any) map[string]any {
	c.t.Helper()
	out, _ := c.expect("POST", "/t/"+tenant+mcpPath, request{bearer: bearer, json: map[string]any{"jsonrpc": "2.0", "id": 1,
		"method": method, "params": params}}, http.StatusOK)
	return out
}

// tool calls a tool and answers its result.
func (c *contract) tool(tenant, bearer, name string, args map[string]any) map[string]any {
	c.t.Helper()
	return c.mcp(tenant, bearer, "tools/call", map[string]any{"name": name, "arguments": args})
}

// Each answer of Armature's tools is the body its operation answers in Armature's document, and what is written is
// kept per tenant for the next read and the control API (#129).
func TestArmatureAnswersFitArmaturesDocument(t *testing.T) {
	c := newContract(t)
	const tenant = "story"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "oauth": false, "product": "armature",
		"issues": map[string]any{"CP-12": map[string]any{"type": "Story", "summary": "Sign in with a passkey",
			"descriptionMarkdown": "As a member I want a passkey.\n\nSo I need no password.", "reporter": "erin",
			"readers": []string{"erin"}}}}, http.StatusCreated)
	erin := PATPrefix + "erin"
	answer := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result := c.tool(tenant, erin, name, args)
		if result["isError"] == true {
			t.Fatalf("%s %v refused: %v", name, args, result)
		}
		structured, _ := result["structuredContent"].(map[string]any)
		fits(t, armatureAnswer(t, name), name, structured)
		return structured
	}
	issue := map[string]any{"issueKey": "CP-12"}
	if me := answer(WhoamiTool, map[string]any{}); me["principal"].(map[string]any)["user"].(map[string]any)["name"] != "erin" {
		t.Errorf("whoami = %v, want erin", me)
	}
	if got := answer(GetIssueTool, issue)["issue"].(map[string]any); got["summary"] != "Sign in with a passkey" ||
		len(got["description"].(map[string]any)["content"].([]any)) != 2 {
		t.Errorf("get_issue = %v", got)
	}
	if got := answer(ListCommentsTool, issue)["comments"].([]any); len(got) != 0 {
		t.Errorf("an issue nobody commented on lists %v", got)
	}
	for i, body := range []map[string]any{{"text": "Which browsers?"}, {"bodyMarkdown": "**Which** devices?"},
		{"body": textDocument("Anything else?")}} {
		body["issueKey"] = "CP-12"
		if id := answer(AddCommentTool, body)["comment"].(map[string]any)["id"]; id != CommentID(tenant, "CP-12", i+1) {
			t.Errorf("comment %d has the id %v, want %s", i+1, id, CommentID(tenant, "CP-12", i+1))
		}
	}
	if got := answer(ListCommentsTool, issue)["comments"].([]any); len(got) != 3 {
		t.Errorf("the issue lists %d comments, want 3", len(got))
	}
	link := map[string]any{"issueKey": "CP-12", "url": "https://exciter.test/r/RTR-1", "title": "RTR-1", "source": "Exciter",
		"relation": "requirement"}
	first := answer(AddRemoteLinkTool, link)["remoteLink"].(map[string]any)
	link["title"] = "RTR-1 Passkeys"
	if again := answer(AddRemoteLinkTool, link)["remoteLink"].(map[string]any); again["id"] != first["id"] || again["title"] != "RTR-1 Passkeys" {
		t.Errorf("putting the same address again = %v, want the link retitled", again)
	}
	if got := answer(ListRemoteLinksTool, issue)["remoteLinks"].([]any); len(got) != 1 {
		t.Errorf("the issue lists %d remote links, want 1", len(got))
	}
	written := c.controlled("GET", "/control/tenants/"+tenant+"/records", nil, http.StatusOK)
	if len(written["comments"].(map[string]any)["CP-12"].([]any)) != 3 || len(written["remoteLinks"].(map[string]any)["CP-12"].([]any)) != 1 {
		t.Errorf("the records = %v", written)
	}

	for _, refused := range []map[string]any{
		c.tool(tenant, PATPrefix+"frank", GetIssueTool, issue),
		c.tool(tenant, erin, GetIssueTool, map[string]any{"issueKey": "CP-13"}),
		c.tool(tenant, erin, AddCommentTool, map[string]any{"issueKey": "CP-12", "text": " "}),
		c.tool(tenant, erin, AddCommentTool, map[string]any{"issueKey": "CP-12", "bodyMarkdown": "x", "body": textDocument("y")}),
		c.tool(tenant, erin, AddRemoteLinkTool, map[string]any{"issueKey": "CP-12", "url": "ftp://x", "title": "t", "source": "s"}),
	} {
		if refused["isError"] != true {
			t.Errorf("Armature would refuse what the stub answered: %v", refused)
		}
	}
	for _, args := range []map[string]any{{}, {"issueKey": "CP-12", "verbose": true}} {
		if out := c.rpc(tenant, erin, "tools/call", map[string]any{"name": GetIssueTool, "arguments": args}); out["error"] == nil {
			t.Errorf("get_issue with %v = %v, want the arguments refused", args, out)
		}
	}
}

// A connected app writes only what its grant reaches, as Armature's appScope refuses the rest; tools/list shows every
// tool to every token, as Armature's does.
func TestArmatureWritesNeedTheirScopes(t *testing.T) {
	c := newContract(t)
	const tenant = "armature--scopes"
	c.controlled("POST", "/control/tenants", map[string]any{"name": tenant, "product": "armature",
		"issues": map[string]any{"CP-1": map[string]any{"summary": "s", "readers": []string{"alice"}}}}, http.StatusCreated)
	grant := c.armatureGrant(tenant, "alice", "read issue.write comment.write")
	comment := map[string]any{"issueKey": "CP-1", "text": "Why?"}
	link := map[string]any{"issueKey": "CP-1", "url": "https://exciter.test/r/1", "title": "R", "source": "Exciter"}
	for _, want := range []struct {
		scope           string
		comments, links bool
	}{{"read", false, false}, {"read issue.write", false, true}, {"read comment.write", true, false}} {
		token := grant(want.scope)
		if got := c.mcp(tenant, token, "tools/list", nil)["tools"].([]any); len(got) != len(listed(t, SnapshotArmature)) {
			t.Errorf("a token of %q lists %d tools, want all", want.scope, len(got))
		}
		if commented := c.tool(tenant, token, AddCommentTool, comment)["isError"] != true; commented != want.comments {
			t.Errorf("a token of %q commented: %v, want %v", want.scope, commented, want.comments)
		}
		if linked := c.tool(tenant, token, AddRemoteLinkTool, link)["isError"] != true; linked != want.links {
			t.Errorf("a token of %q linked: %v, want %v", want.scope, linked, want.links)
		}
	}
	if refused := c.tool(tenant, grant("read"), AddCommentTool, comment); !strings.Contains(stringsOf(refused["content"]), "allowed to read only") {
		t.Errorf("a reading token's comment was refused as %v", refused)
	}
}

// armatureGrant connects username to an Armature tenant with scope, answering a function that exchanges the grant's
// token for one of narrower scopes, as Rotor does for each run (#45).
func (c *contract) armatureGrant(tenant, username, scope string) func(narrower string) string {
	c.t.Helper()
	issuer := c.base + "/t/" + tenant
	made, _ := c.expect("POST", "/t/"+tenant+"/api/v1/oauth/register", request{json: map[string]any{
		"redirect_uris": []string{callback}, "client_name": "Rotor", "token_endpoint_auth_method": "client_secret_basic",
		"grant_types": []string{"authorization_code", "refresh_token", grantExchange}}}, http.StatusCreated)
	id, secret := made["client_id"].(string), made["client_secret"].(string)
	c.controlled("POST", "/control/tenants/"+tenant+"/clients/approve", nil, http.StatusOK)
	verifier, challenge := pkce()
	q := url.Values{"response_type": {"code"}, "client_id": {id}, "redirect_uri": {callback}, "state": {"s"}, "code_challenge": {challenge},
		"code_challenge_method": {"S256"}, "resource": {issuer + apiPrefix}, "scope": {scope}}
	_, _, resp := c.armatureCall("GET", "/t/"+tenant+"/api/v1/oauth/authorize?"+q.Encode(), request{})
	page, _ := url.Parse(resp.Header.Get("Location"))
	_, _, resp = c.armatureCall("POST", "/t/"+tenant+"/oauth/consent", request{form: url.Values{FieldRequest: {page.Query().Get(FieldRequest)},
		FieldUsername: {username}, FieldDecision: {DecisionAllow}}})
	back, _ := url.Parse(resp.Header.Get("Location"))
	token := func(form url.Values) map[string]any {
		c.t.Helper()
		status, out, _ := c.armatureCall("POST", "/t/"+tenant+"/api/v1/oauth/token", request{basicID: id, basicToken: secret, form: form})
		if status != http.StatusOK {
			c.t.Fatalf("the token endpoint = %d %v", status, out)
		}
		return out
	}
	access := token(url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "redirect_uri": {callback},
		"code_verifier": {verifier}, "resource": {issuer + apiPrefix}})["access_token"].(string)
	return func(narrower string) string {
		return token(url.Values{"grant_type": {grantExchange}, "subject_token": {access}, "subject_token_type": {tokenTypeAccess},
			"scope": {narrower}})["access_token"].(string)
	}
}
