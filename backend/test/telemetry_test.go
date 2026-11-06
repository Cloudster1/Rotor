//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// jaegerTrace is a trace as Jaeger's query API answers it: its spans with their tags, and the service each comes from.
type jaegerTrace struct {
	Data []struct {
		Spans []struct {
			OperationName string `json:"operationName"`
			ProcessID     string `json:"processID"`
			Tags          []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"tags"`
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

	// OpenInference's spans (#241) join Rotor's: the graph, the model call and the tool call that made the MCP call.
	want := []string{"rotor-api POST /api/v1/workflows/{workflowID}/runs", "rotor-api run.start", "rotor-worker run.start_sandbox",
		"rotor-runner run", "rotor-runner node ask", "rotor-runner LangGraph", "rotor-runner whoami", "rotor-runner mcp whoami",
		"rotor-runner kind LLM", "rotor-runner kind AGENT", "rotor-runner tag gen_ai.request.model", "rotor-runner tag rotor.workflow"}
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
					service := d.Processes[s.ProcessID].ServiceName
					seen[service+" "+s.OperationName] = true
					for _, tag := range s.Tags {
						seen[service+" tag "+tag.Key] = true
						if tag.Key == "openinference.span.kind" {
							seen[fmt.Sprint(service, " kind ", tag.Value)] = true
						}
					}
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
	for _, text := range []string{"input.value", "output.value", "llm.input_messages", "llm.output_messages"} {
		if found := missingSpans(seen, []string{"rotor-runner tag " + text}); len(found) == 0 {
			t.Errorf("the collector got %s, and it never gets a span's text", text)
		}
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

// LangSmith is a destination like any other since #246, sent to with its key and project as headers; the routes of the
// LangSmith setting went with it (#256).
func TestLangSmithGetsARunsSpansOnlyOnceAnAdministratorAllowsIt(t *testing.T) {
	key, product := "llm-key-"+uuid.NewString(), "smith-"+uuid.NewString()[:8]
	script := []map[string]any{{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."},
		{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": script})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	workflowID := createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string)))
	smithKey := "lsv2_pt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sink, endpoint := newSink(t, stub)

	for _, route := range []struct{ method, path string }{{http.MethodGet, "/settings/langsmith"},
		{http.MethodPut, "/settings/langsmith"}, {http.MethodDelete, "/settings/langsmith/key"}} {
		if status, answer := alice.api(route.method, route.path, map[string]any{"enabled": false}); status != http.StatusNotFound {
			t.Errorf("%s %s = %d %v, want 404 since #256", route.method, route.path, status, answer)
		}
	}

	// Off, as the LangSmith preset adds it: a run sends LangSmith nothing.
	name := "LangSmith " + sink
	headers := map[string]string{"x-api-key": smithKey, "Langsmith-Project": "rotor-test"}
	d := addDestination(t, alice, map[string]any{"name": name, "endpoint": endpoint, "headers": headers})
	id := d["id"].(string)
	startTraced(t, bob, workflowID)
	if calls := sinkCalls(t, stub, sink); len(calls) != 0 {
		t.Fatalf("LangSmith got %d exports while it was off", len(calls))
	}

	// On with text, once acknowledged: the next run's spans reach it with their prompts and its headers.
	status, answer := alice.api(http.MethodPut, "/settings/trace-destinations/"+id, map[string]any{"name": name,
		"endpoint": endpoint, "enabled": true, "content": true, "acknowledgeContentLeaves": true})
	if status != http.StatusOK || strings.Contains(stringify(answer), smithKey) {
		t.Fatalf("turning it on = %d %v", status, answer)
	}
	_, trace := startTraced(t, bob, workflowID)
	body, calls := sinkBody(t, stub, sink, modelPromptKey)
	for _, c := range calls {
		if c.Headers["x-api-key"] != smithKey || c.Headers["langsmith-project"] != "rotor-test" {
			t.Fatalf("an export reached LangSmith with headers %v", c.Headers)
		}
	}
	if !bytes.Contains(body, []byte("node ask")) {
		t.Fatalf("LangSmith did not get the run's spans for trace %s", trace)
	}
	for _, secret := range []string{key, "stub_oat_", smithKey} {
		if bytes.Contains(body, []byte(secret)) {
			t.Errorf("what LangSmith got holds %q", secret)
		}
	}
}
