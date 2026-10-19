package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// liveRun is what reconciling needs of the run a sandbox is labelled with.
type liveRun struct {
	status    string
	sandboxID string
	leased    bool
}

// reconcile holds the sandboxes against the runs, in one listing: a sandbox no live run owns is destroyed, and a
// live run whose sandbox exited or vanished is marked so that expire replaces it in the same pass.
func (w *Worker) reconcile(ctx context.Context) error {
	found, err := w.sandboxes.List(ctx, map[string]string{sandbox.LabelStack: w.launch.Stack})
	if err != nil {
		return fmt.Errorf("reconcile: list the sandboxes: %w", err)
	}
	ids, present := make([]uuid.UUID, 0, len(found)), make([]string, 0, len(found))
	for _, s := range found {
		if id, err := uuid.Parse(s.Labels[sandbox.LabelRun]); err == nil {
			ids = append(ids, id)
		}
		if s.Status.State != sandbox.StateExited {
			present = append(present, string(s.ID))
		}
	}
	runs := map[uuid.UUID]liveRun{}
	err = w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, status, COALESCE(sandbox_id, ''), COALESCE(claimed_until > now(), false)
			FROM run WHERE id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var r liveRun
			if err := rows.Scan(&id, &r.status, &r.sandboxID, &r.leased); err != nil {
				return err
			}
			runs[id] = r
		}
		return rows.Err()
	})
	if err != nil {
		return fmt.Errorf("reconcile: read the runs: %w", err)
	}
	var errs []error
	kept := 0
	for _, s := range found {
		id, _ := uuid.Parse(s.Labels[sandbox.LabelRun])
		r, ok := runs[id]
		live := ok && (r.status == runnerctl.StatusStarting || r.status == runnerctl.StatusRunning)
		// A sandbox being started is not named by its run until the starting worker lets go.
		if live && (r.sandboxID == string(s.ID) || r.leased) {
			if s.Status.State != sandbox.StateExited {
				kept++
			}
			continue
		}
		switch err := w.sandboxes.Destroy(ctx, s.ID); {
		case err == nil:
			w.Metrics.reap(reapReason(ok, r.status))
		case !errors.Is(err, sandbox.ErrNotFound):
			errs = append(errs, err)
		}
	}
	w.Metrics.sawLive(kept)
	errs = append(errs, w.markGone(ctx, present))
	return errors.Join(errs...)
}

// reapReason says why reconcile destroyed a sandbox: its run ended or waits, or no live run owns it any more.
func reapReason(known bool, status string) string {
	switch {
	case known && runnerctl.Ended(status):
		return ReapEnded
	case known && status == runnerctl.StatusWaiting:
		return ReapWaiting
	}
	return ReapOrphan
}

// markGone makes every live run whose sandbox is not among those present due for replacement now.
func (w *Worker) markGone(ctx context.Context, present []string) error {
	return w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE run SET
				runner_lost_at = CASE WHEN status = 'running' THEN now() - make_interval(secs => $2) ELSE runner_lost_at END,
				sandbox_started_at = CASE WHEN status = 'starting' THEN now() - make_interval(secs => $3) ELSE sandbox_started_at END
			WHERE status IN ('starting', 'running') AND sandbox_id IS NOT NULL AND NOT (sandbox_id = ANY($1))
			  AND (claimed_until IS NULL OR claimed_until < now())`,
			present, config.LostGrace.Seconds(), config.StartTimeout.Seconds())
		return err
	})
}

// expire acts on the runs whose time ran out: a sandbox that never said Hello, a runner that stopped answering, a
// run past its deadline, the sandbox and tokens of a run that now waits, an approval nobody decided, a long wait.
func (w *Worker) expire(ctx context.Context) error {
	return errors.Join(
		w.each(ctx, "unstarted", `r.status = 'starting' AND r.sandbox_id IS NOT NULL
			AND r.sandbox_started_at <= now() - make_interval(secs => $3)`, func(ctx context.Context, c claimed) error {
			return w.dropSandbox(ctx, c, ReapUnstarted)
		}, config.StartTimeout.Seconds()),
		w.each(ctx, "lost", `r.status = 'running' AND r.runner_lost_at <= now() - make_interval(secs => $3)`,
			w.replace, config.LostGrace.Seconds()),
		w.each(ctx, "deadline", `r.status = 'running' AND r.deadline_at < now()`, func(ctx context.Context, c claimed) error {
			if err := w.dropSandbox(ctx, c, ReapDeadline); err != nil {
				return err
			}
			return w.end(ctx, c.ID, runnerctl.StatusFailed, ReasonTimeout,
				"The run took longer than its timeout and was ended. Raise limits.timeout if it needs longer.")
		}),
		// A waiting run keeps nothing alive: no sandbox and no token; a revocation that failed is tried again.
		w.each(ctx, "waiting", `r.status = 'waiting' AND (r.sandbox_id IS NOT NULL
			OR EXISTS (SELECT 1 FROM run_token t WHERE t.run_id = r.id AND t.revoked_at IS NULL))`, w.idle),
		// An approval nobody decided in time ends its run; the approval expires with it.
		w.each(ctx, "approval_expired", `r.status = 'waiting' AND EXISTS (SELECT 1 FROM approval a
				WHERE a.run_id = r.id AND a.status = 'pending' AND a.expires_at <= now())`, func(ctx context.Context, c claimed) error {
			return w.end(ctx, c.ID, runnerctl.StatusExpired, runnerctl.ReasonApprovalExpired,
				"Nobody decided the approval in time, so the run expired. Start it again to ask once more.")
		}),
		w.each(ctx, "expired", `r.status = 'waiting' AND NOT EXISTS (SELECT 1 FROM run_event e
				WHERE e.run_id = r.id AND e.at > now() - make_interval(secs => $3))`, func(ctx context.Context, c claimed) error {
			return w.end(ctx, c.ID, runnerctl.StatusExpired, ReasonWaitedTooLong, "Nobody answered the run in time, so it expired.")
		}, config.WaitingTTL.Seconds()),
	)
}

// idle drops a waiting run's sandbox and hands it to Waiting, which revokes its tokens; resuming it starts a fresh
// sandbox from the last checkpoint, which is handed fresh tokens.
func (w *Worker) idle(ctx context.Context, c claimed) error {
	if err := w.dropSandbox(ctx, c, ReapWaiting); err != nil {
		return err
	}
	if w.Waiting == nil {
		return nil
	}
	return w.Waiting(ctx, c.ID)
}

// replace starts a lost runner's run again in a fresh sandbox, which resumes from the last checkpoint; after
// config.MaxRunAttempts sandboxes the run fails.
func (w *Worker) replace(ctx context.Context, c claimed) error {
	if err := w.dropSandbox(ctx, c, ReapLost); err != nil {
		return err
	}
	if c.Attempts >= config.MaxRunAttempts {
		return w.end(ctx, c.ID, runnerctl.StatusFailed, ReasonRunnerLost,
			"The run's sandbox stopped answering several times, so the run was ended. Try again, or ask an administrator to look at the worker's log.")
	}
	payload, _ := json.Marshal(map[string]any{"replaced": true, "attempt": c.Attempts + 1})
	err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE run SET status = 'starting' WHERE id = $1 AND claimed_by = $2 AND status = 'running'`, c.ID, w.id)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return runnerctl.AppendEvent(ctx, tx, c.ID, runnerctl.EventRunnerLost, "", payload)
	})
	if err == nil {
		w.announce(ctx, c.ID)
	}
	return err
}
