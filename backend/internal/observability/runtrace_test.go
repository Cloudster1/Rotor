package observability

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Every run is traced (#242): a run keeps the sampled trace it was started in, and gets a fresh sampled one when there
// is none or it was not sampled, whatever the collector would have kept.
func TestEveryRunGetsASampledTrace(t *testing.T) {
	const caller = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got := RunTraceparent(Extract(context.Background(), caller)); got != caller {
		t.Errorf("a run started in a sampled trace carries %q, want %q", got, caller)
	}
	for name, ctx := range map[string]context.Context{
		"no trace":         context.Background(),
		"an unsampled one": Extract(context.Background(), "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"),
	} {
		got := RunTraceparent(ctx)
		sc := trace.SpanContextFromContext(Extract(context.Background(), got))
		if !sc.IsValid() || !sc.IsSampled() || strings.Contains(got, "4bf92f3577b34da6a3ce929d0e0e4736") {
			t.Errorf("a run started with %s carries %q, want a fresh sampled trace", name, got)
		}
	}
	if FreshTraceparent() == FreshTraceparent() {
		t.Error("two fresh traces are the same")
	}
}

// The operator's collector keeps the share of runs the sample ratio says, by trace id, and the worker's spans of a run
// follow its runner's.
func TestTheCollectorKeepsTheShareOfRunsTheRatioSays(t *testing.T) {
	const run = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	none := &Tracing{ratio: sdktrace.TraceIDRatioBased(0)}
	all := &Tracing{ratio: sdktrace.TraceIDRatioBased(1)}
	id := TraceIDOf(run)
	if none.Keeps(id) || !all.Keeps(id) || (*Tracing)(nil).Keeps(id) {
		t.Error("the collector's share does not follow the ratio")
	}
	if got := all.ForCollector(run); got != run {
		t.Errorf("a kept run's trace continues as %q", got)
	}
	got := none.ForCollector(run)
	if sc := trace.SpanContextFromContext(Extract(context.Background(), got)); sc.IsSampled() || sc.TraceID() != id {
		t.Errorf("a run the collector does not keep continues as %q, want its trace unsampled", got)
	}
}
