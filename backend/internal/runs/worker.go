package runs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
)

// Reasons the worker ends a run with.
const (
	ReasonSandboxStart  = "sandbox_start"
	ReasonRunnerLost    = "runner_lost"
	ReasonTimeout       = "timeout"
	ReasonWaitedTooLong = "waited_too_long"
)

// Launch is how the worker starts a run's sandbox: the runner image, where it dials, and the CA it trusts.
type Launch struct {
	Image  string
	Dial   string
	Stack  string
	Limits sandbox.Limits
	// CA answers the PEM of the CA that signs the runner protocol's certificate.
	CA func(ctx context.Context) (string, error)
	// OpenBao is where the runner unwraps the tokens and keys it is handed (#23); it holds no credential there.
	OpenBao string
}

// Teardown is what else ends with a run, such as revoking its tokens (#23); an error tries again on the next tick.
type Teardown func(ctx context.Context, runID uuid.UUID) error

// Worker carries runs from queued to their end. Any number may run and restart at any time: each run it acts on is
// leased with SKIP LOCKED, so two workers never act on one run, and a dead worker's lease runs out.
type Worker struct {
	db        *db.Cluster
	sandboxes sandbox.Backend
	launch    Launch
	log       *slog.Logger
	id        string
	// Teardown runs once for each run that ended, after its sandbox is gone; nil does nothing more.
	Teardown Teardown
	// Waiting runs for a run that waits, after its sandbox is gone, such as revoking its tokens (#28); an error
	// tries again on the next tick. Nil does nothing more.
	Waiting Teardown
	// Metrics counts the sandboxes alive and reaped; nil counts nothing.
	Metrics *WorkerMetrics
	// Passed is told after every pass, such as for a health check; nil tells nobody.
	Passed func()
	// Events tells those following a run that the worker wrote it an event; nil leaves them to poll.
	Events Publisher
	// Also runs in each pass before queued runs start, such as the triggers' passes (#34); nil does nothing more.
	Also func(ctx context.Context) error
}

// announce tells those following the run that it has a new event, once the event is committed.
func (w *Worker) announce(ctx context.Context, runID uuid.UUID) {
	if w.Events != nil {
		w.Events.Publish(ctx, runnerctl.EventsChannel(runID), nil)
	}
}

// NewWorker returns a worker that starts sandboxes on backend as launch says.
func NewWorker(cluster *db.Cluster, backend sandbox.Backend, launch Launch, log *slog.Logger) *Worker {
	host, _ := os.Hostname()
	suffix := make([]byte, config.WorkerIDBytes)
	_, _ = rand.Read(suffix)
	return &Worker{db: cluster, sandboxes: backend, launch: launch, log: log, id: host + "-" + hex.EncodeToString(suffix)}
}

// Run works every config.WorkerTick until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("run worker started", "worker", w.id)
	ticker := time.NewTicker(config.WorkerTick)
	defer ticker.Stop()
	for {
		if err := w.Once(ctx); err != nil && ctx.Err() == nil {
			w.log.Error("a worker pass failed", "error", err)
		}
		if w.Passed != nil {
			w.Passed()
		}
		select {
		case <-ctx.Done():
			w.log.Info("run worker stopped", "worker", w.id)
			return
		case <-ticker.C:
		}
	}
}

// Once makes one pass: sandboxes first, so a dead one is replaced in the same pass, then what else queues runs,
// then starts, then cleanup.
func (w *Worker) Once(ctx context.Context) error {
	errs := []error{w.reconcile(ctx), w.expire(ctx)}
	if w.Also != nil {
		errs = append(errs, w.Also(ctx))
	}
	return errors.Join(append(errs, w.start(ctx), w.teardown(ctx))...)
}

// claimed is a run this worker holds the lease of.
type claimed struct {
	ID        uuid.UUID
	Status    string
	SandboxID string
	Attempts  int
}

// each leases up to config.WorkerBatch runs that where selects (over run r, its arguments from $3), acts on each and
// lets go; act's error is logged and the run is tried again on a later tick.
func (w *Worker) each(ctx context.Context, step, where string, act func(ctx context.Context, c claimed) error, args ...any) error {
	var batch []claimed
	err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(`UPDATE run SET claimed_by = $1, claimed_until = now() + make_interval(secs => $2)
			FROM (SELECT r.id FROM run r WHERE (r.claimed_until IS NULL OR r.claimed_until < now()) AND (%s)
				ORDER BY r.created_at LIMIT %d FOR UPDATE SKIP LOCKED) due
			WHERE run.id = due.id
			RETURNING run.id, run.status, COALESCE(run.sandbox_id, ''), run.attempts`, where, config.WorkerBatch),
			append([]any{w.id, config.WorkerLease.Seconds()}, args...)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c claimed
			if err := rows.Scan(&c.ID, &c.Status, &c.SandboxID, &c.Attempts); err != nil {
				return err
			}
			batch = append(batch, c)
		}
		return rows.Err()
	})
	if err != nil {
		return fmt.Errorf("%s: claim: %w", step, err)
	}
	for _, c := range batch {
		if err := act(ctx, c); err != nil {
			w.log.Warn("the worker could not act on a run yet", "step", step, "run", c.ID, "error", err)
		}
		w.release(context.WithoutCancel(ctx), c.ID)
	}
	return nil
}

