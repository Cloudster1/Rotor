package config

import (
	"fmt"
	"strconv"
	"time"
)

// Telemetry is where a process sends its traces and serves its metrics (#37).
type Telemetry struct {
	// OTLPEndpoint is the collector's whole URL for traces, /v1/traces included; empty keeps no traces.
	OTLPEndpoint string
	// SampleRatio is the share of new traces kept; a trace begun elsewhere keeps its own decision.
	SampleRatio float64
	// APIMetricsAddr is where the api serves /metrics, beside its public listener; empty serves none.
	APIMetricsAddr string
}

// Telemetry (#37); docs/api-contract-m7-telemetry.md describes it.
const (
	// DefaultSampleRatio keeps every trace; a busy installation lowers it.
	DefaultSampleRatio = 1.0
	// DefaultAPIMetricsAddr is where the api serves /metrics, on the stack's network alone.
	DefaultAPIMetricsAddr = ":9465"
	// RunnerSpanExportTimeout bounds sending one runner batch on, which the exporter would otherwise retry for a
	// minute against a collector that does not answer (#234).
	RunnerSpanExportTimeout = 10 * time.Second
	// MaxRunnerSpanExports bounds the runner batches being sent on at once; a batch beyond them is dropped.
	MaxRunnerSpanExports = 32
	// MetricsCollectTimeout bounds a gauge read from the database while Prometheus scrapes.
	MetricsCollectTimeout = 5 * time.Second
)

// RunDurationBuckets are the seconds rotor_run_duration_seconds counts runs into: a run takes seconds to hours.
var RunDurationBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200}

func loadTelemetry(l *loader) Telemetry {
	ratio := DefaultSampleRatio
	if v := l.str("ROTOR_TRACE_SAMPLE_RATIO", ""); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			l.problem(fmt.Sprintf("ROTOR_TRACE_SAMPLE_RATIO is %q; set it to a share from 0 to 1, such as 0.1.", v))
		} else {
			ratio = parsed
		}
	}
	return Telemetry{
		OTLPEndpoint:   l.str("ROTOR_OTLP_ENDPOINT", ""),
		SampleRatio:    ratio,
		APIMetricsAddr: l.orEmpty("ROTOR_API_METRICS_ADDR", DefaultAPIMetricsAddr),
	}
}
