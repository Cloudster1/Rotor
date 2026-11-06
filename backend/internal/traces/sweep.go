package traces

import (
	"context"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// The sweep's statements: trace_due() of migration 01500 says a trace has taken no span for the retention, and the
// policies let rotor_admin delete no other span or trace.
const (
	sweepSpans = `DELETE FROM trace_span sp USING (
			SELECT s.trace, s.span_id FROM trace t JOIN trace_span s ON s.trace = t.id
			WHERE t.expired_at IS NULL AND trace_due(t.last_span_at) LIMIT $1) due
		WHERE sp.trace = due.trace AND sp.span_id = due.span_id`
	// A run's trace keeps its row and counts, so the API can say it expired; an owned trace goes whole.
	expireRunTraces = `UPDATE trace t SET expired_at = now()
		WHERE t.run_id IS NOT NULL AND t.expired_at IS NULL AND trace_due(t.last_span_at)
		  AND NOT EXISTS (SELECT 1 FROM trace_span s WHERE s.trace = t.id)`
	removeOwnedTraces = `DELETE FROM trace t
		WHERE t.owner_id IS NOT NULL AND trace_due(t.last_span_at) AND NOT EXISTS (SELECT 1 FROM trace_span s WHERE s.trace = t.id)`
)

// Sweep deletes the spans of traces that took none for the retention, at most once every config.TraceSweepEvery
// unless the last sweep stopped at its bound with more to do. One worker sweeps at a time; the others skip.
func (s *Service) Sweep(ctx context.Context) error {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	if !s.swept.IsZero() && s.now().Sub(s.swept) < config.TraceSweepEvery {
		return nil
	}
	s.swept = s.now()
	for range config.TraceSweepMaxBatches {
		n, err := s.sweepBatch(ctx)
		if err != nil {
			return err
		}
		if n < config.TraceSweepBatch {
			return nil
		}
	}
	// More is due than one sweep deletes; the next pass goes on.
	s.swept = time.Time{}
	return nil
}

// sweepBatch deletes one batch of spans past the retention, then ends the traces it emptied; it answers how many
// spans it deleted.
func (s *Service) sweepBatch(ctx context.Context) (int, error) {
	var spans, traces int64
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, int64(config.TraceSweepLockKey)).Scan(&locked); err != nil || !locked {
			return err
		}
		tag, err := tx.Exec(ctx, sweepSpans, config.TraceSweepBatch)
		if err != nil {
			return err
		}
		spans = tag.RowsAffected()
		for _, statement := range []string{expireRunTraces, removeOwnedTraces} {
			tag, err := tx.Exec(ctx, statement)
			if err != nil {
				return err
			}
			traces += tag.RowsAffected()
		}
		return nil
	})
	if err == nil && spans+traces > 0 && s.log != nil {
		s.log.Info("deleted spans past the trace retention", "spans", spans, "traces", traces)
	}
	return int(spans), err
}
