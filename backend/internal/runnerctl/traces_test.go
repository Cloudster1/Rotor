package runnerctl

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// storeCall is one batch handed to the trace store.
type storeCall struct {
	ctx     context.Context
	runID   uuid.UUID
	dataKey string
	spans   []observability.RunnerSpan
}

// stuckStore takes each batch and answers only once the test lets it, as a database under load may not.
type stuckStore struct {
	calls   chan storeCall
	release chan struct{}
}

func (s *stuckStore) Keep(ctx context.Context, runID uuid.UUID, dataKey string, _ observability.RunTracing,
	spans []observability.RunnerSpan) error {
	s.calls <- storeCall{ctx: ctx, runID: runID, dataKey: dataKey, spans: spans}
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return ctx.Err()
}

func (s *stuckStore) KeepContent(context.Context) bool { return false }

func spanBatch(t *testing.T, trace string) *runnerpb.SpanBatch {
	t.Helper()
	now := timestamppb.New(time.Now())
	return &runnerpb.SpanBatch{Spans: []*runnerpb.RunnerSpan{
		{TraceId: hexBytes(t, trace), SpanId: hexBytes(t, "00f067aa0ba902b7"), Name: "node review", Start: now, End: now}}}
}

// Every checked batch is kept in Rotor beside its export (#242), with the run's key to seal its text.
func TestARunsSpansAreKeptBesideTheirExport(t *testing.T) {
	const trace = "0af7651916cd43dd8448eb211c80319c"
	exported := &keptSpans{sent: make(chan struct{}, 1)}
	store := &stuckStore{calls: make(chan storeCall, 1), release: make(chan struct{})}
	close(store.release)
	s := &Server{Telemetry: exported, Traces: store}
	r := &run{ID: uuid.New(), WorkflowID: uuid.New(), dataKey: "vault:v1:key", Traceparent: "00-" + trace + "-b7ad6b7169203331-01"}
	s.spans(context.Background(), r, spanBatch(t, trace))

	if len(exported.kept(t, true)) != 1 {
		t.Error("the batch was not exported")
	}
	select {
	case call := <-store.calls:
		if call.runID != r.ID || call.dataKey != r.dataKey || len(call.spans) != 1 || call.spans[0].Attributes["rotor.run"] != r.ID.String() {
			t.Errorf("the store got run %s, key %q and %v", call.runID, call.dataKey, call.spans)
		}
	case <-time.After(handOffWait):
		t.Fatal("the batch was never stored")
	}

	// Without an exporter the store still keeps the run's spans.
	alone := &stuckStore{calls: make(chan storeCall, 1), release: make(chan struct{})}
	close(alone.release)
	(&Server{Traces: alone}).spans(context.Background(), r, spanBatch(t, trace))
	select {
	case <-alone.calls:
	case <-time.After(handOffWait):
		t.Fatal("without an exporter the batch was never stored")
	}
}

// Storing spans never holds up a run, as #234 made sure for the export: the stream hands the batch on, the store has
// slots of its own apart from the export's, and a batch beyond them is dropped.
func TestAStoreThatDoesNotAnswerNeverHoldsUpARunsStreamNorItsExport(t *testing.T) {
	const trace = "0af7651916cd43dd8448eb211c80319c"
	store := &stuckStore{calls: make(chan storeCall, config.MaxTraceStoreWrites+1), release: make(chan struct{})}
	defer close(store.release)
	exported := &keptSpans{sent: make(chan struct{}, config.MaxTraceStoreWrites+1)}
	s := &Server{Telemetry: exported, Traces: store}
	r := &run{ID: uuid.New(), Traceparent: "00-" + trace + "-b7ad6b7169203331-01"}
	batch := spanBatch(t, trace)

	stream, end := context.WithCancel(context.Background())
	handled := make(chan struct{})
	go func() {
		for range config.MaxTraceStoreWrites + 1 {
			s.spans(stream, r, batch)
		}
		close(handled)
	}()
	select {
	case <-handled:
	case <-time.After(handOffWait):
		t.Fatal("the stream waited for the store, and so would the run's Done behind the spans")
	}
	end()

	for range config.MaxTraceStoreWrites {
		select {
		case call := <-store.calls:
			deadline, bounded := call.ctx.Deadline()
			if !bounded || time.Until(deadline) > config.TraceStoreTimeout {
				t.Errorf("storing may take until %v, want at most %s", deadline, config.TraceStoreTimeout)
			}
			if call.ctx.Err() != nil {
				t.Error("the stream's end cut short storing the spans sent right before Done")
			}
		case <-time.After(handOffWait):
			t.Fatal("a batch within the store's slots was never stored")
		}
	}
	select {
	case <-store.calls:
		t.Error("a batch beyond the store's slots was stored instead of dropped")
	default:
	}
	// The export's slots are its own: a stuck store costs it nothing.
	for range min(config.MaxRunnerSpanExports, config.MaxTraceStoreWrites) {
		exported.kept(t, true)
	}
}
