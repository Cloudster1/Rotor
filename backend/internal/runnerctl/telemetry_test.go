package runnerctl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

type keptSpans struct{ spans []observability.RunnerSpan }

func (k *keptSpans) Spans(_ context.Context, _ uuid.UUID, spans []observability.RunnerSpan) {
	k.spans = append(k.spans, spans...)
}

func (k *keptSpans) Content(context.Context) bool { return false }

func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	out := make([]byte, len(s)/2)
	for i := range out {
		var b byte
		for _, c := range s[2*i : 2*i+2] {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= byte(c - '0')
			default:
				b |= byte(c-'a') + 10
			}
		}
		out[i] = b
	}
	return out
}

// A runner's spans are trusted as far as the control plane checks them: its own run's trace alone, cut short and
// redacted again, since the sandbox runs code the workflow's author wrote.
func TestARunnersSpansAreCheckedBeforeTheyGoOn(t *testing.T) {
	const trace, other = "0af7651916cd43dd8448eb211c80319c", "11111111111111111111111111111111"
	kept := &keptSpans{}
	s := &Server{Telemetry: kept}
	r := &run{ID: uuid.New(), Traceparent: "00-" + trace + "-b7ad6b7169203331-01"}
	now := timestamppb.New(time.Now())
	span := func(traceID, id string, attrs map[string]string) *runnerpb.RunnerSpan {
		return &runnerpb.RunnerSpan{TraceId: hexBytes(t, traceID), SpanId: hexBytes(t, id), Name: "node a", Start: now, End: now, Attributes: attrs}
	}
	s.spans(context.Background(), r, &runnerpb.SpanBatch{Spans: []*runnerpb.RunnerSpan{
		span(trace, "00f067aa0ba902b7", map[string]string{"token": "Bearer stub_oat_abcdefgh12345678", "long": strings.Repeat("x", 4*config.MaxSpanAttrChars)}),
		span(other, "00f067aa0ba902b8", nil),
		{TraceId: hexBytes(t, trace), SpanId: []byte{1}, Start: now, End: now},
	}})
	if len(kept.spans) != 1 {
		t.Fatalf("kept %d spans, want only the one of the run's trace", len(kept.spans))
	}
	got := kept.spans[0]
	if strings.Contains(got.Attributes["token"], "stub_oat_") || len(got.Attributes["long"]) != config.MaxSpanAttrChars {
		t.Errorf("the span's attributes went on as %v", got.Attributes)
	}

	untraced := &run{ID: uuid.New()}
	s.spans(context.Background(), untraced, &runnerpb.SpanBatch{Spans: []*runnerpb.RunnerSpan{span(trace, "00f067aa0ba902b9", nil)}})
	if len(kept.spans) != 1 {
		t.Error("a run without a trace had spans sent on")
	}

	content := WithoutContent([]observability.RunnerSpan{{Attributes: map[string]string{
		"gen_ai.prompt.0.content": "hi", "gen_ai.completion.0.content": "hello", "gen_ai.request.model": "m"}}})
	if attrs := content[0].Attributes; len(attrs) != 1 || attrs["gen_ai.request.model"] != "m" {
		t.Errorf("the collector would get %v; prompts and answers are LangSmith's alone", attrs)
	}
}
