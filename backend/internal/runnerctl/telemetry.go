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

// Telemetry is where a run's spans go once the control plane checked them (#37); *langsmith.Forwarder is the real
// one, sending them to the collector and, when an administrator allows it, to LangSmith.
type Telemetry interface {
	// Spans sends a run's spans on.
	Spans(ctx context.Context, runID uuid.UUID, spans []observability.RunnerSpan)
	// Content says whether runs' LLM spans may carry the prompt and the answer: only LangSmith reads them.
	Content(ctx context.Context) bool
}

// ContentPrefix starts the attributes that carry a prompt or an answer, which go to LangSmith alone.
const ContentPrefix = "gen_ai.prompt."

// CompletionPrefix starts the attributes that carry a model's answer.
const CompletionPrefix = "gen_ai.completion."

// IsContent says whether an attribute carries a prompt or an answer.
func IsContent(key string) bool {
	return strings.HasPrefix(key, ContentPrefix) || strings.HasPrefix(key, CompletionPrefix)
}

// spans checks a runner's batch and sends it on: only spans of the run's own trace, at most so many, their names and
// attributes cut short and redacted again, since the sandbox runs code the workflow's author wrote.
func (s *Server) spans(ctx context.Context, r *run, batch *runnerpb.SpanBatch) {
	if s.Telemetry == nil || batch == nil {
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
		if span, ok := runnerSpan(sp, traceID); ok {
			out = append(out, span)
		}
	}
	if len(out) > 0 {
		s.Telemetry.Spans(ctx, r.ID, out)
	}
}

// runnerSpan is one span as the control plane trusts it, or false for one of another trace or without an id.
func runnerSpan(sp *runnerpb.RunnerSpan, traceID trace.TraceID) (observability.RunnerSpan, bool) {
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
	attrs := make(map[string]string, min(len(sp.Attributes), config.MaxRunnerSpanAttributes))
	for k, v := range sp.Attributes {
		if len(attrs) == config.MaxRunnerSpanAttributes {
			break
		}
		attrs[cut(k, config.MaxSpanNameChars)] = cut(string(redactText(v)), config.MaxSpanAttrChars)
	}
	return observability.RunnerSpan{TraceID: got, SpanID: id, ParentID: parent, Name: cut(sp.Name, config.MaxSpanNameChars),
		Start: sp.Start.AsTime(), End: sp.End.AsTime(), Attributes: attrs, Failed: sp.Error,
		Message: cut(string(redactText(sp.StatusMessage)), config.MaxSpanAttrChars)}, true
}

// WithoutContent drops the attributes that carry a prompt or an answer, for a destination other than LangSmith.
func WithoutContent(spans []observability.RunnerSpan) []observability.RunnerSpan {
	out := make([]observability.RunnerSpan, len(spans))
	for i, sp := range spans {
		attrs := make(map[string]string, len(sp.Attributes))
		for k, v := range sp.Attributes {
			if !IsContent(k) {
				attrs[k] = v
			}
		}
		sp.Attributes = attrs
		out[i] = sp
	}
	return out
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
