package observability

import (
	"context"
	"crypto/rand"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Every run is traced, since Rotor keeps its trace (#242): a run takes the sampled trace it was started in, or a
// fresh one of its own, and the operator's collector takes a share of runs as ROTOR_TRACE_SAMPLE_RATIO says.

// RunTraceparent is the trace context a run carries: the one on ctx when it is sampled, else a fresh sampled one.
func RunTraceparent(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() && sc.IsSampled() {
		return Inject(ctx)
	}
	return FreshTraceparent()
}

// FreshTraceparent is a new sampled trace context: a new trace id and a random span id as the run's parent.
func FreshTraceparent() string {
	var tid trace.TraceID
	var sid trace.SpanID
	for !tid.IsValid() {
		// crypto/rand.Read never returns an error since Go 1.24; it crashes the program instead.
		_, _ = rand.Read(tid[:])
	}
	for !sid.IsValid() {
		_, _ = rand.Read(sid[:])
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	return Inject(trace.ContextWithSpanContext(context.Background(), sc))
}

// TraceIDOf is a traceparent's trace id, invalid for none.
func TraceIDOf(traceparent string) trace.TraceID {
	return trace.SpanContextFromContext(Extract(context.Background(), traceparent)).TraceID()
}

// Keeps says whether the collector takes a trace Rotor sampled itself, as the sample ratio says of its id.
func (t *Tracing) Keeps(id trace.TraceID) bool {
	if t == nil || t.ratio == nil {
		return false
	}
	return t.ratio.ShouldSample(sdktrace.SamplingParameters{TraceID: id}).Decision == sdktrace.RecordAndSample
}

// ForCollector is a run's traceparent as the process's own spans continue it: unsampled when the collector does not
// keep the run's trace, so the worker's spans of a run go where its runner's do.
func (t *Tracing) ForCollector(traceparent string) string {
	sc := trace.SpanContextFromContext(Extract(context.Background(), traceparent))
	if t == nil || !sc.IsValid() || !sc.IsSampled() || t.Keeps(sc.TraceID()) {
		return traceparent
	}
	unsampled := sc.WithTraceFlags(sc.TraceFlags().WithSampled(false))
	return Inject(trace.ContextWithSpanContext(context.Background(), unsampled))
}
