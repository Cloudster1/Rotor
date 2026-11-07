package httpapi

// Metrics and logs sent to the OTLP endpoint are refused in a sentence an exporter logs (#262): Rotor has no place to
// keep or show them, and the operator's collector takes them already.

import (
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// The paths an SDK adds for metrics and logs to the endpoint OTEL_EXPORTER_OTLP_ENDPOINT names.
const (
	otlpMetricsPath = APIPrefix + "/otlp/v1/metrics"
	otlpLogsPath    = APIPrefix + "/otlp/v1/logs"
)

// refusedSignal says whether a request is an export of metrics or logs, whose answer is everyone's alike, so no
// credential it carries is looked at and none is refused in place of the sentence.
func refusedSignal(r *http.Request) bool {
	return r.URL.Path == otlpMetricsPath || r.URL.Path == otlpLogsPath
}

func (s *Server) handleRefuseMetrics(w http.ResponseWriter, r *http.Request) { refuseSignal(w, r) }

func (s *Server) handleRefuseLogs(w http.ResponseWriter, r *http.Request) { refuseSignal(w, r) }

// refuseSignal answers 501 in the request's encoding, JSON when it names none OTLP knows; exporters do not retry it.
func refuseSignal(w http.ResponseWriter, r *http.Request) {
	format, err := otlp.FormatOf(r.Header.Get("Content-Type"), "")
	if err != nil {
		format = otlp.JSON
	}
	otlpRefuse(w, r, format, http.StatusNotImplemented, otlp.SayTracesAlone)
}
