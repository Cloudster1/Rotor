package observability

import (
	"slices"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// RunTracing is what a run's workflow chose of its trace (#259): text or none, which destinations, and the share of
// runs sent there. Its zero value sends nowhere, so a choice is always read, as RunTracingColumns reads it.
type RunTracing struct {
	// NoText keeps no text of the run, in Rotor or at a destination, whatever the installation says.
	NoText bool
	// Chosen limits the run's trace to Destinations; otherwise every enabled destination gets it.
	Chosen       bool
	Destinations []uuid.UUID
	// SampleRatio is the share of the workflow's traces sent to destinations, decided by the trace id.
	SampleRatio float64
}

// AllTracing is a workflow that chose nothing: text as the installation says, every destination, every run.
var AllTracing = RunTracing{SampleRatio: 1}

// RunTracingColumns reads a RunTracing from workflow_tracing joined as t, its defaults where the workflow has no row.
const RunTracingColumns = `coalesce(t.content = 'none', false), t.destinations IS NOT NULL, coalesce(t.destinations, '{}'),
	coalesce(t.sample_ratio, 1)`

// SendsTo says whether the run's trace of id goes to a destination: one chosen, in the share the ratio keeps.
func (c RunTracing) SendsTo(id trace.TraceID, destination uuid.UUID) bool {
	if c.Chosen && !slices.Contains(c.Destinations, destination) {
		return false
	}
	// As the collector's ratio decides, so a trace goes whole or not at all whichever replica sends a batch of it.
	return sdktrace.TraceIDRatioBased(c.SampleRatio).ShouldSample(sdktrace.SamplingParameters{TraceID: id}).Decision ==
		sdktrace.RecordAndSample
}
