package otlpgrpc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// fakeIntake lets in "Bearer good", refuses any other value as the HTTP endpoint would, and is over its rate when told.
type fakeIntake struct {
	busy, busySpans bool
	taken           int
}

type ctxKeySender struct{}

func (f *fakeIntake) OTLPSender(ctx context.Context, authorization string) (context.Context, error) {
	if authorization != "Bearer good" {
		return ctx, &otlp.Refusal{Status: http.StatusUnauthorized, Message: "Send a token."}
	}
	return context.WithValue(ctx, ctxKeySender{}, true), nil
}

func (f *fakeIntake) OTLPAdmit(ctx context.Context) error {
	if ctx.Value(ctxKeySender{}) == nil {
		return &otlp.Refusal{Status: http.StatusInternalServerError, Message: "admitted nobody"}
	}
	if f.busy {
		return &otlp.Busy{Wait: 42 * time.Second}
	}
	return nil
}

func (f *fakeIntake) OTLPTake(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (otlp.Rejections, error) {
	if f.busySpans {
		return otlp.Rejections{}, &otlp.Busy{Wait: time.Second, Spans: true}
	}
	f.taken += otlp.Count(req)
	var r otlp.Rejections
	r.Add(otlp.ReasonName, 1)
	return r, nil
}

// bytesCodec sends bytes as they are, which no exporter would, to send a message that does not parse.
type bytesCodec struct{}

func (bytesCodec) Marshal(v any) (mem.BufferSlice, error) {
	return mem.BufferSlice{mem.SliceBuffer(v.([]byte))}, nil
}

func (bytesCodec) Unmarshal(mem.BufferSlice, any) error { return nil }

func (bytesCodec) Name() string { return "proto" }

// dial serves the service over an in-memory listener and answers a connection to it.
func dial(t *testing.T, intake Intake) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := NewServer(intake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///otlp", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func withToken(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", token)
}

func someSpans(n int) *coltracepb.ExportTraceServiceRequest {
	spans := make([]*tracepb.Span, n)
	for i := range spans {
		spans[i] = &tracepb.Span{Name: "span"}
	}
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}}}}}
}

// A sender with a token is answered partial_success, gzip or not; without one, UNAUTHENTICATED before anything is read.
func TestAnExportIsTakenWithATokenAndRefusedWithout(t *testing.T) {
	intake := &fakeIntake{}
	client := coltracepb.NewTraceServiceClient(dial(t, intake))
	for _, opts := range [][]grpc.CallOption{nil, {grpc.UseCompressor(gzip.Name)}} {
		resp, err := client.Export(withToken("Bearer good"), someSpans(2), opts...)
		if err != nil || resp.GetPartialSuccess().GetRejectedSpans() != 1 || resp.GetPartialSuccess().GetErrorMessage() == "" {
			t.Fatalf("export = %v, %v", resp, err)
		}
	}
	_, err := client.Export(context.Background(), someSpans(1))
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "Send a token." {
		t.Errorf("without a token = %v", err)
	}
	if intake.taken != 4 {
		t.Errorf("took %d spans, want the four sent with the token", intake.taken)
	}
}

// Over a rate is RESOURCE_EXHAUSTED with the wait as RetryInfo, the one sign on which an exporter retries it.
func TestASenderOverItsRateIsToldWhenToSendAgain(t *testing.T) {
	for name, intake := range map[string]*fakeIntake{"requests": {busy: true}, "spans": {busySpans: true}} {
		_, err := coltracepb.NewTraceServiceClient(dial(t, intake)).Export(withToken("Bearer good"), someSpans(1))
		st := status.Convert(err)
		var wait time.Duration
		for _, d := range st.Details() {
			if info, ok := d.(*errdetails.RetryInfo); ok {
				wait = info.GetRetryDelay().AsDuration()
			}
		}
		if st.Code() != codes.ResourceExhausted || wait == 0 {
			t.Errorf("%s: %v, retry after %s", name, err, wait)
		}
	}
}

// A message past the limits is RESOURCE_EXHAUSTED without RetryInfo, and one that does not parse INVALID_ARGUMENT.
func TestAMessageTooLargeOrMalformedIsRefused(t *testing.T) {
	conn := dial(t, &fakeIntake{})
	large := someSpans(1)
	large.ResourceSpans[0].ScopeSpans[0].Spans[0].Name = string(make([]byte, config.MaxOTLPBodyBytes+1))
	_, err := coltracepb.NewTraceServiceClient(conn).Export(withToken("Bearer good"), large)
	if st := status.Convert(err); st.Code() != codes.ResourceExhausted || len(st.Details()) != 0 {
		t.Errorf("past the limit as sent = %v", err)
	}
	// A first field that says five bytes follow, and none do.
	truncated := []byte{0x0a, 0x05}
	err = conn.Invoke(withToken("Bearer good"), coltracepb.TraceService_Export_FullMethodName, truncated, new([]byte),
		grpc.ForceCodecV2(bytesCodec{}))
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("a message that does not parse = %v", err)
	}
}

// Metrics and logs are refused in a sentence that says where they go instead (#262).
func TestMetricsAreRefusedSayingRotorTakesTracesAlone(t *testing.T) {
	_, err := colmetricspb.NewMetricsServiceClient(dial(t, &fakeIntake{})).
		Export(withToken("Bearer good"), &colmetricspb.ExportMetricsServiceRequest{})
	if st := status.Convert(err); st.Code() != codes.Unimplemented || st.Message() != otlp.SayTracesAlone {
		t.Errorf("metrics = %v", err)
	}
}
