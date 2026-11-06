//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// OpenTelemetry's own gRPC exporter sends with a Rotor access token and with an ingest key, gzip on, and the spans land
// as the sender's, the key named on them; without a token it is refused and nothing is kept (#262).
func TestTheStockGRPCExporterSendsWithATokenOrAnIngestKeyAndNotWithout(t *testing.T) {
	h := newHarness(t)
	h.keepText(t, false)
	sender, adminID := h.addPerson(t, "member"), h.addPerson(t, "admin")
	key := makeKey(t, h, h.browserFor(t, adminID), map[string]any{"name": "grpc " + uuid.NewString()[:8]})

	for name, c := range map[string]struct {
		credential string
		sendsAs    uuid.UUID
		keyID      string
	}{
		"an access token": {h.appToken(t, sender, false), sender, ""},
		"an ingest key":   {key.secret, adminID, key.id},
	} {
		traceID := randomTraceID()
		root := grpcSpanOf(traceID, "LangGraph", map[string]string{"openinference.span.kind": "CHAIN", "rotor.sender": "forged"})
		if err := exportSpans(t, stockExporter(t, "Bearer "+c.credential), root); err != nil {
			t.Fatalf("%s: the export failed: %v", name, err)
		}
		kept, ok := h.keptSpanOf(t, traceID, root.spanID)
		if !ok {
			t.Fatalf("%s: the span was not kept", name)
		}
		if kept.senderID == nil || *kept.senderID != c.sendsAs || kept.source != "otlp" || kept.service != appService ||
			kept.kind != "CHAIN" || kept.attributes["rotor.sender"] != c.sendsAs.String() ||
			kept.attributes["rotor.ingest_key"] != c.keyID {
			t.Errorf("%s: kept %+v", name, kept)
		}
	}

	traceID := randomTraceID()
	stranger := grpcSpanOf(traceID, "LangGraph", nil)
	err := exportSpans(t, stockExporter(t, ""), stranger)
	if err == nil || !strings.Contains(err.Error(), codes.Unauthenticated.String()) || !strings.Contains(err.Error(), "bearer token alone") {
		t.Errorf("without a token = %v", err)
	}
	if _, ok := h.keptSpanOf(t, traceID, stranger.spanID); ok {
		t.Error("a span sent without a token was kept")
	}
	_, err = coltracepb.NewTraceServiceClient(otlpConn(t)).Export(withBearer(t, h.appToken(t, sender, true)), &coltracepb.ExportTraceServiceRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("a grant to read = %v", err)
	}
}

// Over gRPC the endpoint keeps HTTP's limits, partial success and codes, and a sender's rates are one window across
// both transports, answered with RetryInfo.
func TestOTLPOverGRPCIsHeldToTheHTTPEndpointsLimits(t *testing.T) {
	h := newHarness(t)
	sender, other := h.addPerson(t, "member"), h.addPerson(t, "member")
	token := h.appToken(t, sender, false)
	client := coltracepb.NewTraceServiceClient(otlpConn(t))

	traceID := randomTraceID()
	good, nameless := appSpan(traceID, "LangGraph", nil), appSpan(traceID, "", nil)
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(protobufOf(t, good, nameless), req); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Export(withBearer(t, token), req)
	if err != nil || resp.GetPartialSuccess().GetRejectedSpans() != 1 ||
		!strings.Contains(resp.GetPartialSuccess().GetErrorMessage(), otlp.ReasonName) {
		t.Errorf("one span without a name = %v, %v", resp, err)
	}
	if _, ok := h.keptSpanOf(t, traceID, good.spanID); !ok {
		t.Error("the span with a name was not kept")
	}

	large := &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{
		Spans: []*tracepb.Span{{Name: strings.Repeat("x", config.MaxOTLPBodyBytes)}}}}}}}
	_, err = client.Export(withBearer(t, token), large)
	if st := status.Convert(err); st.Code() != codes.ResourceExhausted || len(st.Details()) != 0 || !strings.Contains(st.Message(), "MiB") {
		t.Errorf("past the size as sent = %v", err)
	}
	err = otlpConn(t).Invoke(withBearer(t, token), coltracepb.TraceService_Export_FullMethodName, []byte{0x0a, 0x05}, new([]byte),
		grpcRawCodec())
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("a message that does not parse = %v", err)
	}

	// HTTP spends a sender's requests of the minute, and gRPC is refused the next one.
	empty := otlpCall{token: h.appToken(t, other, false), contentType: mediaJSON, body: []byte(`{}`)}
	for i := range config.OTLPRequestsPerMinute {
		if status, _, answer := sendSpans(t, empty); status != http.StatusOK {
			t.Fatalf("request %d = %d %q", i+1, status, answer)
		}
	}
	_, err = client.Export(withBearer(t, empty.token), &coltracepb.ExportTraceServiceRequest{})
	if wait := retryAfter(err); status.Code(err) != codes.ResourceExhausted || wait <= 0 {
		t.Errorf("a request past the minute's over gRPC = %v, retry after %s", err, wait)
	}
}

// Metrics and logs are refused over both transports, to anyone, in a sentence saying where they go instead.
func TestMetricsAndLogsAreRefusedSayingRotorTakesTracesAlone(t *testing.T) {
	for _, path := range []string{"/otlp/v1/metrics", "/otlp/v1/logs"} {
		for _, media := range []string{mediaJSON, mediaProtobuf} {
			status, header, body := sendSpansTo(t, path, otlpCall{contentType: media, body: []byte("anything")})
			if status != http.StatusNotImplemented || header.Get("Content-Type") != media {
				t.Errorf("%s as %s = %d %q", path, media, status, header.Get("Content-Type"))
			}
			if media == mediaJSON && !strings.Contains(string(body), "takes traces alone") {
				t.Errorf("%s says %q", path, body)
			}
		}
	}
	conn := otlpConn(t)
	_, metricsErr := colmetricspb.NewMetricsServiceClient(conn).Export(t.Context(), &colmetricspb.ExportMetricsServiceRequest{})
	_, logsErr := collogspb.NewLogsServiceClient(conn).Export(t.Context(), &collogspb.ExportLogsServiceRequest{})
	for name, err := range map[string]error{"metrics": metricsErr, "logs": logsErr} {
		if st := status.Convert(err); st.Code() != codes.Unimplemented || st.Message() != otlp.SayTracesAlone {
			t.Errorf("%s over gRPC = %v", name, err)
		}
	}
}

// retryAfter is the wait a refusal's RetryInfo asks for, zero without one.
func retryAfter(err error) time.Duration {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.RetryInfo); ok {
			return info.GetRetryDelay().AsDuration()
		}
	}
	return 0
}
