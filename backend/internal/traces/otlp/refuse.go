package otlp

import (
	"fmt"
	"time"
)

// SayTracesAlone answers an export of metrics or logs, which Rotor does not take (#262): it has no place to keep or
// show them, and the operator's collector takes them already.
const SayTracesAlone = "Rotor takes traces alone over OTLP. Send metrics and logs to your own OpenTelemetry collector, " +
	"or set OTEL_METRICS_EXPORTER=none and OTEL_LOGS_EXPORTER=none."

// Refusal is a request the endpoint does not take, in words either transport answers: the HTTP status, which gives
// the gRPC code, and the sentence.
type Refusal struct {
	Status  int
	Message string
}

func (r *Refusal) Error() string { return fmt.Sprintf("%d: %s", r.Status, r.Message) }

// Busy is a sender over its requests or its spans a minute, and how long until its window ends; each transport says
// it in its own words, since only HTTP has Retry-After.
type Busy struct {
	Wait  time.Duration
	Spans bool
}

func (b *Busy) Error() string { return fmt.Sprintf("over the rate for %s", b.Wait) }
