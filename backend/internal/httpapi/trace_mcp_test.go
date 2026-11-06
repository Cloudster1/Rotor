package httpapi

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces"
)

var traceEpoch = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

// spanOfRun is a span of a run with an input and an output of n characters each.
func spanOfRun(i int, parent, kind string, n int) traces.Span {
	start := traceEpoch.Add(time.Duration(i) * time.Millisecond)
	return traces.Span{SpanID: fmt.Sprintf("%016x", i+1), ParentSpanID: parent, Name: fmt.Sprintf("step %d", i), Kind: kind,
		Service: "rotor-runner", Source: "runner", StartedAt: start, EndedAt: start.Add(time.Millisecond), Status: "unset",
		Attributes: map[string]string{"rotor.node": "ask", "metadata": "{}", "llm.invocation_parameters": "{}"},
		Text:       map[string]string{inputKey: strings.Repeat("i", n), outputKey: strings.Repeat("o", n)}}
}

func runTraceOf(spans []traces.Span) traces.Trace {
	run := uuid.New()
	return traces.Trace{TraceID: strings.Repeat("ab", 16), RunID: &run, State: traces.StateKept, Content: true, Spans: len(spans)}
}

// walk visits every span of a shortened tree.
func walk(spans []traceSpan, visit func(traceSpan)) {
	for _, s := range spans {
		visit(s)
		walk(s.Children, visit)
	}
}

func TestATraceTreePreviewsInputsAndSaysWhatWasCut(t *testing.T) {
	root := spanOfRun(0, "", "CHAIN", config.MaxSpanContentChars)
	child := spanOfRun(1, root.SpanID, "LLM", 10)
	child.Text = nil
	tree := toTraceTree(runTraceOf(nil), traces.Tree([]traces.Span{child, root}), config.TracePreviewChars)
	if len(tree.Roots) != 1 || len(tree.Roots[0].Children) != 1 || tree.Links == nil {
		t.Fatalf("the tree is %+v", tree)
	}
	in := tree.Roots[0].Input
	if in == nil || utf8.RuneCountInString(in.Value) != config.TracePreviewChars || !in.Cut {
		t.Errorf("the root's input previews as %+v", in)
	}
	if got := tree.Roots[0].Children[0]; got.Input != nil || got.Output != nil || got.Attributes["metadata"] != "{}" {
		t.Errorf("a span without text answers %+v", got.spanFields)
	}
	whole := toSpanDetail(root)
	if utf8.RuneCountInString(whole.Input.Value) != config.MaxSpanContentChars || !whole.Input.Cut || len(whole.Content) != 2 {
		t.Errorf("one span whole answers %d characters, cut %v, content %d", len(whole.Input.Value), whole.Input.Cut, len(whole.Content))
	}
}

func TestAModelReadsAtMostItsSpansFailedOnesAndTheirAncestorsFirst(t *testing.T) {
	// A chain of a root, many steps under it, and a failure deep in the last step, after the cap.
	spans := []traces.Span{spanOfRun(0, "", "CHAIN", 10)}
	for i := 1; i <= config.MCPTraceMaxSpans+50; i++ {
		spans = append(spans, spanOfRun(i, spans[0].SpanID, "TOOL", 10))
	}
	last := spans[len(spans)-1]
	failed := spanOfRun(len(spans), last.SpanID, "LLM", 10)
	failed.Status, failed.StatusMessage = failedStatus, "the model refused"
	failed.Events = []traces.Event{{Name: exceptionEvent, Time: traceEpoch, Attributes: map[string]string{
		"exception.stacktrace": strings.Repeat("s", config.MaxSpanStackChars)}}, {Name: "retry", Time: traceEpoch}}
	spans = append(spans, failed)

	tree := forModel(runTraceOf(spans), traces.Tree(spans))
	if size := jsonSize(traceResponse{Trace: tree}); size > config.MCPTraceBudgetBytes {
		t.Errorf("the answer is %d bytes, over the budget of %d", size, config.MCPTraceBudgetBytes)
	}
	count, found := 0, false
	walk(tree.Roots, func(s traceSpan) {
		count++
		if s.SpanID == failed.SpanID {
			found = true
			if s.Input == nil || s.Output == nil {
				t.Error("the failed span's input and output are left out")
			}
			if len(s.Events) != 1 || utf8.RuneCountInString(s.Events[0].Attributes["exception.stacktrace"]) != config.MCPTraceValueChars {
				t.Errorf("the failed span's events are %+v", s.Events)
			}
		}
		if _, ok := s.Attributes["metadata"]; ok || s.Attributes["rotor.node"] != "ask" {
			t.Errorf("span %s keeps the attributes %v", s.SpanID, s.Attributes)
		}
	})
	if !found {
		t.Fatal("the failed span is left out")
	}
	if count > config.MCPTraceMaxSpans {
		t.Errorf("the answer holds %d spans, over %d", count, config.MCPTraceMaxSpans)
	}
	root := tree.Roots[0]
	if root.Omitted != len(spans)-2-(count-2) || root.Omitted == 0 {
		t.Errorf("the root says it omitted %d of its %d children, %d kept", root.Omitted, len(spans)-2, len(root.Children))
	}
	if root.Children[len(root.Children)-1].SpanID != last.SpanID {
		t.Error("the failed span's parent is not the last child kept")
	}
}

