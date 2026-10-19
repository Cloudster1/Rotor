package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// A request continues the trace its caller sent and is named after its route, never its raw path; the probes make
// no spans at all.
func TestARequestJoinsItsCallersTraceUnderItsRoute(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tracing, err := observability.SetupTracing(context.Background(), "rotor-api", "test", config.Telemetry{SampleRatio: 1},
		slog.New(slog.DiscardHandler), exporter)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(traced)
	r.Get("/runs/{runID}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	const trace = "0af7651916cd43dd8448eb211c80319c"
	req := httptest.NewRequest(http.MethodGet, "/runs/0190a5c4-0000-7000-8000-000000000001", nil)
	req.Header.Set("traceparent", "00-"+trace+"-b7ad6b7169203331-01")
	r.ServeHTTP(httptest.NewRecorder(), req)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if err := tracing.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want one for the run and none for the probe", len(spans))
	}
	if spans[0].Name != "GET /runs/{runID}" || spans[0].SpanContext.TraceID().String() != trace {
		t.Errorf("the span is %q in trace %s", spans[0].Name, spans[0].SpanContext.TraceID())
	}
}
