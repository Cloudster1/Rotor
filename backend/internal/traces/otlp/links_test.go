package otlp

import (
	"net/http"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc/codes"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// A span's links are read with their attributes, a link without whole ids costs itself alone, and the first
// MaxSpanLinks are kept (#262).
func TestASpansLinksAreReadAndBounded(t *testing.T) {
	req := oneSpan(t)
	sp := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	sp.Links = []*tracepb.Span_Link{
		{TraceId: []byte{1, 2}, SpanId: mustHex(t, parentHex)},
		{TraceId: make([]byte, 16), SpanId: mustHex(t, parentHex)},
		{TraceId: mustHex(t, traceHex), SpanId: mustHex(t, parentHex), TraceState: "vendor=x",
			Attributes: []*commonpb.KeyValue{kv("messaging.batch.index", str("3")), kv("rotor.run", str("forged"))}},
	}
	for range config.MaxSpanLinks + 2 {
		sp.Links = append(sp.Links, &tracepb.Span_Link{TraceId: mustHex(t, traceHex), SpanId: mustHex(t, spanHex)})
	}
	spans, rejected := Spans(req)
	if rejected.Total() != 0 || len(spans) != 1 {
		t.Fatalf("took %d spans, rejected %q", len(spans), rejected.Sentence())
	}
	links := spans[0].Links
	if len(links) != config.MaxSpanLinks {
		t.Fatalf("kept %d links, want %d", len(links), config.MaxSpanLinks)
	}
	first := links[0]
	if first.TraceID.String() != traceHex || first.SpanID.String() != parentHex || len(first.Attributes) != 1 ||
		first.Attributes["messaging.batch.index"] != "3" {
		t.Errorf("the first whole link = %s %s %v", first.TraceID, first.SpanID, first.Attributes)
	}
}

// Both transports answer a refusal with the code its HTTP status maps to.
func TestEveryRefusalHasItsGRPCCode(t *testing.T) {
	for httpStatus, want := range map[int]codes.Code{
		http.StatusUnauthorized: codes.Unauthenticated, http.StatusForbidden: codes.PermissionDenied,
		http.StatusTooManyRequests: codes.ResourceExhausted, http.StatusRequestEntityTooLarge: codes.ResourceExhausted,
		http.StatusBadRequest: codes.InvalidArgument, http.StatusServiceUnavailable: codes.Unavailable,
		http.StatusNotImplemented: codes.Unimplemented, http.StatusTeapot: codes.Unknown,
	} {
		if got := CodeOf(httpStatus); got != want {
			t.Errorf("%d is %s, want %s", httpStatus, got, want)
		}
	}
}
