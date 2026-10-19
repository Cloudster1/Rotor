package observability

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newExporter sends finished spans over OTLP/HTTP. The endpoint is the whole URL, /v1/traces included, so a collector
// on another path, or LangSmith's endpoint, is one setting away; headers carry what such an endpoint asks for.
func newExporter(ctx context.Context, endpoint string, headers map[string]string) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
	if len(headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}
	return otlptracehttp.New(ctx, opts...)
}

// NewExporter is newExporter for a destination other than the process's own collector, such as LangSmith.
func NewExporter(ctx context.Context, endpoint string, headers map[string]string) (sdktrace.SpanExporter, error) {
	return newExporter(ctx, endpoint, headers)
}
