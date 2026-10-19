package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// traced gives every request a server span, continuing a traceparent the caller sent. Chi knows the route only once
// it has routed, so the span is named after the pattern when the handler returns, never after the raw path.
func traced(next http.Handler) http.Handler {
	named := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if rc := chi.RouteContext(r.Context()); rc != nil {
			if pattern := rc.RoutePattern(); pattern != "" {
				span := trace.SpanFromContext(r.Context())
				span.SetName(r.Method + " " + pattern)
				span.SetAttributes(semconv.HTTPRoute(pattern))
			}
		}
	})
	return otelhttp.NewHandler(named, "http.request",
		otelhttp.WithSpanNameFormatter(spanName),
		// The probes run every few seconds and say nothing about a request anyone made.
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" && r.URL.Path != "/readyz" }),
	)
}

// spanName is the method and chi's route once routed, or the method alone before: otelhttp asks again at the end.
func spanName(_ string, r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return r.Method + " " + pattern
		}
	}
	return r.Method
}
