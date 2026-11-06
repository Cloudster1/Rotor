//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Text an OpenInference span carries for a model call, which a destination gets only once it may.
const modelPromptKey = "llm.input_messages.0.message.content"

// sinkCall is one export a sink of the tool stub kept.
type sinkCall struct {
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body"`
}

// newSink is a sink of the tool stub of the test's own, forgotten when it ends, and the endpoint the api sends it at.
func newSink(t *testing.T, stub *toolStub) (string, string) {
	t.Helper()
	sink := "sink-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	t.Cleanup(func() { stub.controlCall(http.MethodDelete, "/control/otlp/"+sink, nil, http.StatusNoContent) })
	return sink, os.Getenv(envToolStubInternal) + "/otlp/" + sink + "/v1/traces"
}

func sinkCalls(t *testing.T, stub *toolStub, sink string) []sinkCall {
	t.Helper()
	answer := stub.controlCall(http.MethodGet, "/control/otlp/"+sink, nil, http.StatusOK)
	raw, _ := json.Marshal(answer["calls"])
	var calls []sinkCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		t.Fatalf("the sink's calls do not read: %v", err)
	}
	return calls
}

// sinkBody is every export a sink got, once one holds want; it fails the test when none does in time.
func sinkBody(t *testing.T, stub *toolStub, sink, want string) ([]byte, []sinkCall) {
	t.Helper()
	var body []byte
	var calls []sinkCall
	if !eventually(func() bool {
		calls, body = sinkCalls(t, stub, sink), nil
		for _, c := range calls {
			body = append(body, c.Body...)
		}
		return bytes.Contains(body, []byte(want))
	}) {
		t.Fatalf("the destination did not get %q", want)
	}
	return body, calls
}

// startTraced starts a workflow as b under a trace of the test's own, waits for it to succeed, and answers the run's
// id and its trace id.
func startTraced(t *testing.T, b *browser, workflowID string) (string, string) {
	t.Helper()
	trace := strings.ReplaceAll(uuid.NewString(), "-", "")
	status, run := b.apiWith(http.MethodPost, "/workflows/"+workflowID+"/runs", map[string]any{"inputs": map[string]any{}},
		map[string]string{"traceparent": "00-" + trace + "-b7ad6b7169203331-01"})
	if status != http.StatusCreated {
		t.Fatalf("starting a traced run = %d %v", status, run)
	}
	id := run["id"].(string)
	if done := waitRun(t, b, id, "succeeded", "failed"); done["status"] != "succeeded" {
		t.Fatalf("the traced run ended %v: %v", done["status"], done["reason"])
	}
	return id, trace
}

// addDestination adds one as alice, removed when the test ends.
func addDestination(t *testing.T, alice *browser, body map[string]any) map[string]any {
	t.Helper()
	status, answer := alice.api(http.MethodPost, "/settings/trace-destinations", body)
	if status != http.StatusCreated {
		t.Fatalf("adding a destination = %d %v", status, answer)
	}
	d := answer["destination"].(map[string]any)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/settings/trace-destinations/"+d["id"].(string), nil) })
	return d
}

