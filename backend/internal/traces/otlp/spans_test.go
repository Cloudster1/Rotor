package otlp

import (
	"strings"
	"testing"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	statuspb "google.golang.org/genproto/googleapis/rpc/status"

	"github.com/cloudster1/rotor/backend/internal/config"
)

func str(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

func kv(key string, v *commonpb.AnyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: v}
}

// A sender's span is read as Rotor keeps it: values as strings, the resource's service, and none of the rotor.* keys
// only Rotor writes.
func TestASpanIsReadWithItsServiceAndWithoutRotorsKeys(t *testing.T) {
	req := oneSpan(t)
	sp := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	sp.Attributes = append(sp.Attributes,
		kv("rotor.run", str("someone else's run")), kv("rotor", str("x")), kv("rotorish", str("kept")),
		kv("llm.token_count.total", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 42}}),
		kv("retry", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}),
		kv("llm.invocation_parameters.temperature", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 0.5}}),
		kv("tags", &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{
			Values: []*commonpb.AnyValue{str("a"), str("b")}}}}))
	sp.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "the model refused"}
	sp.Events = []*tracepb.Span_Event{{Name: "exception", TimeUnixNano: endNanos, Attributes: []*commonpb.KeyValue{
		kv("exception.type", str("ValueError")), kv("rotor.node", str("forged"))}}}
	sp.Links = []*tracepb.Span_Link{{TraceId: mustHex(t, traceHex), SpanId: mustHex(t, parentHex)}}

	spans, rejected := Spans(req)
	if rejected.Total() != 0 || len(spans) != 1 {
		t.Fatalf("took %d spans, rejected %q", len(spans), rejected.Sentence())
	}
	got := spans[0]
	if got.Service != "my-app" || got.TraceID.String() != traceHex || got.SpanID.String() != spanHex || got.ParentID.String() != parentHex {
		t.Errorf("read %s %s %s of %q", got.TraceID, got.SpanID, got.ParentID, got.Service)
	}
	if got.Start.UnixNano() != startNanos || got.End.UnixNano() != endNanos || !got.Failed || got.Message != "the model refused" {
		t.Errorf("read %v to %v, failed %v %q", got.Start, got.End, got.Failed, got.Message)
	}
	want := map[string]string{"openinference.span.kind": "LLM", "rotorish": "kept", "llm.token_count.total": "42", "retry": "true",
		"llm.invocation_parameters.temperature": "0.5", "tags": `["a","b"]`}
	if len(got.Attributes) != len(want) {
		t.Errorf("attributes = %v, want %v", got.Attributes, want)
	}
	for k, v := range want {
		if got.Attributes[k] != v {
			t.Errorf("%s = %q, want %q", k, got.Attributes[k], v)
		}
	}
	if len(got.Events) != 1 || got.Events[0].Attributes["exception.type"] != "ValueError" || len(got.Events[0].Attributes) != 1 {
		t.Errorf("events = %v", got.Events)
	}
}

func TestAResourceWithoutAServiceIsTheUnknownService(t *testing.T) {
	req := oneSpan(t)
	req.ResourceSpans[0].Resource = nil
	req.ResourceSpans[0].ScopeSpans[0].Spans[0].Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}
	spans, _ := Spans(req)
	if len(spans) != 1 || spans[0].Service != unknownService || !spans[0].OK {
		t.Errorf("spans = %v", spans)
	}
}

// Each check costs the span that fails it alone, and partial_success names every reason with its count.
func TestSpansThatFailACheckAreRejectedOneByOneWithTheirReason(t *testing.T) {
	good := func() *tracepb.Span {
		return proto.Clone(oneSpan(t).ResourceSpans[0].ScopeSpans[0].Spans[0]).(*tracepb.Span)
	}
	noName, zeroTrace, shortSpan, backwards, twice := good(), good(), good(), good(), good()
	noName.Name, noName.SpanId = " ", mustHex(t, "0000000000000001")
	zeroTrace.TraceId = make([]byte, len(zeroTrace.TraceId))
	shortSpan.SpanId = shortSpan.SpanId[:4]
	backwards.SpanId, backwards.EndTimeUnixNano = mustHex(t, "0000000000000002"), startNanos-1
	req := &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{
		Spans: []*tracepb.Span{good(), noName, zeroTrace, shortSpan, backwards, twice}}}}}}
	spans, rejected := Spans(req)
	if len(spans) != 1 || rejected.Total() != 5 {
		t.Fatalf("took %d, rejected %d", len(spans), rejected.Total())
	}
	want := "5 of the spans were not taken: 1 " + ReasonName + "; 2 " + ReasonIDs + "; 1 " + ReasonOrder + "; 1 " + ReasonTwice + "."
	if got := rejected.Sentence(); got != want {
		t.Errorf("the sentence is %q, want %q", got, want)
	}
}

func TestSpansPastWhatOneRequestCarriesAreRejected(t *testing.T) {
	span := oneSpan(t).ResourceSpans[0].ScopeSpans[0].Spans[0]
	all := make([]*tracepb.Span, config.MaxOTLPSpansPerRequest+3)
	for i := range all {
		s := proto.Clone(span).(*tracepb.Span)
		s.SpanId = []byte{0, 0, 0, 0, byte(i >> 16), byte(i >> 8), byte(i), 1}
		all[i] = s
	}
	req := &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: all}}}}}
	spans, rejected := Spans(req)
	if len(spans) != config.MaxOTLPSpansPerRequest || Count(req) != len(all) ||
		!strings.Contains(rejected.Sentence(), "3 "+ReasonTooMany) {
		t.Errorf("took %d of %d, %q", len(spans), Count(req), rejected.Sentence())
	}
}

// The answers are written in the request's encoding, so the exporter that sent it reads them.
func TestTheAnswersAreInTheRequestsEncoding(t *testing.T) {
	var rejected Rejections
	rejected.Add(ReasonName, 2)
	rejected.Merge(Rejections{})
	raw, err := Answer(Protobuf, rejected)
	if err != nil {
		t.Fatal(err)
	}
	var resp coltracepb.ExportTraceServiceResponse
	if err := proto.Unmarshal(raw, &resp); err != nil || resp.GetPartialSuccess().GetRejectedSpans() != 2 ||
		resp.GetPartialSuccess().GetErrorMessage() != rejected.Sentence() {
		t.Errorf("the protobuf answer reads %v, %v", &resp, err)
	}
	raw, err = Answer(JSON, Rejections{})
	if err != nil || string(raw) != "{}" {
		t.Errorf("a full success in JSON is %q, %v", raw, err)
	}
	raw, err = Status(JSON, 413, "Send smaller batches.")
	var status statuspb.Status
	if err != nil || protojson.Unmarshal(raw, &status) != nil || status.GetMessage() != "Send smaller batches." || status.GetCode() == 0 {
		t.Errorf("a refusal in JSON is %q, %v", raw, err)
	}
	if JSON.MediaType() != MediaJSON || Protobuf.MediaType() != MediaProtobuf {
		t.Error("an answer's media type is not its request's")
	}
}