func TestAModelReadsValuesCutForFailedSpansThenTheLatestModelAndToolCalls(t *testing.T) {
	spans := []traces.Span{spanOfRun(0, "", "CHAIN", 5000)}
	for i := 1; i <= 60; i++ {
		kind := "CHAIN"
		if i%2 == 0 {
			kind = "LLM"
		}
		spans = append(spans, spanOfRun(i, spans[0].SpanID, kind, 5000))
	}
	spans[3].Status = failedStatus
	tree := forModel(runTraceOf(spans), traces.Tree(spans))
	if size := jsonSize(traceResponse{Trace: tree}); size > config.MCPTraceBudgetBytes {
		t.Errorf("the answer is %d bytes, over the budget of %d", size, config.MCPTraceBudgetBytes)
	}
	byID := map[string]traceSpan{}
	walk(tree.Roots, func(s traceSpan) { byID[s.SpanID] = s })
	if v := byID[spans[3].SpanID].Input; v == nil || utf8.RuneCountInString(v.Value) != config.MCPTraceValueChars || !v.Cut {
		t.Errorf("the failed chain's input is %+v", v)
	}
	if byID[spans[60].SpanID].Input == nil {
		t.Error("the latest model call's input is left out")
	}
	if byID[spans[2].SpanID].Input != nil {
		t.Error("the earliest model call's input fits though later ones came first and the budget ran out")
	}
	if byID[spans[1].SpanID].Input != nil || byID[spans[0].SpanID].Input != nil {
		t.Error("a chain that did not fail is given its input")
	}
}

func TestOneSpanIsCutAlikeToFitAToolsAnswer(t *testing.T) {
	sp := spanOfRun(0, "", "LLM", config.MaxSpanContentChars)
	for i := range 6 {
		sp.Text[fmt.Sprintf("llm.input_messages.%d.message.content", i)] = strings.Repeat("m", config.MaxSpanContentChars)
	}
	whole := traceSpanResponse{Span: toSpanDetail(sp)}
	if jsonSize(whole) <= config.MCPMaxToolResultBytes {
		t.Fatal("the span fits whole, so the test proves nothing")
	}
	fit := fitSpan(whole)
	if size := jsonSize(fit) + encoderNewline; size > config.MCPMaxToolResultBytes || size < config.MCPMaxToolResultBytes*3/4 {
		t.Errorf("the span answers %d bytes for a cap of %d", size, config.MCPMaxToolResultBytes)
	}
	if !fit.Span.Input.Cut || len(fit.Span.Content) != 8 || fit.Span.Content[inputKey] != fit.Span.Input.Value {
		t.Errorf("the cut span is %+v", fit.Span.Input)
	}
	small := traceSpanResponse{Span: toSpanDetail(spanOfRun(1, "", "LLM", 10))}
	if got := fitSpan(small); got.Span.Input.Cut || got.Span.Input.Value != small.Span.Input.Value {
		t.Error("a span that fits is cut")
	}
}
