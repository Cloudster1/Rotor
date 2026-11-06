package runnerctl

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// TraceStore keeps a run's checked spans in Postgres for whoever reads the run (#242); *traces.Service is the real one.
type TraceStore interface {
	// Keep stores a run's spans by ctx's deadline, their text sealed with the run's data key while Rotor keeps text
	// and the run's workflow did not choose none.
	Keep(ctx context.Context, runID uuid.UUID, dataKey string, choice observability.RunTracing, spans []observability.RunnerSpan) error
	// KeepContent says whether Rotor keeps spans' text now, so a run started now records it.
	KeepContent(ctx context.Context) bool
}

// keep stores a run's spans as export sends them: on a pool of its own, so a slow destination never costs the store
// nor the store a destination, and without the stream waiting. With every slot taken the batch is dropped.
func (s *Server) keep(ctx context.Context, r *run, spans []observability.RunnerSpan) {
	s.storingOnce.Do(func() { s.storing = make(chan struct{}, config.MaxTraceStoreWrites) })
	select {
	case s.storing <- struct{}{}:
	default:
		if s.Log != nil {
			s.Log.Debug("dropped a runner's spans while the trace store was busy", "run", r.ID, "spans", len(spans))
		}
		return
	}
	// The stream ends right after Done, so storing outlives it, bounded on its own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.TraceStoreTimeout)
	runID, dataKey, choice := r.ID, r.dataKey, r.tracing
	go func() {
		defer func() { cancel(); <-s.storing }()
		if err := s.Traces.Keep(ctx, runID, dataKey, choice, spans); err != nil && s.Log != nil {
			s.Log.Warn("could not keep a run's spans", "run", runID, "spans", len(spans), "error", err)
		}
	}()
}
