package triggers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// Scheduler queues the firings of due schedules and turns off triggers whose owner's consent ended; the worker
// runs it each pass. Any number may run: one at a time takes the due schedules, under an advisory lock.
type Scheduler struct {
	db  *db.Cluster
	log *slog.Logger
	// Now is the scheduler's clock; tests set it, nil is the real one.
	Now func() time.Time

	mu        sync.Mutex
	lastSweep time.Time
}

// NewScheduler returns a scheduler.
func NewScheduler(cluster *db.Cluster, log *slog.Logger) *Scheduler {
	return &Scheduler{db: cluster, log: log}
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// due is a schedule whose time has come.
type due struct {
	id         uuid.UUID
	cron, zone string
	at         time.Time
}

// Schedule queues one firing for each enabled schedule whose next time has come and moves its next time on from
// now: a time missed while no worker ran fires once, and earlier ones are not made up.
func (s *Scheduler) Schedule(ctx context.Context) error {
	now := s.now()
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var mine bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, int64(config.SchedulerLockKey)).Scan(&mine); err != nil || !mine {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, cron, time_zone, next_run_at FROM trigger
			WHERE enabled AND kind = 'schedule' AND next_run_at <= $1
			ORDER BY next_run_at LIMIT $2 FOR UPDATE SKIP LOCKED`, now, config.TriggerBatch)
		if err != nil {
			return err
		}
		var batch []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.cron, &d.zone, &d.at); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range batch {
			if _, err := Enqueue(ctx, tx, d.id, ScheduleKey(d.at), nil, d.at); err != nil {
				return err
			}
			var next *time.Time
			if schedule, err := ParseSchedule(d.cron, d.zone); err == nil {
				if t := schedule.Next(now); !t.IsZero() {
					next = &t
				}
			} else {
				s.log.Error("a stored schedule no longer reads; it fires no more", "trigger", d.id, "error", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE trigger SET next_run_at = $2 WHERE id = $1`, d.id, next); err != nil {
				return err
			}
		}
		return nil
	})
}

// Sweep turns off, every config.TriggerSweepEvery, the triggers whose owner's consent or account ended, the
// workflow was archived, or the tool removed; each firing checks again, so a sweep only makes it visible sooner.
func (s *Scheduler) Sweep(ctx context.Context) error {
	s.mu.Lock()
	if now := s.now(); now.Sub(s.lastSweep) < config.TriggerSweepEvery {
		s.mu.Unlock()
		return nil
	}
	s.lastSweep = s.now()
	s.mu.Unlock()
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT t.id, CASE
				WHEN NOT u.is_active THEN 'owner_switched_off'
				WHEN w.archived_at IS NOT NULL THEN 'workflow_archived'
				WHEN t.kind = 'event' AND t.tool_id IS NULL THEN 'tool_removed'
				ELSE 'connection_lost' END
			FROM trigger t JOIN app_user u ON u.id = t.owner_id JOIN workflow w ON w.id = t.workflow_id
			LEFT JOIN workflow_version v ON v.workflow_id = w.id AND v.number = w.current_version
			WHERE t.enabled AND (NOT u.is_active OR w.archived_at IS NOT NULL OR (t.kind = 'event' AND t.tool_id IS NULL)
				OR EXISTS (SELECT 1 FROM jsonb_each(COALESCE(v.spec -> 'tools', '{}'::jsonb)) b(name, binding)
					WHERE NOT EXISTS (SELECT 1 FROM connection c JOIN tool tl ON tl.id = c.tool_id
						WHERE c.user_id = t.owner_id AND tl.name = b.binding ->> 'tool' AND c.status = 'connected'
						AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(b.binding -> 'scopes') wanted(scope)
							WHERE NOT (wanted.scope = ANY (c.scopes)
								OR (wanted.scope LIKE '%:read' AND left(wanted.scope, -5) = ANY (c.scopes)))))))
			LIMIT $1 FOR UPDATE OF t SKIP LOCKED`, config.TriggerBatch)
		if err != nil {
			return err
		}
		type off struct {
			id     uuid.UUID
			reason string
		}
		var batch []off
		for rows.Next() {
			var o off
			if err := rows.Scan(&o.id, &o.reason); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, o)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var errs []error
		for _, o := range batch {
			errs = append(errs, turnOff(ctx, tx, o.id, o.reason, ""))
		}
		// Ended firings are kept for a while, so an owner sees what their trigger did lately.
		_, err = tx.Exec(ctx, `DELETE FROM trigger_fire WHERE done_at < now() - make_interval(secs => $1)`, config.TriggerFireRetention.Seconds())
		return errors.Join(append(errs, err)...)
	})
}

// turnOff turns a trigger off for a reason its owner reads, with no actor in the audit log since Rotor did it;
// detail adds to the reason's sentence, such as the node that may write.
func turnOff(ctx context.Context, tx db.DBTX, id uuid.UUID, reason, detail string) error {
	message := Sentence(reason)
	if detail != "" {
		message += " " + detail
	}
	tag, err := tx.Exec(ctx, `UPDATE trigger SET enabled = false, disabled_reason = $2, disabled_detail = $3, disabled_at = now(),
			next_run_at = NULL WHERE id = $1 AND enabled`, id, reason, message)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerDisabled, TargetType: "trigger", TargetID: id.String(),
		Data: map[string]any{"reason": reason}})
}
