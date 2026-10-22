package toolstub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The contract: the stub serves nothing its tools do not and answers in their shapes, held to the documents make
// tool-contracts vendors under api/tools and the tools/list snapshots make tool-snapshots records beside them.

const toolsDir = "../../../api/tools/"

// snapshotFile is where make tool-snapshots records a tool's tools/list.
func snapshotFile(tool string) string { return toolsDir + tool + "/tools-list.json" }

// listed is a snapshot's tools, as recorded.
func listed(t *testing.T, tool string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(snapshotFile(tool))
	if err != nil {
		t.Fatalf("read %s's tools/list snapshot; record it with make tool-snapshots TOOL=%s: %v", tool, tool, err)
	}
	var list struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	return list.Tools
}

// outputSchema compiles the output schema a tool of Exciter's snapshot lists, which MCP clients check answers against.
func outputSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	var schema any
	for _, tool := range listed(t, SnapshotExciter) {
		if tool["name"] == name {
			schema = tool["outputSchema"]
		}
	}
	if schema == nil {
		t.Fatalf("the snapshot's %s lists no output schema", name)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(name+".json", schema); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

// fits checks a structured answer against a compiled schema.
func fits(t *testing.T, schema *jsonschema.Schema, what string, answer any) {
	t.Helper()
	encoded, _ := json.Marshal(answer)
	instance, _ := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err := schema.Validate(instance); err != nil {
		t.Errorf("%s answered %s, which its schema refuses: %v", what, encoded, err)
	}
}

func loadDocument(t *testing.T, tool string) *openapi.Document {
	t.Helper()
	raw, err := os.ReadFile(toolsDir + tool + "/openapi.json")
	if err != nil {
		t.Fatalf("read %s's document; vendor it with make tool-contracts TOOL=%s REF=<sha>: %v", tool, tool, err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s's document does not read as OpenAPI: %v", tool, err)
	}
	return &doc
}

func newTestStub(t *testing.T) *Stub {
	t.Helper()
	snapshots := map[string][]byte{}
	for _, tool := range []string{SnapshotExciter, SnapshotArmature} {
		raw, err := os.ReadFile(snapshotFile(tool))
		if err != nil {
			t.Fatalf("read %s's tools/list snapshot; record it with make tool-snapshots TOOL=%s: %v", tool, tool, err)
		}
		snapshots[tool] = raw
	}
	s, err := New(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// stubOnly are the stub's routes that are not Exciter's API: the consent page is Exciter's web client.
var stubOnly = map[string]bool{"GET /oauth/consent": true, "POST /oauth/consent": true}

// documentPath is the operation of Exciter's document a stub route stands for, or "" for a stub-only one.
func documentPath(method, route string) string {
	route = strings.TrimSuffix(route, "/")
	if rest, ok := strings.CutPrefix(route, TenantPrefix+"/{tenant}"); ok {
		route = rest
	} else {
		route = strings.Replace(route, TenantPrefix+"/{tenant}", "", 1)
	}
	if stubOnly[method+" "+route] {
		return ""
	}
	if rest, ok := strings.CutPrefix(route, apiPrefix); ok {
		return rest
	}
	return route
}

func TestEveryRouteIsAnExciterOperation(t *testing.T) {
	doc := loadDocument(t, "exciter")
	router, ok := newTestStub(t).Public().(chi.Routes)
	if !ok {
		t.Fatal("the stub's router is not walkable")
	}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path := documentPath(method, route)
		// The stub model stands in for a language model provider, not for Exciter; its contract is OpenAI's chat API.
		// The LangSmith stand-in takes OTLP, whose contract is OpenTelemetry's.
		if path == "" || strings.HasPrefix(path, llmPath+"/") || path == LangSmithPath {
			return nil
		}
		if item, ok := doc.Paths[path]; !ok || item[strings.ToLower(method)] == nil {
			t.Errorf("the stub serves %s %s, which Exciter's document does not have as %s", method, route, path)
		}
		return nil
	})
}

// answeredTools are the tools the stub answers per snapshot, which the recorded list has to carry.
var answeredTools = map[string][]string{
	SnapshotExciter: {WhoamiTool, GetRequirementTool, SearchRequirementsTool, CreateRequirementTool, UpdateRequirementTool,
		ListArmatureLinksTool, LinkArmatureIssueTool, RequestReviewsTool},
	SnapshotArmature: {WhoamiTool, GetIssueTool, ListCommentsTool, AddCommentTool, ListRemoteLinksTool, AddRemoteLinkTool},
}

// Each snapshot is served as recorded, so a client sees the tool's own tools by name, schema and hints, and a
// tenant serves the one its option or its product names (#129).
func TestToolsListIsTheSnapshot(t *testing.T) {
	stub := newTestStub(t)
	for snapshot, answered := range answeredTools {
		want := listed(t, snapshot)
		for _, opts := range []Options{{Snapshot: snapshot}, {Product: snapshot}} {
			if got := stub.toolsFor(newTenant("contract", opts), &caller{}); !reflect.DeepEqual(got, want) {
				t.Errorf("a tenant of %+v serves %d tools that are not %s's %d", opts, len(got), snapshot, len(want))
			}
		}
		for _, name := range answered {
			if !slices.ContainsFunc(want, func(tool map[string]any) bool { return tool["name"] == name }) {
				t.Errorf("%s's snapshot has no %s tool, which the stub answers", snapshot, name)
			}
		}
	}
}

// contract drives the stub and checks every request and answer against Exciter's document.
type contract struct {
	t       *testing.T
	doc     *openapi.Document
	base    string
	control string
	client  *http.Client
	served  map[string]bool
}

func newContract(t *testing.T) *contract {
	s := newTestStub(t)
	public, control := httptest.NewServer(s.Public()), httptest.NewServer(s.Control())
	t.Cleanup(public.Close)
	t.Cleanup(control.Close)
	return &contract{t: t, doc: loadDocument(t, "exciter"), base: public.URL, control: control.URL, served: map[string]bool{},
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// request is one call: a JSON or form body, a bearer or Basic credential.
type request struct {
	json       any
	form       url.Values
	bearer     string
	basicID    string
	basicToken string
}

// route finds the template of a concrete tenant path, for the document lookup; tenants are one segment.
func route(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		if i > 0 && parts[i-1] == strings.TrimPrefix(TenantPrefix, "/") {
			parts[i] = "{tenant}"
		}
	}
	return strings.Join(parts, "/")
}

// call sends a request to the stub and, for an operation of Exciter's, checks the body sent and answered.
func (c *contract) call(method, path string, in request) (*http.Response, []byte) {
	c.t.Helper()
	var (
		body        io.Reader
		sent        []byte
		contentType string
	)
	switch {
	case in.json != nil:
		sent, _ = json.Marshal(in.json)
		body, contentType = bytes.NewReader(sent), "application/json"
	case in.form != nil:
		body, contentType = strings.NewReader(in.form.Encode()), "application/x-www-form-urlencoded"
	}
	req, _ := http.NewRequest(method, c.base+path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if in.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+in.bearer)
	}
	if in.basicID != "" {
		req.SetBasicAuth(url.QueryEscape(in.basicID), url.QueryEscape(in.basicToken))
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	bare, _, _ := strings.Cut(path, "?")
	docPath := documentPath(method, route(bare))
	if docPath == "" {
		return resp, raw
	}
	op := c.doc.Paths[docPath][strings.ToLower(method)]
	if op == nil {
		c.t.Fatalf("%s %s is not in Exciter's document", method, docPath)
	}
	c.checkSent(op, method, docPath, sent, in.form)
	c.checkAnswer(op, method, docPath, resp, raw)
	return resp, raw
}

func (c *contract) checkSent(op *openapi.Operation, method, path string, sent []byte, form url.Values) {
	c.t.Helper()
	if sent == nil && form == nil {
		return
	}
	if op.RequestBody == nil {
		c.t.Errorf("%s %s takes no body in Exciter, and was sent one", method, path)
		return
	}
	media := op.RequestBody.Content["application/json"]
	if form != nil {
		media = op.RequestBody.Content["application/x-www-form-urlencoded"]
		values := map[string]any{}
		for k := range form {
			values[k] = form.Get(k)
		}
		sent, _ = json.Marshal(values)
	}
	if media.Schema == nil {
		c.t.Errorf("%s %s takes no body of that type in Exciter", method, path)
	} else if err := c.doc.ValidateJSON(media.Schema, sent); err != nil {
		c.t.Errorf("%s %s: Exciter would refuse the body %s: %v", method, path, sent, err)
	}
}

func (c *contract) checkAnswer(op *openapi.Operation, method, path string, resp *http.Response, raw []byte) {
	c.t.Helper()
	answer, ok := op.Responses[strconv.Itoa(resp.StatusCode)]
	if !ok {
		if resp.StatusCode < http.StatusBadRequest {
			c.t.Errorf("%s %s answered %d, which Exciter never does", method, path, resp.StatusCode)
			return
		}
		answer = op.Responses["default"]
	}
	if media, isJSON := answer.Content["application/json"]; isJSON {
		if err := c.doc.ValidateJSON(media.Schema, raw); err != nil {
			c.t.Errorf("%s %s answered %d in a shape Exciter does not: %v\n%s", method, path, resp.StatusCode, err, raw)
		}
	} else if len(answer.Content) == 0 && len(bytes.TrimSpace(raw)) > 0 && resp.StatusCode != http.StatusFound {
		c.t.Errorf("%s %s answered %d with a body, where Exciter has none", method, path, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusBadRequest {
		c.served[method+" "+path] = true
	}
}

// expect calls and insists on the status, answering the JSON body.
func (c *contract) expect(method, path string, in request, want int) (map[string]any, *http.Response) {
	c.t.Helper()
	resp, raw := c.call(method, path, in)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out, resp
}

// controlled calls the control API, which is no operation of Exciter's.
func (c *contract) controlled(method, path string, body any, want int) map[string]any {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.control+path, reader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("control %s %s = %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// validates checks a body against a component of another tool's document.
func validates(t *testing.T, tool, component string, raw []byte) {
	t.Helper()
	doc := loadDocument(t, tool)
	if err := doc.ValidateJSON(&openapi.Schema{Ref: "#/components/schemas/" + component}, raw); err != nil {
		t.Errorf("the stub's answer is not %s's %s: %v\n%s", tool, component, err, raw)
	}
}

// MCP clients check a tool's structured answer against the output schema it lists, so whoami answers in the shape of
// Exciter's own: a stub that did not would fail every client that checks, as the runner's does.
func TestWhoamiAnswersInTheShapeExciterLists(t *testing.T) {
	c := newContract(t)
	c.controlled("POST", "/control/tenants", map[string]any{"name": "plain", "oauth": false}, http.StatusCreated)
	fits(t, outputSchema(t, WhoamiTool), WhoamiTool, c.whoami("plain", PATPrefix+"erin"))
}

// get_requirement answers in the shape Exciter lists too, for a key the tenant lets the caller read, and refuses
// any other as Exciter refuses one it does not show (#35).
func TestGetRequirementAnswersInTheShapeExciterLists(t *testing.T) {
	c := newContract(t)
	c.controlled("POST", "/control/tenants", map[string]any{"name": "reqs", "oauth": false,
		"requirements": map[string]any{"RTR-7": []string{"erin"}}}, http.StatusCreated)
	read := func(username, key string) map[string]any {
		return c.mcp("reqs", PATPrefix+username, "tools/call", map[string]any{"name": GetRequirementTool, "arguments": map[string]any{"reqKey": key}})
	}
	answer := read("erin", "RTR-7")
	if answer["isError"] == true {
		t.Errorf("get_requirement refused a reader: %v", answer)
	}
	fits(t, outputSchema(t, GetRequirementTool), GetRequirementTool, answer["structuredContent"])
	for _, refused := range []map[string]any{read("frank", "RTR-7"), read("erin", "RTR-8")} {
		if refused["isError"] != true {
			t.Errorf("a requirement the caller may not see was answered: %v", refused)
		}
	}
}