func TestATraceDestinationGetsARunsSpansWithTextOnlyOnceAcknowledged(t *testing.T) {
	key, product := "llm-key-"+uuid.NewString(), "dest-"+uuid.NewString()[:8]
	script := []map[string]any{{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."},
		{"toolCalls": []map[string]any{{"name": "whoami"}}}, {"content": "You are bob."}}
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": script})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)
	workflowID := createWorkflow(t, bob, agentWorkflow(product, provider["name"].(string)))
	sink, endpoint := newSink(t, stub)
	secret := "sk-" + uuid.NewString()
	name := "Sink " + sink

	d := addDestination(t, alice, map[string]any{"name": name, "endpoint": endpoint, "enabled": true,
		"headers": map[string]string{"Authorization": "Bearer " + secret}, "traceLink": "https://traces.example/t/{traceId}?run={runId}"})
	id := d["id"].(string)
	if d["content"] != false || stringify(d["headerNames"]) != `["Authorization"]` || d["headersSetAt"] == nil {
		t.Errorf("the destination reads %v", d)
	}
	// The header's value is never answered back, by the add, the list or the destination.
	_, list := alice.api(http.MethodGet, "/settings/trace-destinations", nil)
	_, one := alice.api(http.MethodGet, "/settings/trace-destinations/"+id, nil)
	for what, answer := range map[string]any{"the add": d, "the list": list, "the destination": one} {
		if strings.Contains(stringify(answer), secret) {
			t.Errorf("%s answers the header's value: %v", what, answer)
		}
	}

	// Without the acknowledgement the destination gets the spans with no text, sent with its header.
	runID, trace := startTraced(t, bob, workflowID)
	body, calls := sinkBody(t, stub, sink, "node ask")
	for _, c := range calls {
		if c.Headers["authorization"] != "Bearer "+secret {
			t.Fatalf("an export reached the destination with authorization %q", c.Headers["authorization"])
		}
	}
	for _, text := range []string{modelPromptKey, "input.value", "Who am I?"} {
		if bytes.Contains(body, []byte(text)) {
			t.Errorf("the destination got %q before text was allowed", text)
		}
	}

	// A run's trace links to it, for whoever reads the run.
	want := fmt.Sprintf("https://traces.example/t/%s?run=%s", trace, runID)
	var links string
	if !eventually(func() bool {
		_, answer := bob.api(http.MethodGet, "/runs/"+runID+"/trace", nil)
		tree, _ := answer["trace"].(map[string]any)
		links = stringify(tree["links"])
		return strings.Contains(links, want)
	}) || !strings.Contains(links, `"destination":"`+name+`"`) {
		t.Errorf("the run's trace links %s, want %s", links, want)
	}

	// Text needs the acknowledgement; with it, the next run's spans carry their prompts.
	change := map[string]any{"name": name, "endpoint": endpoint, "enabled": true, "content": true,
		"traceLink": "https://traces.example/t/{traceId}?run={runId}"}
	if status, answer := alice.api(http.MethodPut, "/settings/trace-destinations/"+id, change); status != http.StatusUnprocessableEntity ||
		errorCode(answer) != "content_leaves_unacknowledged" {
		t.Fatalf("text unacknowledged = %d %v", status, answer)
	}
	change["acknowledgeContentLeaves"] = true
	status, answer := alice.api(http.MethodPut, "/settings/trace-destinations/"+id, change)
	changed, _ := answer["destination"].(map[string]any)
	if status != http.StatusOK || changed["content"] != true || changed["contentAcknowledgedBy"] == nil ||
		stringify(changed["headerNames"]) != `["Authorization"]` {
		t.Fatalf("text acknowledged = %d %v", status, answer)
	}
	stub.controlCall(http.MethodDelete, "/control/otlp/"+sink, nil, http.StatusNoContent)
	startTraced(t, bob, workflowID)
	_, calls = sinkBody(t, stub, sink, modelPromptKey)
	if calls[0].Headers["authorization"] != "Bearer "+secret {
		t.Errorf("leaving the headers out of a change lost them: %q", calls[0].Headers["authorization"])
	}

	// The audit log says what changed, never the header's value.
	h := newHarness(t)
	var entries string
	if err := h.super.QueryRow(t.Context(), `SELECT coalesce(string_agg(action || ' ' || data::text, ' | ' ORDER BY id), '')
		FROM audit_log WHERE target_type = 'trace_destination' AND target_id = $1`, id).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`trace_destination.added`, `"headers": ["Authorization"]`, `trace_destination.changed`,
		`"headers": "kept"`, `"acknowledged": true`} {
		if !strings.Contains(entries, want) {
			t.Errorf("the audit log lacks %s: %s", want, entries)
		}
	}
	if strings.Contains(entries, secret) {
		t.Errorf("the audit log holds the header's value: %s", entries)
	}
}

func TestATraceDestinationIsTestedWithOneSpanWithoutText(t *testing.T) {
	stub := newToolStub(t, map[string]any{})
	alice := newBrowser(t)
	alice.signIn("alice")
	sink, endpoint := newSink(t, stub)
	d := addDestination(t, alice, map[string]any{"name": "Tested " + sink, "endpoint": endpoint,
		"headers": map[string]string{"x-api-key": "tested"}})

	status, answer := alice.api(http.MethodPost, "/settings/trace-destinations/"+d["id"].(string)+"/test", nil)
	if status != http.StatusOK || answer["delivered"] != true || answer["status"] != float64(http.StatusOK) {
		t.Fatalf("testing a destination that answers = %d %v", status, answer)
	}
	calls := sinkCalls(t, stub, sink)
	if len(calls) != 1 || calls[0].Headers["x-api-key"] != "tested" || !bytes.Contains(calls[0].Body, []byte("rotor.test")) {
		t.Errorf("the sink got %d calls: %+v", len(calls), calls)
	}

	gone := addDestination(t, alice, map[string]any{"name": "Nowhere " + sink, "endpoint": os.Getenv(envToolStubInternal) + "/nowhere/v1/traces"})
	status, answer = alice.api(http.MethodPost, "/settings/trace-destinations/"+gone["id"].(string)+"/test", nil)
	if status != http.StatusOK || answer["delivered"] != false || answer["status"] != float64(http.StatusNotFound) || answer["message"] == "" {
		t.Errorf("testing a destination that refuses = %d %v", status, answer)
	}
	if status, _ := alice.api(http.MethodPost, "/settings/trace-destinations/"+uuid.NewString()+"/test", nil); status != http.StatusNotFound {
		t.Errorf("testing no destination = %d", status)
	}
}
