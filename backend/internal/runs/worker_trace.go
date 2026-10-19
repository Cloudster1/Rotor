package runs

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// traced runs act under a span named name in the run's own trace, so starting its sandbox and cleaning up after it
// show beside the request that started it and the runner's spans (#37).
func (w *Worker) traced(name string, act func(ctx context.Context, c claimed) error) func(ctx context.Context, c claimed) error {
	return func(ctx context.Context, c claimed) error {
		ctx = observability.Extract(ctx, w.traceparent(ctx, c.ID))
		ctx, end := observability.StartSpan(ctx, name, attribute.String("rotor.run", c.ID.String()))
		err := act(ctx, c)
		end(err)
		return err
	}
}

// traceparent is the run's trace context, or "" when it has none.
func (w *Worker) traceparent(ctx context.Context, runID uuid.UUID) string {
	var tp string
	_ = w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT traceparent FROM run WHERE id = $1`, runID).Scan(&tp)
	})
	return tp
}

// ran counts how long an ended run took, from its first sandbox's start to its end, by how it ended.
func (w *Worker) ran(ctx context.Context, runID uuid.UUID) {
	if w.Metrics == nil {
		return
	}
	var status string
	var seconds *float64
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT status, EXTRACT(EPOCH FROM finished_at - started_at)::float8 FROM run WHERE id = $1`,
			runID).Scan(&status, &seconds)
	})
	if err == nil && seconds != nil {
		w.Metrics.took.Observe(status, *seconds)
	}
}
