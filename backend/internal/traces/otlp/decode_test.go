package otlp

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/config"
)

const (
	traceHex  = "5b8efff798038103d269b633813fc60c"
	spanHex   = "eee19b7ec3c1b174"
	parentHex = "eee19b7ec3c1b173"
	// startNanos needs every one of its digits: a float would round it.
	startNanos = 1544712660000000001
	endNanos   = 1544712661000000001
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func gzipped(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// oneSpan is a request with one span of a LangChain app, as the Python exporter would send it.
func oneSpan(t *testing.T) *coltracepb.ExportTraceServiceRequest {
	t.Helper()
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "my-app"}}}}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId: mustHex(t, traceHex), SpanId: mustHex(t, spanHex), ParentSpanId: mustHex(t, parentHex), Name: "ChatOpenAI",
			StartTimeUnixNano: startNanos, EndTimeUnixNano: endNanos,
			Attributes: []*commonpb.KeyValue{{Key: "openinference.span.kind",
				Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "LLM"}}}},
		}}}},
	}}}
}

func TestTheMediaTypeAndEncodingChooseTheFormatOrAreRefused(t *testing.T) {
	cases := []struct {
		contentType, encoding string
		want                  Format
		err                   error
	}{
		{"application/x-protobuf", "", Protobuf, nil},
		{"application/x-protobuf", "gzip", Protobuf, nil},
		{"application/json; charset=utf-8", "identity", JSON, nil},
		{"application/json", "GZIP", JSON, nil},
		{"text/plain", "", JSON, ErrMediaType},
		{"", "", JSON, ErrMediaType},
		{"application/x-protobuf", "br", Protobuf, ErrEncoding},
	}
	for _, c := range cases {
		got, err := FormatOf(c.contentType, c.encoding)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("FormatOf(%q, %q) = %v, %v; want %v, %v", c.contentType, c.encoding, got, err, c.want, c.err)
		}
	}
}

func TestABodyIsReadWithinItsLimitsSentAndUnpacked(t *testing.T) {
	raw := []byte("spans")
	if got, err := Read(bytes.NewReader(raw), ""); err != nil || !bytes.Equal(got, raw) {
		t.Errorf("a plain body reads %q, %v", got, err)
	}
	if got, err := Read(bytes.NewReader(gzipped(t, raw)), "gzip"); err != nil || !bytes.Equal(got, raw) {
		t.Errorf("a gzip body reads %q, %v", got, err)
	}
	if _, err := Read(bytes.NewReader(make([]byte, config.MaxOTLPBodyBytes+1)), ""); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a body past the limit as sent = %v, want ErrTooLarge", err)
	}
	// Zeros pack small, so a body well within the limit as sent unpacks past it.
	bomb := gzipped(t, make([]byte, config.MaxOTLPDecodedBytes+1))
	if len(bomb) > config.MaxOTLPBodyBytes {
		t.Fatalf("the packed body is %d bytes, past what is sent", len(bomb))
	}
	if _, err := Read(bytes.NewReader(bomb), "gzip"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a body past the limit unpacked = %v, want ErrTooLarge", err)
	}
	if _, err := Read(bytes.NewReader(raw), "gzip"); !errors.Is(err, ErrMalformed) {
		t.Errorf("a body that is not gzip = %v, want ErrMalformed", err)
	}
}

func TestProtobufAndOTLPsJSONDecodeToTheSameRequest(t *testing.T) {
	want := oneSpan(t)
	raw, err := proto.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	fromProto, err := Decode(Protobuf, raw)
	if err != nil || !proto.Equal(fromProto, want) {
		t.Fatalf("the protobuf body decodes to %v, %v", fromProto, err)
	}
	// OTLP's JSON: ids in hex, times as strings or numbers, enums as numbers, a field this Rotor does not know.
	body := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"my-app"}}]},
		"scopeSpans":[{"spans":[{"traceId":"` + traceHex + `","spanId":"` + spanHex + `","parentSpanId":"` + parentHex + `",
		"name":"ChatOpenAI","kind":1,"startTimeUnixNano":"1544712660000000001","endTimeUnixNano":1544712661000000001,
		"attributes":[{"key":"openinference.span.kind","value":{"stringValue":"LLM"}}],"newField":true}]}]}]}`
	fromJSON, err := Decode(JSON, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	fromJSON.ResourceSpans[0].ScopeSpans[0].Spans[0].Kind = tracepb.Span_SPAN_KIND_UNSPECIFIED
	if !proto.Equal(fromJSON, want) {
		t.Errorf("the JSON body decodes to %v, want %v", fromJSON, want)
	}
}

func TestABodyThatIsNoRequestIsRefusedAndABadIDCostsItsSpanAlone(t *testing.T) {
	for name, body := range map[string]string{"not JSON": "spans", "a list": "[]", "a wrong type": `{"resourceSpans":"x"}`} {
		if _, err := Decode(JSON, []byte(body)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s = %v, want ErrMalformed", name, err)
		}
	}
	if _, err := Decode(Protobuf, []byte{0xff, 0xff}); !errors.Is(err, ErrMalformed) {
		t.Errorf("a broken protobuf body = %v, want ErrMalformed", err)
	}
	body := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"not hex","spanId":"` + spanHex + `","name":"a"},
		{"traceId":"` + traceHex + `","spanId":"` + spanHex + `","name":"b"}]}]}]}`
	req, err := Decode(JSON, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	spans, rejected := Spans(req)
	if len(spans) != 1 || spans[0].Name != "b" || !strings.Contains(rejected.Sentence(), "1 "+ReasonIDs) {
		t.Errorf("a span with an id that is not hex: taken %v, %q", spans, rejected.Sentence())
	}
}
