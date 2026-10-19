package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// RunnerService names the runner's spans, which the control plane sends on for it.
const RunnerService = "rotor-runner"

// RunnerSpan is a span a runner sent over its stream, already checked and cut down by the control plane.
type RunnerSpan struct {
	TraceID    trace.TraceID
	SpanID     trace.SpanID
	ParentID   trace.SpanID
	Name       string
	Start, End time.Time
	Attributes map[string]string
	Failed     bool
	Message    string
}

// RunnerSnapshots are runner spans as the exporters take them, under the service rotor-runner of env.
func RunnerSnapshots(env string, spans []RunnerSpan) []sdktrace.ReadOnlySpan {
	res := serviceResource(RunnerService, env)
	stubs := make(tracetest.SpanStubs, 0, len(spans))
	for _, s := range spans {
		stub := tracetest.SpanStub{
			Name:      s.Name,
			StartTime: s.Start,
			EndTime:   s.End,
			SpanKind:  trace.SpanKindInternal,
			Resource:  res,
			SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: s.TraceID, SpanID: s.SpanID, TraceFlags: trace.FlagsSampled,
			}),
		}
		if s.ParentID.IsValid() {
			stub.Parent = trace.NewSpanContext(trace.SpanContextConfig{TraceID: s.TraceID, SpanID: s.ParentID, TraceFlags: trace.FlagsSampled, Remote: true})
		}
		for k, v := range s.Attributes {
			stub.Attributes = append(stub.Attributes, attribute.String(k, v))
		}
		if s.Failed {
			stub.Status = sdktrace.Status{Code: codes.Error, Description: s.Message}
		}
		stubs = append(stubs, stub)
	}
	return stubs.Snapshots()
}

// Env is the deployment the tracer stamps on its spans.
func (t *Tracing) Env() string {
	if t == nil {
		return ""
	}
	return t.env
}

// Export sends spans made elsewhere, such as a runner's, to the process's collector; without one it does nothing.
func (t *Tracing) Export(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if t == nil || len(spans) == 0 {
		return nil
	}
	return t.exporter.ExportSpans(ctx, spans)
}
