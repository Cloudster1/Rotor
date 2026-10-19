package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// tracerName is what every span of Rotor's control plane is attributed to.
const tracerName = "github.com/cloudster1/rotor/backend"

// Tracing is a process's tracer and where its spans go; nil when no collector is set, which makes every span free.
type Tracing struct {
	provider *sdktrace.TracerProvider
	exporter sdktrace.SpanExporter
	env      string
}

// SetupTracing installs the W3C propagator always, so a traceparent is read and passed on whatever else is on, and a
// provider only when spans have somewhere to go: the collector, or exporter when a test hands one in.
func SetupTracing(ctx context.Context, service, env string, cfg config.Telemetry, log *slog.Logger, exporter sdktrace.SpanExporter) (*Tracing, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		// A collector that is down is not this process's problem to shout about.
		log.Debug("telemetry", "error", err)
	}))
	if exporter == nil && cfg.OTLPEndpoint != "" {
		var err error
		if exporter, err = newExporter(ctx, cfg.OTLPEndpoint, nil); err != nil {
			return nil, err
		}
	}
	if exporter == nil {
		return nil, nil
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(serviceResource(service, env)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	return &Tracing{provider: provider, exporter: exporter, env: env}, nil
}

func serviceResource(service, env string) *resource.Resource {
	return resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service), semconv.DeploymentEnvironmentNameKey.String(env))
}

// Flush sends what the tracer holds without ending it.
func (t *Tracing) Flush(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.ForceFlush(ctx)
}

// Shutdown flushes what the tracer still holds and ends it.
func (t *Tracing) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// Tracer is the control plane's tracer, whichever provider is installed.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// StartSpan begins a span named name under the one on ctx; end closes it and marks err on it. Without a provider
// both are no-ops.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(err error)) {
	ctx, span := Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// Inject writes the span on ctx as a traceparent, for a run to carry from its start to its sandbox.
func Inject(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// Extract reads a traceparent back onto a context, so later spans of a run belong to its trace.
func Extract(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

// TraceIDFrom is the trace id on ctx as hex, or "" when there is none.
func TraceIDFrom(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
