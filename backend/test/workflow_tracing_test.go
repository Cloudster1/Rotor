//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// tracingSetup is a workflow of bob's whose runs make a model call, and alice, who administers Rotor.
type tracingSetup struct {
	stub       *toolStub
	alice, bob *browser
	workflow   func() string
}

func newTracingSetup(t *testing.T) tracingSetup {
	t.Helper()
	key, product := "llm-key-"+uuid.NewString(), "wft-"+uuid.NewString()[:8]
	// The last turn answers every call after the script runs out, so any number of runs make one model call each.
	stub := newToolStub(t, map[string]any{"product": product, "llmKey": key, "llmScript": []map[string]any{{"content": "You are bob."}}})
	bob, _, _ := connected(t, stub)
	alice := newBrowser(t)
	alice.signIn("alice")
	provider := addProvider(t, alice, stub, key)["name"].(string)
	return tracingSetup{stub: stub, alice: alice, bob: bob,
		workflow: func() string { return createWorkflow(t, bob, agentWorkflow(product, provider)) }}
}

// setTracing changes a workflow's tracing as b and answers what it now reads.
func setTracing(t *testing.T, b *browser, workflowID string, body map[string]any) map[string]any {
	t.Helper()
	status, answer := b.api(http.MethodPut, "/workflows/"+workflowID+"/settings/tracing", body)
	if status != http.StatusOK {
		t.Fatalf("setting the workflow's tracing to %v = %d %v", body, status, answer)
	}
	return answer
}

// ofTrace is the exports a sink got that carry spans of a trace, by its id's bytes in the protobuf.
func ofTrace(t *testing.T, calls []sinkCall, trace string) []sinkCall {
	t.Helper()
	raw, err := hex.DecodeString(trace)
	if err != nil {
		t.Fatal(err)
	}
	var out []sinkCall
	for _, c := range calls {
		if bytes.Contains(c.Body, raw) {
			out = append(out, c)
		}
	}
	return out
}

// sinkGets waits until a sink got spans of a trace, and answers its exports of it.
func sinkGets(t *testing.T, stub *toolStub, sink, trace string) []sinkCall {
	t.Helper()
	var got []sinkCall
	if !eventually(func() bool {
		got = ofTrace(t, sinkCalls(t, stub, sink), trace)
		return len(got) > 0
	}) {
		t.Fatalf("the sink %s got nothing of trace %s", sink, trace)
	}
	return got
}

// sealedSpans counts a run's spans kept, and those of them holding text.
func (h *harness) sealedSpans(t *testing.T, runID string) (spans, sealed int) {
	t.Helper()
	if err := h.super.QueryRow(context.Background(), `SELECT count(*), count(s.sealed_content) FROM trace_span s
		JOIN trace t ON t.id = s.trace WHERE t.run_id = $1`, runID).Scan(&spans, &sealed); err != nil {
		t.Fatal(err)
	}
	return spans, sealed
}

// A workflow that keeps no text keeps none in Rotor's trace and sends none to a destination that takes text, while the
// installation keeps text and another run of the same workflow, once it inherits again, has it in both places.
func TestAWorkflowWithoutTraceTextKeepsAndSendsNone(t *testing.T) {
	h := newHarness(t)
	s := newTracingSetup(t)
	workflowID := s.workflow()
	h.keepText(t, true)
	sink, endpoint := newSink(t, s.stub)
	addDestination(t, s.alice, map[string]any{"name": "Text " + sink, "endpoint": endpoint, "enabled": true, "content": true,
		"acknowledgeContentLeaves": true})

	none := setTracing(t, s.bob, workflowID, map[string]any{"content": "none", "destinations": nil, "sampleRatio": 1})
	tracing, _ := none["tracing"].(map[string]any)
	installation, _ := none["installation"].(map[string]any)
	if tracing["content"] != "none" || installation["keepContent"] != true || none["canChange"] != true || tracing["changedBy"] == nil {
		t.Fatalf("the workflow's tracing reads %v", none)
	}
	silent, silentTrace := startTraced(t, s.bob, workflowID)

	setTracing(t, s.bob, workflowID, map[string]any{"content": "inherit", "destinations": nil, "sampleRatio": 1})
	told, toldTrace := startTraced(t, s.bob, workflowID)
	// The later run's text reaching the sink means the earlier run's spans, sent before it, have arrived too.
	if !eventually(func() bool {
		for _, c := range ofTrace(t, sinkCalls(t, s.stub, sink), toldTrace) {
			if bytes.Contains(c.Body, []byte(modelPromptKey)) {
				return true
			}
		}
		return false
	}) {
		t.Fatal("a run that inherits never sent its text to a destination that takes it")
	}
	for _, c := range sinkGets(t, s.stub, sink, silentTrace) {
		for _, text := range []string{modelPromptKey, "input.value", "Who am I?"} {
			if bytes.Contains(c.Body, []byte(text)) {
				t.Errorf("a run of a workflow without text sent %q to a destination", text)
			}
		}
	}

	var spans, sealed int
	if !eventually(func() bool { spans, sealed = h.sealedSpans(t, told); return sealed > 0 }) {
		t.Fatalf("a run that inherits kept %d spans, none with text, while the installation keeps text", spans)
	}
	if spans, sealed = h.sealedSpans(t, silent); spans == 0 || sealed != 0 {
		t.Errorf("a run of a workflow without text kept %d spans, %d with text", spans, sealed)
	}

	var entries string
	if err := h.super.QueryRow(context.Background(), `SELECT coalesce(string_agg(data::text, ' | ' ORDER BY id), '') FROM audit_log
		WHERE action = 'workflow.tracing_changed' AND target_id = $1`, workflowID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entries, `"content": "none"`) || !strings.Contains(entries, `"content": "inherit"`) {
		t.Errorf("the audit log reads %s", entries)
	}
}

