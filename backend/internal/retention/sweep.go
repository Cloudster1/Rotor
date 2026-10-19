package retention

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// due finds the ended runs past their retention, oldest first: the installation's days, or the workflow's own when
// its current version sets fewer. A run is deleted only once torn down, so its tokens are revoked first.
const due = `SELECT r.id FROM run r
	JOIN workflow w ON w.id = r.workflow_id
	LEFT JOIN workflow_version v ON v.workflow_id = w.id AND v.number = w.current_version
	CROSS JOIN retention_setting s
	WHERE r.finished_at IS NOT NULL AND r.torn_down_at IS NOT NULL
	  AND r.finished_at < now() - make_interval(days => LEAST(s.run_data_days,
	      CASE WHEN v.spec->>'retention' ~ '^[1-9][0-9]{0,3}d$' THEN rtrim(v.spec->>'retention', 'd')::int END))
	ORDER BY r.finished_at
	LIMIT $1
	FOR UPDATE OF r SKIP LOCKED`

// deleted is what one batch removed of one workflow's runs, for its audit entry.
type deleted struct {
	name           string
	runs           int
	oldest, newest time.Time
}

// Sweep deletes the runs past their retention, at most once every config.RetentionSweepEvery, unless the last sweep
// stopped at its bound with more to do. One worker sweeps at a time; the others skip.
func (s *Service) Sweep(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.last.IsZero() && s.now().Sub(s.last) < config.RetentionSweepEvery {
		return nil
	}
	s.last = s.now()
	for range config.RetentionMaxBatches {
		n, err := s.batch(ctx)
		if err != nil {
			return err
		}
		if n < config.RetentionBatch {
			return nil
		}
	}
	// More is due than one sweep deletes; the next pass goes on.
	s.last = time.Time{}
	return nil
}

// batch deletes one batch of runs past their retention, with all they hold by cascade, and records one audit entry
// per workflow; it answers how many runs it deleted.
func (s *Service) batch(ctx context.Context) (int, error) {
	count := 0
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, int64(config.RetentionLockKey)).Scan(&locked); err != nil || !locked {
			return err
		}
		rows, err := tx.Query(ctx, `WITH due AS (`+due+`)
			DELETE FROM run USING due WHERE run.id = due.id
			RETURNING run.id, run.workflow_id, run.workflow_name, run.finished_at`, config.RetentionBatch)
		if err != nil {
			return err
		}
		byWorkflow := map[uuid.UUID]*deleted{}
		var gone []uuid.UUID
		for rows.Next() {
			var id, workflow uuid.UUID
			var name string
			var finished time.Time
			if err := rows.Scan(&id, &workflow, &name, &finished); err != nil {
				rows.Close()
				return err
			}
			gone = append(gone, id)
			d, ok := byWorkflow[workflow]
			if !ok {
				d = &deleted{name: name, oldest: finished, newest: finished}
				byWorkflow[workflow] = d
			}
			d.runs++
			d.oldest, d.newest = minTime(d.oldest, finished), maxTime(d.newest, finished)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(gone) == 0 {
			return err
		}
		count = len(gone)
		// Triggers name the last run they started and firings the run each started; neither points at a deleted one.
		if _, err := tx.Exec(ctx, `UPDATE trigger SET last_run_id = NULL WHERE last_run_id = ANY($1)`, gone); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE trigger_fire SET run_id = NULL WHERE run_id = ANY($1)`, gone); err != nil {
			return err
		}
		for workflow, d := range byWorkflow {
			if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRunDataDeleted, TargetType: "workflow", TargetID: workflow.String(),
				Data: map[string]any{"workflowName": d.name, "runs": d.runs, "oldestFinishedAt": d.oldest, "newestFinishedAt": d.newest}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && count > 0 {
		s.log.Info("deleted runs past their retention", "runs", count)
	}
	return count, err
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
