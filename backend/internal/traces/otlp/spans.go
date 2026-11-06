package otlp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// serviceKey names the app in a resource; OpenTelemetry's SDKs write unknownService where the app named none.
const (
	serviceKey     = "service.name"
	unknownService = "unknown_service"
)

// Why a span of a request was not taken, as partial_success names it.
var (
	ReasonTooMany = fmt.Sprintf("past the %d spans one request may carry", config.MaxOTLPSpansPerRequest)
	ReasonIDs     = "without a trace id of 16 bytes and a span id of 8, neither all zero"
	ReasonName    = "without a name"
	ReasonOrder   = "ending before they start"
	ReasonTwice   = "whose span id came twice in one request"
)

// Rejections counts the spans of a request that were not taken, by why, in the order each reason first came.
type Rejections struct {
	order  []string
	counts map[string]int
}

// Add counts n spans not taken for reason.
func (r *Rejections) Add(reason string, n int) {
	if n <= 0 {
		return
	}
	if r.counts == nil {
		r.counts = map[string]int{}
	}
	if r.counts[reason] == 0 {
		r.order = append(r.order, reason)
	}
	r.counts[reason] += n
}

// Total is how many spans were not taken.
func (r *Rejections) Total() int {
	n := 0
	for _, c := range r.counts {
		n += c
	}
	return n
}

// Sentence names each reason and its count, as an OTLP client logs it; empty when every span was taken.
func (r *Rejections) Sentence() string {
	total := r.Total()
	if total == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.order))
	for _, reason := range r.order {
		parts = append(parts, fmt.Sprintf("%d %s", r.counts[reason], reason))
	}
	return fmt.Sprintf("%d of the spans were not taken: %s.", total, strings.Join(parts, "; "))
}

// Count is how many spans a request carries, taken or not.
func Count(req *coltracepb.ExportTraceServiceRequest) int {
	n := 0
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			n += len(ss.GetSpans())
		}
	}
	return n
}

// Spans are a request's spans that pass the checks, each with its resource's service.name, its links and without the
// rotor.* keys only Rotor writes; the other resource attributes are dropped. Limits and redaction come after.
func Spans(req *coltracepb.ExportTraceServiceRequest) ([]observability.RunnerSpan, Rejections) {
	var out []observability.RunnerSpan
	var rejected Rejections
	type key struct {
		trace trace.TraceID
		span  trace.SpanID
	}
	seen := map[key]bool{}
	considered := 0
	for _, rs := range req.GetResourceSpans() {
		service := serviceOf(rs.GetResource().GetAttributes())
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				considered++
				if considered > config.MaxOTLPSpansPerRequest {
					rejected.Add(ReasonTooMany, 1)
					continue
				}
				span, reason := spanOf(sp, service)
				if k := (key{span.TraceID, span.SpanID}); reason == "" {
					if seen[k] {
						reason = ReasonTwice
					}
					seen[k] = true
				}
				if reason != "" {
					rejected.Add(reason, 1)
					continue
				}
				out = append(out, span)
			}
		}
	}
	return out, rejected
}

// spanOf is one span as Rotor reads it, or the reason it is not taken.
func spanOf(sp *tracepb.Span, service string) (observability.RunnerSpan, string) {
	var traceID trace.TraceID
	var id, parent trace.SpanID
	if len(sp.GetTraceId()) != len(traceID) || len(sp.GetSpanId()) != len(id) ||
		(len(sp.GetParentSpanId()) != 0 && len(sp.GetParentSpanId()) != len(parent)) {
		return observability.RunnerSpan{}, ReasonIDs
	}
	copy(traceID[:], sp.GetTraceId())
	copy(id[:], sp.GetSpanId())
	copy(parent[:], sp.GetParentSpanId())
	if !traceID.IsValid() || !id.IsValid() {
		return observability.RunnerSpan{}, ReasonIDs
	}
	if strings.TrimSpace(sp.GetName()) == "" {
		return observability.RunnerSpan{}, ReasonName
	}
	start, end := timeOf(sp.GetStartTimeUnixNano()), timeOf(sp.GetEndTimeUnixNano())
	if end.Before(start) {
		return observability.RunnerSpan{}, ReasonOrder
	}
	events := sp.GetEvents()[max(0, len(sp.GetEvents())-config.MaxSpanEvents):]
	out := observability.RunnerSpan{TraceID: traceID, SpanID: id, ParentID: parent, Name: sp.GetName(), Start: start, End: end,
		Attributes: attributes(sp.GetAttributes()), Service: service, Events: make([]observability.SpanEvent, 0, len(events))}
	switch sp.GetStatus().GetCode() {
	case tracepb.Status_STATUS_CODE_ERROR:
		out.Failed, out.Message = true, sp.GetStatus().GetMessage()
	case tracepb.Status_STATUS_CODE_OK:
		out.OK = true
	}
	for _, e := range events {
		out.Events = append(out.Events, observability.SpanEvent{Name: e.GetName(), Time: timeOf(e.GetTimeUnixNano()),
			Attributes: attributes(e.GetAttributes())})
	}
	out.Links = links(sp.GetLinks())
	return out, ""
}