// A workflow left out of a destination sends it nothing while another workflow's runs still go there, and a share of
// 0 sends nothing anywhere, though Rotor keeps the run's trace.
func TestAWorkflowLeftOutOfADestinationSendsItNothing(t *testing.T) {
	h := newHarness(t)
	s := newTracingSetup(t)
	chosen, all := s.workflow(), s.workflow()
	sinkA, endpointA := newSink(t, s.stub)
	sinkB, endpointB := newSink(t, s.stub)
	a := addDestination(t, s.alice, map[string]any{"name": "A " + sinkA, "endpoint": endpointA, "enabled": true})
	addDestination(t, s.alice, map[string]any{"name": "B " + sinkB, "endpoint": endpointB, "enabled": true})

	answer := setTracing(t, s.bob, chosen, map[string]any{"content": "inherit", "destinations": []string{a["id"].(string)},
		"sampleRatio": 1})
	if got := stringify(answer["tracing"].(map[string]any)["destinations"]); got != `["`+a["id"].(string)+`"]` {
		t.Fatalf("the workflow's destinations read %s", got)
	}
	_, onlyA := startTraced(t, s.bob, chosen)
	_, everywhere := startTraced(t, s.bob, all)
	sinkGets(t, s.stub, sinkA, onlyA)
	sinkGets(t, s.stub, sinkA, everywhere)
	sinkGets(t, s.stub, sinkB, everywhere)
	if got := ofTrace(t, sinkCalls(t, s.stub, sinkB), onlyA); len(got) != 0 {
		t.Errorf("a destination the workflow left out got %d exports of its run", len(got))
	}

	setTracing(t, s.bob, chosen, map[string]any{"content": "inherit", "destinations": nil, "sampleRatio": 0})
	unsampled, unsampledTrace := startTraced(t, s.bob, chosen)
	_, after := startTraced(t, s.bob, all)
	sinkGets(t, s.stub, sinkA, after)
	sinkGets(t, s.stub, sinkB, after)
	for _, sink := range []string{sinkA, sinkB} {
		if got := ofTrace(t, sinkCalls(t, s.stub, sink), unsampledTrace); len(got) != 0 {
			t.Errorf("a share of 0 sent %d exports to %s", len(got), sink)
		}
	}
	if !eventually(func() bool { spans, _ := h.sealedSpans(t, unsampled); return spans > 0 }) {
		t.Error("Rotor did not keep the trace of a run its workflow sends nowhere")
	}
}

// A span a tool sends into a run of a workflow without text keeps none either, while the installation keeps text.
func TestASpanJoiningARunOfAWorkflowWithoutTextKeepsNone(t *testing.T) {
	h := newHarness(t)
	h.keepText(t, true)
	starter := h.addPerson(t, "member")
	r := h.runWithTrace(t, starter)
	if _, err := h.super.Exec(context.Background(), `INSERT INTO workflow_tracing (workflow_id, content) VALUES ($1, 'none')`,
		r.workflow); err != nil {
		t.Fatal(err)
	}
	tool := appSpan(r.traceID.String(), "whoami", map[string]string{"openinference.span.kind": "TOOL", "input.value": "who am I"})
	if status, _, body := sendSpans(t, otlpCall{token: h.appToken(t, starter, false), contentType: mediaJSON,
		body: jsonOf(t, tool)}); status != http.StatusOK {
		t.Fatalf("sending a span into the run = %d %s", status, body)
	}
	kept, ok := h.keptSpanOf(t, r.traceID.String(), tool.spanID)
	if !ok || kept.runID == nil || *kept.runID != r.id {
		t.Fatalf("the span was kept as %+v, %v", kept, ok)
	}
	if _, plain := kept.attributes["input.value"]; kept.sealed != nil || plain {
		t.Errorf("a span joining a run of a workflow without text kept its text: sealed %v, attributes %v", kept.sealed != nil, kept.attributes)
	}
}