func (w *Worker) release(ctx context.Context, runID uuid.UUID) {
	err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE run SET claimed_by = NULL, claimed_until = NULL WHERE id = $1 AND claimed_by = $2`, runID, w.id)
		return err
	})
	if err != nil {
		w.log.Warn("cannot let go of a run; its lease runs out", "run", runID, "error", err)
	}
}

// start gives each queued run, and each starting one without a sandbox, a sandbox with a fresh bootstrap token.
func (w *Worker) start(ctx context.Context) error {
	return w.each(ctx, "start", `(r.status = 'queued' AND NOT `+atCapacity+`) OR (r.status = 'starting' AND r.sandbox_id IS NULL)`,
		w.traced("run.start_sandbox", w.startOne))
}

func (w *Worker) startOne(ctx context.Context, c claimed) error {
	if c.Attempts >= config.MaxRunAttempts {
		return w.end(ctx, c.ID, runnerctl.StatusFailed, ReasonSandboxStart,
			"The run's sandbox did not start after several attempts. Try again, or ask an administrator to look at the worker's log.")
	}
	err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if c.Status == runnerctl.StatusQueued {
			if err := holdForCapacity(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE run SET status = 'starting', attempts = attempts + 1
			WHERE id = $1 AND claimed_by = $2 AND status = $3`, c.ID, w.id, c.Status)
		if err != nil || tag.RowsAffected() == 0 {
			return errors.Join(err, errors.New("the run moved on before its sandbox started"))
		}
		_, err = tx.Exec(ctx, `DELETE FROM runner_credential WHERE run_id = $1`, c.ID)
		return err
	})
	if errors.Is(err, errHeld) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := runnerctl.Bootstrap(ctx, w.db, c.ID)
	if err != nil {
		return err
	}
	ca, err := w.launch.CA(ctx)
	if err != nil {
		return fmt.Errorf("read the runner CA: %w", err)
	}
	id, err := w.sandboxes.Spawn(ctx, sandbox.Spec{RunID: c.ID, Image: w.launch.Image, Limits: w.launch.Limits,
		Labels:    map[string]string{sandbox.LabelStack: w.launch.Stack},
		Env:       map[string]string{"ROTOR_CONTROL_PLANE_ADDR": w.launch.Dial, "ROTOR_RUNNER_CA": ca, "ROTOR_OPENBAO_ADDR": w.launch.OpenBao},
		SecretEnv: map[string]string{"ROTOR_RUNNER_BOOTSTRAP": token},
		Deadline:  w.deadline(ctx, c.ID)})
	if err != nil {
		return err
	}
	return w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// A run cancelled meanwhile no longer names the sandbox, which the next reconcile destroys.
		if _, err := tx.Exec(ctx, `UPDATE run SET sandbox_id = $3, sandbox_started_at = now()
			WHERE id = $1 AND claimed_by = $2 AND status = 'starting'`, c.ID, w.id, string(id)); err != nil {
			return err
		}
		// What started, never how: the spec carries the bootstrap token.
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSandboxSpawned, TargetType: "run", TargetID: c.ID.String(),
			Data: map[string]any{"sandboxId": string(id), "image": w.launch.Image}})
	})
}

// deadline is how long the run's sandbox may live: its pinned timeout and grace, or zero when the version names none.
func (w *Worker) deadline(ctx context.Context, runID uuid.UUID) time.Duration {
	var timeout *string
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT v.spec->'limits'->>'timeout' FROM run r
			JOIN workflow_version v ON v.workflow_id = r.workflow_id AND v.number = r.version WHERE r.id = $1`, runID).Scan(&timeout)
	})
	if err != nil || timeout == nil {
		return 0
	}
	d, err := time.ParseDuration(*timeout)
	if err != nil {
		return 0
	}
	return d + config.SandboxDeadlineGrace
}

// teardown cleans up after each ended run once: its sandbox, its credentials, then whatever Teardown adds.
func (w *Worker) teardown(ctx context.Context) error {
	return w.each(ctx, "teardown", `r.status IN ('succeeded', 'failed', 'cancelled', 'expired') AND r.torn_down_at IS NULL`,
		w.traced("run.teardown", func(ctx context.Context, c claimed) error {
			if err := w.dropSandbox(ctx, c, ReapEnded); err != nil {
				return err
			}
			if w.Teardown != nil {
				if err := w.Teardown(ctx, c.ID); err != nil {
					return err
				}
			}
			w.ran(ctx, c.ID)
			return w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `UPDATE run SET torn_down_at = now() WHERE id = $1`, c.ID)
				return err
			})
		}))
}

// dropSandbox destroys the run's sandbox, if it has one, counting it under reason, and the credentials it could
// reconnect with.
func (w *Worker) dropSandbox(ctx context.Context, c claimed, reason string) error {
	if c.SandboxID != "" {
		switch err := w.sandboxes.Destroy(ctx, sandbox.ID(c.SandboxID)); {
		case err == nil:
			w.Metrics.reap(reason)
		case !errors.Is(err, sandbox.ErrNotFound):
			return err
		}
	}
	return w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM runner_credential WHERE run_id = $1`, c.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE run SET sandbox_id = NULL, sandbox_started_at = NULL, runner_lost_at = NULL
			WHERE id = $1 AND claimed_by = $2`, c.ID, w.id); err != nil || c.SandboxID == "" {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionSandboxDestroyed, TargetType: "run", TargetID: c.ID.String(),
			Data: map[string]any{"sandboxId": c.SandboxID, "reason": reason}})
	})
}

// end ends a run the worker gives up on, with a message the person reads.
func (w *Worker) end(ctx context.Context, runID uuid.UUID, status, reason, message string) error {
	err := w.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return runnerctl.End(ctx, tx, runID, status, reason, map[string]any{"message": message})
	})
	if err == nil {
		w.announce(ctx, runID)
	}
	return err
}
