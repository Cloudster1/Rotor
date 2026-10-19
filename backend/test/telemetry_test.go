//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// envJaeger is the observe profile's Jaeger, which the gate's stack starts.
const envJaeger = "ROTOR_TEST_JAEGER_URL"

// traceWait bounds how long the spans of a run take to reach Jaeger through the collector's batches.
const traceWait = time.Minute

// jaegerURL is Jaeger's address, skipping the test outside a stack started with it; in CI that is a failure.
func jaegerURL(t *testing.T) string {
	t.Helper()
	address := os.Getenv(envJaeger)
	if address != "" {
		if resp, err := http.Get(address + "/api/services"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return address
			}
		}
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("Jaeger does not answer at %q; the gate's stack starts it with the observe profile", address)
	}
	t.Skipf("Jaeger does not answer at %q; start the stack with make observe", address)
	return ""
}

// agentWorkflow asks the stub model, which calls whoami through the tool, so the run has a node, a model call and an
// MCP call to trace.
func agentWorkflow(tool, provider string) string {
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + uniqueName("traced") + `}
tools:
  t: {tool: ` + tool + `, scopes: [mcp], allowTools: [whoami]}
model: {name: stub-model, provider: ` + provider + `}
limits: {maxSteps: 10, timeout: 5m, maxTokens: 10000}
entry: ask
nodes:
  - {id: ask, type: agent, prompt: 'Who am I?', tools: [t]}
`
}

// tracedRun starts a workflow as b under a trace of the test's own and waits for it to succeed; it answers the trace id.
func tracedRun(t *testing.T, b *browser, workflowID string) string {
	t.Helper()
	trace := strings.ReplaceAll(uuid.NewString(), "-", "")
	status, run := b.apiWith(http.MethodPost, "/workflows/"+workflowID+"/runs", map[string]any{"inputs": map[string]any{}},
		map[string]string{"traceparent": "00-" + trace + "-b7ad6b7169203331-01"})
	if status != http.StatusCreated {
		t.Fatalf("starting a traced run = %d %v", status, run)
	}
	if done := waitRun(t, b, run["id"].(string), "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the traced run ended %v: %v", done["status"], done["reason"])
	}
	return trace
}

// jaegerTrace is a trace as Jaeger's query API answers it: its spans, and the service each comes from.
type jaegerTrace struct {
	Data []struct {
		Spans []struct {
			OperationName string `json:"operationName"`
			ProcessID     string `json:"processID"`
		} `json:"spans"`
		Processes map[string]struct {
			ServiceName string `json:"serviceName"`
		} `json:"processes"`
	} `json:"data"`
}

func TestARunIsOneTraceAcrossTheApiTheWorkerAndTheRunner(t *testing.T) {
	jaeger := jaegerURL(t)
	key, product := "llm-key-"+uuid.NewString(), "traced-"+uuid.NewString()[:8]
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": []map[string]any{
		{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	trace := tracedRun(t, bob, createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string))))

	want := []string{"rotor-api POST /api/v1/workflows/{workflowID}/runs", "rotor-api run.start", "rotor-worker run.start_sandbox",
		"rotor-runner run", "rotor-runner node ask", "rotor-runner llm", "rotor-runner mcp whoami"}
	var seen map[string]bool
	deadline := time.Now().Add(traceWait)
	for time.Now().Before(deadline) {
		seen = map[string]bool{}
		resp, err := http.Get(jaeger + "/api/traces/" + trace)
		if err == nil {
			var got jaegerTrace
			_ = json.NewDecoder(resp.Body).Decode(&got)
			resp.Body.Close()
			for _, d := range got.Data {
				for _, s := range d.Spans {
					seen[d.Processes[s.ProcessID].ServiceName+" "+s.OperationName] = true
				}
			}
		}
		if missing := missingSpans(seen, want); len(missing) == 0 {
			break
		}
		time.Sleep(time.Second)
	}
	if missing := missingSpans(seen, want); len(missing) > 0 {
		t.Fatalf("trace %s lacks %v; it holds %v", trace, missingSpans(seen, want), seen)
	}

	// The worker counts how long the run took once it cleaned up after it; the api serves its own metrics.
	if !eventually(func() bool {
		return strings.Contains(scrape(t, os.Getenv(envWorkerMetrics)), `rotor_run_duration_seconds_count{status="succeeded"}`)
	}) {
		t.Error("the worker does not count the run's duration")
	}
	if api := scrape(t, os.Getenv(envAPIMetrics)); !strings.Contains(api, "# TYPE") {
		t.Errorf("the api's /metrics answers %q", api)
	}
}

// envAPIMetrics is the api's /metrics, on the stack's network.
const envAPIMetrics = "ROTOR_TEST_API_METRICS_URL"

// scrape reads a /metrics page as Prometheus would.
func scrape(t *testing.T, address string) string {
	t.Helper()
	if address == "" {
		t.Skip("the metrics address is not set; run the suite with make test-integration")
	}
	resp, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	_, _ = io.Copy(&b, resp.Body)
	return b.String()
}

