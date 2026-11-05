package runnerctl

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// handOffWait bounds how long a test waits for what the control plane hands on, or for it to hand on without waiting.
const handOffWait = 5 * time.Second

type keptSpans struct {
	mu    sync.Mutex
	spans []observability.RunnerSpan
	sent  chan struct{}
}

func (k *keptSpans) Spans(_ context.Context, _ uuid.UUID, spans []observability.RunnerSpan) {
	k.mu.Lock()
	k.spans = append(k.spans, spans...)
	k.mu.Unlock()
	k.sent <- struct{}{}
}

// kept waits for a batch to arrive when one is due, and answers every span kept so far.
func (k *keptSpans) kept(t *testing.T, due bool) []observability.RunnerSpan {
	t.Helper()
	if due {
		select {
		case <-k.sent:
		case <-time.After(handOffWait):
			t.Fatal("the spans were never sent on")
		}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]observability.RunnerSpan(nil), k.spans...)
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
	kept := &keptSpans{sent: make(chan struct{}, 1)}
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
	spans := kept.kept(t, true)
	if len(spans) != 1 {
		t.Fatalf("kept %d spans, want only the one of the run's trace", len(spans))
	}
	got := spans[0]
	if strings.Contains(got.Attributes["token"], "stub_oat_") || len(got.Attributes["long"]) != config.MaxSpanAttrChars {
		t.Errorf("the span's attributes went on as %v", got.Attributes)
	}

	untraced := &run{ID: uuid.New()}
	s.spans(context.Background(), untraced, &runnerpb.SpanBatch{Spans: []*runnerpb.RunnerSpan{span(trace, "00f067aa0ba902b9", nil)}})
	if len(kept.kept(t, false)) != 1 {
		t.Error("a run without a trace had spans sent on")
	}

	content := WithoutContent([]observability.RunnerSpan{{Attributes: map[string]string{
		"gen_ai.prompt.0.content": "hi", "gen_ai.completion.0.content": "hello", "gen_ai.request.model": "m"}}})
	if attrs := content[0].Attributes; len(attrs) != 1 || attrs["gen_ai.request.model"] != "m" {
		t.Errorf("the collector would get %v; prompts and answers are LangSmith's alone", attrs)
	}
}

// stuckCollector answers only once the test lets it, as a collector that is down never does.
type stuckCollector struct {
	calls   chan context.Context
	release chan struct{}
}

func (c *stuckCollector) Spans(ctx context.Context, _ uuid.UUID, _ []observability.RunnerSpan) {
	c.calls <- ctx
	select {
	case <-c.release:
	case <-ctx.Done():
	}
}

func (c *stuckCollector) Content(context.Context) bool { return false }

// A runner sends its last spans right before its Done, on the same stream (#234): were the stream to wait for the
// collector, a run would reach waiting, and open its approval, only once the export gave up a minute later.
func TestACollectorThatDoesNotAnswerNeverHoldsUpARunsStream(t *testing.T) {
	const trace = "0af7651916cd43dd8448eb211c80319c"
	stuck := &stuckCollector{calls: make(chan context.Context, config.MaxRunnerSpanExports+1), release: make(chan struct{})}
	defer close(stuck.release)
	s := &Server{Telemetry: stuck}
	r := &run{ID: uuid.New(), Traceparent: "00-" + trace + "-b7ad6b7169203331-01"}
	now := timestamppb.New(time.Now())
	batch := &runnerpb.SpanBatch{Spans: []*runnerpb.RunnerSpan{
		{TraceId: hexBytes(t, trace), SpanId: hexBytes(t, "00f067aa0ba902b7"), Name: "node review", Start: now, End: now}}}

	stream, end := context.WithCancel(context.Background())
	handled := make(chan struct{})
	go func() {
		for range config.MaxRunnerSpanExports + 1 {
			s.spans(stream, r, batch)
		}
		close(handled)
	}()
	select {
	case <-handled:
	case <-time.After(handOffWait):
		t.Fatal("the stream waited for the collector, and so would the run's Done behind the spans")
	}
	end()

	for range config.MaxRunnerSpanExports {
		select {
		case ctx := <-stuck.calls:
			deadline, bounded := ctx.Deadline()
			if !bounded || time.Until(deadline) > config.RunnerSpanExportTimeout {
				t.Errorf("an export may take until %v, want at most %s", deadline, config.RunnerSpanExportTimeout)
			}
			if ctx.Err() != nil {
				t.Error("the stream's end cut short an export of the spans sent right before Done")
			}
		case <-time.After(handOffWait):
			t.Fatal("a batch within the export slots was never sent on")
		}
	}
	select {
	case <-stuck.calls:
		t.Error("a batch beyond the export slots was sent on instead of dropped")
	default:
	}
}
