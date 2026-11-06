package observability

import (
	"crypto/rand"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

// traceSample is how many trace ids a ratio is judged over, enough that a quarter cannot land on none or all.
const traceSample = 400

func TestAWorkflowsChoiceSendsWhereItSays(t *testing.T) {
	chosen, other := uuid.New(), uuid.New()
	var id trace.TraceID
	_, _ = rand.Read(id[:])
	if !AllTracing.SendsTo(id, other) {
		t.Error("a workflow that chose nothing does not send to every destination")
	}
	only := RunTracing{Chosen: true, Destinations: []uuid.UUID{chosen}, SampleRatio: 1}
	if !only.SendsTo(id, chosen) || only.SendsTo(id, other) {
		t.Error("a workflow's chosen destinations are not the only ones it sends to")
	}
	if (RunTracing{Chosen: true, SampleRatio: 1}).SendsTo(id, chosen) {
		t.Error("a workflow that chose no destination sends to one")
	}
	if (RunTracing{SampleRatio: 0}).SendsTo(id, chosen) {
		t.Error("a ratio of 0 sends a trace")
	}

	quarter := RunTracing{SampleRatio: 0.25}
	sent := 0
	for range traceSample {
		_, _ = rand.Read(id[:])
		if quarter.SendsTo(id, chosen) {
			sent++
			// The same trace is decided alike every time, so its batches go whole or not at all.
			if !quarter.SendsTo(id, other) {
				t.Fatal("one trace is sent to one destination and not to another")
			}
		}
	}
	if sent == 0 || sent == traceSample {
		t.Errorf("a quarter sent %d of %d traces", sent, traceSample)
	}
}