// eventually polls ok until it holds or traceWait passes.
func eventually(ok func() bool) bool {
	deadline := time.Now().Add(traceWait)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// missingSpans are the wanted spans, by service and a prefix of their name, that seen lacks.
func missingSpans(seen map[string]bool, want []string) []string {
	var missing []string
	for _, w := range want {
		found := false
		for s := range seen {
			if strings.HasPrefix(s, w) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, w)
		}
	}
	return missing
}

func TestLangSmithGetsARunsSpansOnlyOnceAnAdministratorAllowsIt(t *testing.T) {
	key, product := "llm-key-"+uuid.NewString(), "smith-"+uuid.NewString()[:8]
	script := []map[string]any{{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": script})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	workflowID := createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string)))
	smithKey := "lsv2_pt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	t.Cleanup(func() { alice.api(http.MethodDelete, "/settings/langsmith/key", nil) })

	// Off by default: a run sends LangSmith nothing.
	stub.controlCall(http.MethodDelete, "/control/langsmith", nil, http.StatusNoContent)
	if status, setting := alice.api(http.MethodGet, "/settings/langsmith", nil); status != http.StatusOK || setting["enabled"] != false {
		t.Fatalf("the setting reads %d %v", status, setting)
	}
	tracedRun(t, bob, workflowID)
	if calls := langSmithCalls(t, stub); len(calls) != 0 {
		t.Fatalf("LangSmith got %d exports while it was off", len(calls))
	}

	// Turning it on needs a key and the acknowledgement that run data leaves the installation.
	status, answer := alice.api(http.MethodPut, "/settings/langsmith", map[string]any{"enabled": true, "project": "rotor-test", "key": smithKey})
	if status != http.StatusUnprocessableEntity || errorCode(answer) != "data_leaves_unacknowledged" {
		t.Fatalf("turning it on unacknowledged = %d %v", status, answer)
	}
	if status, _ := bob.api(http.MethodPut, "/settings/langsmith", map[string]any{"enabled": false}); status != http.StatusForbidden {
		t.Errorf("a member changing the setting = %d, want 403", status)
	}
	status, answer = alice.api(http.MethodPut, "/settings/langsmith",
		map[string]any{"enabled": true, "project": "rotor-test", "key": smithKey, "acknowledgeDataLeaves": true})
	if status != http.StatusOK || answer["enabled"] != true || answer["keySet"] != true || strings.Contains(stringify(answer), smithKey) {
		t.Fatalf("turning it on = %d %v", status, answer)
	}

	trace := tracedRun(t, bob, workflowID)
	var body []byte
	deadline := time.Now().Add(traceWait)
	for time.Now().Before(deadline) && !bytes.Contains(body, []byte("node ask")) {
		time.Sleep(time.Second)
		body = nil
		for _, c := range langSmithCalls(t, stub) {
			if c.APIKey != smithKey || c.Project != "rotor-test" {
				t.Fatalf("an export reached LangSmith with key %q and project %q", c.APIKey, c.Project)
			}
			body = append(body, c.Body...)
		}
	}
	if !bytes.Contains(body, []byte("node ask")) || !bytes.Contains(body, []byte("gen_ai.prompt.0.content")) {
		t.Fatalf("LangSmith did not get the run's spans with their prompts for trace %s", trace)
	}
	for _, secret := range []string{key, "stub_oat_", smithKey} {
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("what LangSmith got holds %q", secret)
		}
	}

	// Forgetting the key turns it off again; the audit log says who changed it, never the key.
	if status, answer := alice.api(http.MethodDelete, "/settings/langsmith/key", nil); status != http.StatusOK || answer["enabled"] != false || answer["keySet"] != false {
		t.Errorf("forgetting the key = %d %v", status, answer)
	}
	h := newHarness(t)
	var entries string
	if err := h.super.QueryRow(t.Context(), `SELECT coalesce(string_agg(data::text, ' '), '') FROM audit_log WHERE action = 'langsmith.changed'`).Scan(&entries); err != nil ||
		!strings.Contains(entries, `"key": "set"`) || strings.Contains(entries, smithKey) {
		t.Errorf("the audit log holds %q (%v)", entries, err)
	}
}

// langSmithCall is one export the stand-in kept.
type langSmithCall struct {
	APIKey  string `json:"apiKey"`
	Project string `json:"project"`
	Body    []byte `json:"body"`
}

func langSmithCalls(t *testing.T, stub *toolStub) []langSmithCall {
	t.Helper()
	answer := stub.controlCall(http.MethodGet, "/control/langsmith", nil, http.StatusOK)
	raw, _ := json.Marshal(answer["calls"])
	var calls []langSmithCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		t.Fatalf("the stand-in's calls do not read: %v", err)
	}
	return calls
}
