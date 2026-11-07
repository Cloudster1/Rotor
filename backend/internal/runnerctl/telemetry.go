package runnerctl

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// Telemetry is where a run's spans go once the control plane checked them (#37); *tracedest.Forwarder is the real
// one, sending them to the collector and to the trace destinations an administrator chose (#246).
type Telemetry interface {
	// Spans sends a run's spans on by ctx's deadline, where its workflow's choice says; no stream waits for it.
	Spans(ctx context.Context, runID uuid.UUID, choice observability.RunTracing, spans []observability.RunnerSpan)
	// Content says whether a destination the run's trace goes to may get text, so its spans carry it (#241, #246,
	// #259). Each place that may not keep text gets the spans without it.
	Content(ctx context.Context, choice observability.RunTracing, traceID trace.TraceID) bool
}

// traceContent says whether a run starting now sends its spans' text: never when its workflow chose none (#259),
// else while Rotor keeps text or a destination its trace goes to takes it.
func (s *Server) traceContent(ctx context.Context, r *run) bool {
	if r.tracing.NoText {
		return false
	}
	traceID := trace.SpanContextFromContext(observability.Extract(context.Background(), r.Traceparent)).TraceID()
	return (s.Traces != nil && s.Traces.KeepContent(ctx)) || (s.Telemetry != nil && s.Telemetry.Content(ctx, r.tracing, traceID))
}

// runnerRotorKeys are the rotor.* attributes a runner writes besides the run and the workflow, which the control
// plane sets from the run; any other rotor.* key is dropped, since the sandbox runs the author's code.
var runnerRotorKeys = map[string]bool{"rotor.node": true, "rotor.node.type": true, "rotor.binding": true,
	"rotor.interrupted": true, "rotor.messages_omitted": true, observability.ContentCut: true}

// spans checks a runner's batch and sends it on: only spans of the run's own trace, at most so many, held to the span
// limits and redacted again, since the sandbox runs code the workflow's author wrote.
func (s *Server) spans(ctx context.Context, r *run, batch *runnerpb.SpanBatch) {
	if (s.Telemetry == nil && s.Traces == nil) || batch == nil {
		return
	}
	want := observability.Extract(context.Background(), r.Traceparent)
	traceID := trace.SpanContextFromContext(want).TraceID()
	if !traceID.IsValid() {
		return
	}
	out := make([]observability.RunnerSpan, 0, min(len(batch.Spans), config.MaxRunnerSpansPerBatch))
	for _, sp := range batch.Spans {
		if len(out) == config.MaxRunnerSpansPerBatch {
			break
		}
		if span, ok := runnerSpan(sp, traceID, r); ok {
			out = append(out, span)
		}
	}
	if len(out) == 0 {
		return
	}
	if s.Telemetry != nil {
		s.export(ctx, r, out)
	}
	if s.Traces != nil {
		s.keep(ctx, r, out)
	}
}

// export sends a run's spans on without the stream waiting (#234): the runner's Done follows its last spans, and the
// run reaches waiting and opens its approval whatever the collector does. With every slot taken the batch is dropped.
func (s *Server) export(ctx context.Context, r *run, spans []observability.RunnerSpan) {
	runID, choice := r.ID, r.tracing
	s.exportingOnce.Do(func() { s.exporting = make(chan struct{}, config.MaxRunnerSpanExports) })
	select {
	case s.exporting <- struct{}{}:
	default:
		if s.Log != nil {
			s.Log.Debug("dropped a runner's spans while the collector was slow", "run", runID, "spans", len(spans))
		}
		return
	}
	// The stream ends right after Done, so the export outlives it, bounded on its own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.RunnerSpanExportTimeout)
	go func() {
		defer func() { cancel(); <-s.exporting }()
		s.Telemetry.Spans(ctx, runID, choice, spans)
	}()
}

// runnerSpan is one span as the control plane trusts it, or false for one of another trace or without an id.
func runnerSpan(sp *runnerpb.RunnerSpan, traceID trace.TraceID, r *run) (observability.RunnerSpan, bool) {
	var got trace.TraceID
	var id, parent trace.SpanID
	if len(sp.TraceId) != len(got) || len(sp.SpanId) != len(id) || (len(sp.ParentSpanId) != 0 && len(sp.ParentSpanId) != len(parent)) {
		return observability.RunnerSpan{}, false
	}
	copy(got[:], sp.TraceId)
	copy(id[:], sp.SpanId)
	copy(parent[:], sp.ParentSpanId)
	if got != traceID || !id.IsValid() || sp.Start == nil || sp.End == nil {
		return observability.RunnerSpan{}, false
	}
	attrs := make(map[string]string, min(len(sp.Attributes), config.MaxSpanAttributes)+2)
	for k, v := range sp.Attributes {
		if !strings.HasPrefix(k, "rotor.") || runnerRotorKeys[k] {
			attrs[k] = v
		}
	}
	attrs["rotor.run"], attrs["rotor.workflow"] = r.ID.String(), r.WorkflowID.String()
	events := make([]observability.SpanEvent, 0, min(len(sp.Events), config.MaxSpanEvents))
	for _, e := range sp.Events[max(0, len(sp.Events)-config.MaxSpanEvents):] {
		events = append(events, observability.SpanEvent{Name: e.Name, Time: e.Time.AsTime(), Attributes: e.Attributes})
	}
	return observability.Hold(observability.RunnerSpan{TraceID: got, SpanID: id, ParentID: parent, Name: sp.Name,
		Start: sp.Start.AsTime(), End: sp.End.AsTime(), Attributes: attrs, Failed: sp.Error, Message: sp.StatusMessage,
		Events: events}), true
}

// mintFailure names why a run's token was not minted, as rotor_token_mint_failures_total counts it.
func mintFailure(err error) string {
	switch {
	case errors.Is(err, connections.ErrNotConnected):
		return "not_connected"
	case errors.Is(err, connections.ErrReconnect):
		return "reconnect"
	case errors.Is(err, connections.ErrNeedsRegistration):
		return "needs_registration"
	case errors.Is(err, connections.ErrScopeNotConsented):
		return "scope_not_consented"
	case errors.Is(err, connections.ErrUnreachable):
		return "unreachable"
	case errors.Is(err, connections.ErrRunNotLive):
		return "run_not_live"
	case errors.Is(err, connections.ErrSwitchedOff):
		return "switched_off"
	}
	return "other"
}
