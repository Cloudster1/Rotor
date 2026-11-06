//go:build integration

package test

import (
	"context"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/metadata"
)

// envOTLPGRPC is where the api takes OTLP over gRPC on the stack's network (#262).
const envOTLPGRPC = "ROTOR_TEST_OTLP_GRPC_ADDR"

// exportTimeout bounds one export, so a refusal the exporter would retry fails the test instead of hanging it.
const exportTimeout = 10 * time.Second

func otlpGRPCAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv(envOTLPGRPC)
	if addr == "" {
		t.Skipf("%s is not set; run the suite with make test-integration", envOTLPGRPC)
	}
	return addr
}

// stockExporter is OpenTelemetry's own OTLP/gRPC exporter as an app configures it, gzip on and retries off, sending
// authorization when it is not empty.
func stockExporter(t *testing.T, authorization string) *otlptrace.Exporter {
	t.Helper()
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(otlpGRPCAddr(t)), otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithCompressor("gzip"), otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}),
		otlptracegrpc.WithTimeout(exportTimeout)}
	if authorization != "" {
		opts = append(opts, otlptracegrpc.WithHeaders(map[string]string{"authorization": authorization}))
	}
	exp, err := otlptracegrpc.New(context.Background(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exp.Shutdown(context.Background()) })
	return exp
}

// grpcSpan is a span as an app's SDK finished it, under the app's service, with the links it names.
type grpcSpan struct {
	traceID, spanID, parent string
	name                    string
	attrs                   map[string]string
	links                   []sdktrace.Link
}

func spanContextOf(t *testing.T, traceID, spanID string) trace.SpanContext {
	t.Helper()
	tid, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := trace.SpanIDFromHex(spanID)
	if err != nil {
		t.Fatal(err)
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
}

// linkTo is a link to a span, with attributes as an app sets them.
func linkTo(t *testing.T, traceID, spanID string, attrs map[string]string) sdktrace.Link {
	t.Helper()
	link := sdktrace.Link{SpanContext: spanContextOf(t, traceID, spanID)}
	for k, v := range attrs {
		link.Attributes = append(link.Attributes, attribute.String(k, v))
	}
	return link
}

// exportSpans sends spans through exp as an app's batch processor would, and answers what the exporter said.
func exportSpans(t *testing.T, exp *otlptrace.Exporter, spans ...grpcSpan) error {
	t.Helper()
	stubs := make(tracetest.SpanStubs, 0, len(spans))
	for _, s := range spans {
		stub := tracetest.SpanStub{Name: s.name, SpanContext: spanContextOf(t, s.traceID, s.spanID), StartTime: sentAt,
			EndTime: sentAt.Add(time.Millisecond), SpanKind: trace.SpanKindInternal, Links: s.links,
			Resource: sdkresource.NewSchemaless(attribute.String("service.name", appService))}
		if s.parent != "" {
			stub.Parent = spanContextOf(t, s.traceID, s.parent)
		}
		for k, v := range s.attrs {
			stub.Attributes = append(stub.Attributes, attribute.String(k, v))
		}
		stubs = append(stubs, stub)
	}
	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	defer cancel()
	return exp.ExportSpans(ctx, stubs.Snapshots())
}

// grpcSpanOf is a fresh span of traceID, named name.
func grpcSpanOf(traceID, name string, attrs map[string]string) grpcSpan {
	s := appSpan(traceID, name, attrs)
	return grpcSpan{traceID: s.traceID, spanID: s.spanID, name: name, attrs: attrs}
}

// otlpConn is a plain gRPC connection to the service, for what the stock exporter does not show: codes and details.
func otlpConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(otlpGRPCAddr(t), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// withBearer is a call's context carrying a token as an exporter's headers carry it.
func withBearer(t *testing.T, token string) context.Context {
	return metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+token)
}

// rawBytes sends a message as the bytes it is given, which no exporter would, to send one that does not parse.
type rawBytes struct{}

func (rawBytes) Marshal(v any) (mem.BufferSlice, error) {
	return mem.BufferSlice{mem.SliceBuffer(v.([]byte))}, nil
}

func (rawBytes) Unmarshal(mem.BufferSlice, any) error { return nil }

func (rawBytes) Name() string { return "proto" }

func grpcRawCodec() grpc.CallOption { return grpc.ForceCodecV2(rawBytes{}) }
