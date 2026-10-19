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
	// LangSmithEndpoint is LangSmith's OpenTelemetry endpoint, which runs' spans go to once an administrator allows it.
	LangSmithEndpoint string
}

// Telemetry (#37); docs/api-contract-m7-telemetry.md describes it.
const (
	// DefaultSampleRatio keeps every trace; a busy installation lowers it.
	DefaultSampleRatio = 1.0
	// DefaultAPIMetricsAddr is where the api serves /metrics, on the stack's network alone.
	DefaultAPIMetricsAddr = ":9465"
	// DefaultLangSmithEndpoint is LangSmith's own OpenTelemetry endpoint.
	DefaultLangSmithEndpoint = "https://api.smith.langchain.com/otel/v1/traces"
	// MaxRunnerSpansPerBatch bounds the spans one message of a runner carries; more are dropped.
	MaxRunnerSpansPerBatch = 256
	// MaxRunnerSpanAttributes bounds the attributes one runner span keeps.
	MaxRunnerSpanAttributes = 32
	// MaxSpanAttrChars bounds one attribute's text, as a database statement is cut in its span.
	MaxSpanAttrChars = 512
	// MaxSpanNameChars bounds a runner span's name.
	MaxSpanNameChars = 128
	// MaxLangSmithKeyChars bounds the key an administrator gives.
	MaxLangSmithKeyChars = 512
	// MaxLangSmithProjectChars bounds the project runs are filed under in LangSmith.
	MaxLangSmithProjectChars = 128
	// DefaultLangSmithProject is the project when an administrator names none.
	DefaultLangSmithProject = "rotor"
	// MetricsCollectTimeout bounds a gauge read from the database while Prometheus scrapes.
	MetricsCollectTimeout = 5 * time.Second
	// LangSmithSettingTTL is how long an api replica trusts what it read of the LangSmith setting; one changed on
	// another replica takes at most this long to reach this one.
	LangSmithSettingTTL = 30 * time.Second
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
		OTLPEndpoint:      l.str("ROTOR_OTLP_ENDPOINT", ""),
		SampleRatio:       ratio,
		APIMetricsAddr:    l.orEmpty("ROTOR_API_METRICS_ADDR", DefaultAPIMetricsAddr),
		LangSmithEndpoint: l.str("ROTOR_LANGSMITH_ENDPOINT", DefaultLangSmithEndpoint),
	}
}
