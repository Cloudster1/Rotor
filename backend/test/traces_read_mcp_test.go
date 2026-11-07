//go:build integration

package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// An assistant reads a large trace shortened to fit a model (#244): failed spans and their ancestors first, at most
// MCPTraceMaxSpans, values cut and given while the budget lasts, and one span whole cut to fit a tool's answer.
func TestAnAssistantReadsALargeTraceShortenedToItsBudget(t *testing.T) {
	h := newHarness(t)
	keepAssistantsOn(t, h)
	ctx := context.Background()
	person := h.addPerson(t, "member")
	b := h.browserFor(t, person)
	r := h.runWithTrace(t, person)
	h.keepContent(t, true)

	at := time.Now()
	text := strings.Repeat("x", 5000)
	root := stepSpan(r, trace.SpanID{}, "run", "CHAIN", text, at)
	spans := []observability.RunnerSpan{root}
	for i := range config.MCPTraceMaxSpans + 100 {
		spans = append(spans, stepSpan(r, root.SpanID, fmt.Sprintf("tool %d", i), "TOOL", text, at.Add(time.Duration(i+1)*time.Millisecond)))
	}
	last := spans[len(spans)-1]
	failed := stepSpan(r, last.SpanID, "ChatOpenAI", "LLM", text, last.End)
	failed.Failed, failed.Message = true, "the model refused"
	failed.Events = []observability.SpanEvent{{Name: "exception", Time: last.End, Attributes: map[string]string{
		"exception.type": "RateLimitError", "exception.stacktrace": strings.Repeat("s", config.MaxSpanStackChars)}}}
	big := stepSpan(r, root.SpanID, "ChatAnthropic", "LLM", strings.Repeat("y", config.MaxSpanContentChars), at)
	for i := range 6 {
		big.Attributes[fmt.Sprintf("llm.input_messages.%d.message.content", i)] = strings.Repeat("m", config.MaxSpanContentChars)
	}
	spans = append(spans, failed, big)
	if err := h.traceStore(t).Keep(ctx, r.id, r.dataKey, observability.AllTracing, spans); err != nil {
		t.Fatal(err)
	}

	_, access := connectHand(t, h, b, map[string]any{"allow": true})
	called := callTool(t, access, "get_run_trace", map[string]any{"runID": r.id.String()})
	if size := len(toolText(called)); size > config.MCPTraceBudgetBytes+1 {
		t.Errorf("get_run_trace answers %d bytes, over its budget of %d", size, config.MCPTraceBudgetBytes)
	}
	shortened := structured(t, called)
	if got := shortened["trace"].(map[string]any)["spans"]; got != float64(len(spans)) {
		t.Errorf("the shortened trace counts %v spans, want all %d it holds", got, len(spans))
	}
	count, valued := 0, 0
	var found map[string]any
	eachSpan(rootsOf(shortened), func(s map[string]any) {
		count++
		if s["spanId"] == failed.SpanID.String() {
			found = s
		}
		if in, ok := s["input"].(map[string]any); ok {
			valued++
			if utf8.RuneCountInString(in["value"].(string)) > config.MCPTraceValueChars || in["cut"] != true {
				t.Errorf("span %v's input is %d characters", s["name"], len(in["value"].(string)))
			}
		}
		if _, ok := s["attributes"].(map[string]any)["metadata"]; ok {
			t.Errorf("span %v keeps every attribute", s["name"])
		}
	})
	if count > config.MCPTraceMaxSpans || count >= len(spans) {
		t.Errorf("get_run_trace answers %d of %d spans", count, len(spans))
	}
	if found == nil || found["input"] == nil || found["status"] != "error" {
		t.Fatalf("the failed span reads %v", found)
	}
	if stack := found["events"].([]any)[0].(map[string]any)["attributes"].(map[string]any)["exception.stacktrace"].(string); len(stack) != config.MCPTraceValueChars {
		t.Errorf("the failed span's stack is %d characters", len(stack))
	}
	if path := pathTo(rootsOf(shortened), "ChatOpenAI"); len(path) != 3 || path[0] != "run" {
		t.Errorf("the failed span nests as %v", path)
	}
	if omitted := rootsOf(shortened)[0].(map[string]any)["omitted"].(float64); omitted == 0 {
		t.Error("the run's span does not count the children it lost")
	}
	if valued == 0 || valued >= count {
		t.Errorf("%d of %d spans carry values, though the budget should run out", valued, count)
	}

	whole := callTool(t, access, "get_run_trace_span", map[string]any{"runID": r.id.String(), "spanID": big.SpanID.String()})
	if size := len(toolText(whole)); size > config.MCPMaxToolResultBytes {
		t.Errorf("get_run_trace_span answers %d bytes", size)
	}
	span := structured(t, whole)["span"].(map[string]any)
	if in := span["input"].(map[string]any); in["cut"] != true || len(in["value"].(string)) >= config.MaxSpanContentChars {
		t.Errorf("the large span's input is %d characters, cut %v", len(in["value"].(string)), in["cut"])
	}
	if content := span["content"].(map[string]any); len(content) != 8 {
		t.Errorf("the large span keeps %d text attributes", len(content))
	}
	// The browser reads the same span whole.
	page := traceAnswer(t, b, "/runs/"+r.id.String()+"/trace/spans/"+big.SpanID.String())["span"].(map[string]any)
	if len(page["input"].(map[string]any)["value"].(string)) != config.MaxSpanContentChars {
		t.Error("the API cuts one span as MCP does")
	}
}