// links are the first config.MaxSpanLinks of a span's links whose ids are whole; one that is not costs itself alone,
// since a link is context and not the span.
func links(in []*tracepb.Span_Link) []observability.SpanLink {
	var out []observability.SpanLink
	for _, l := range in {
		if len(out) == config.MaxSpanLinks {
			break
		}
		var traceID trace.TraceID
		var spanID trace.SpanID
		if len(l.GetTraceId()) != len(traceID) || len(l.GetSpanId()) != len(spanID) {
			continue
		}
		copy(traceID[:], l.GetTraceId())
		copy(spanID[:], l.GetSpanId())
		if traceID.IsValid() && spanID.IsValid() {
			out = append(out, observability.SpanLink{TraceID: traceID, SpanID: spanID, Attributes: attributes(l.GetAttributes())})
		}
	}
	return out
}

// timeOf reads nanoseconds since the epoch, holding a value past what Go's time keeps to the latest it does.
func timeOf(nanos uint64) time.Time {
	return time.Unix(0, int64(min(nanos, math.MaxInt64))).UTC()
}

// serviceOf is the resource's service.name, or the name OpenTelemetry gives an app that set none.
func serviceOf(attrs []*commonpb.KeyValue) string {
	for _, kv := range attrs {
		if kv.GetKey() == serviceKey {
			if s := strings.TrimSpace(kv.GetValue().GetStringValue()); s != "" {
				return s
			}
		}
	}
	return unknownService
}

// attributes are a span's or an event's attributes as strings, without the rotor.* keys only Rotor writes.
func attributes(kvs []*commonpb.KeyValue) map[string]string {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		key := kv.GetKey()
		if key == "" || key == "rotor" || strings.HasPrefix(key, "rotor.") {
			continue
		}
		if v, ok := valueOf(kv.GetValue()); ok {
			out[key] = toString(v)
		}
	}
	return out
}

// valueOf is an attribute's value as Go holds it, nested values included, or false for one without a value.
func valueOf(v *commonpb.AnyValue) (any, bool) {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue, true
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue, true
	case *commonpb.AnyValue_IntValue:
		return x.IntValue, true
	case *commonpb.AnyValue_DoubleValue:
		return x.DoubleValue, true
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(x.BytesValue), true
	case *commonpb.AnyValue_ArrayValue:
		list := make([]any, 0, len(x.ArrayValue.GetValues()))
		for _, item := range x.ArrayValue.GetValues() {
			if value, ok := valueOf(item); ok {
				list = append(list, value)
			}
		}
		return list, true
	case *commonpb.AnyValue_KvlistValue:
		obj := make(map[string]any, len(x.KvlistValue.GetValues()))
		for _, kv := range x.KvlistValue.GetValues() {
			if value, ok := valueOf(kv.GetValue()); ok {
				obj[kv.GetKey()] = value
			}
		}
		return obj, true
	}
	return nil, false
}

// toString writes a value as the runner's attributes are written: text as it is, anything else as JSON.
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// Merge counts what another step did not take beside what this one did not.
func (r *Rejections) Merge(other Rejections) {
	for _, reason := range other.order {
		r.Add(reason, other.counts[reason])
	}
}
