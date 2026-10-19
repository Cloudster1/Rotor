package triggers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// Firer starts the runs of queued firings, each as its trigger's owner, after checking it as them again.
type Firer struct {
	triggers *Service
	log      *slog.Logger
	id       string
	// Probe tells whether the owner may read an event's subject at its tool (#35); nil lets every event through.
	Probe func(ctx context.Context, t Trigger, inputs map[string]any) (bool, error)
	// Fired counts each firing's end by its outcome; nil counts nothing.
	Fired func(outcome string)
}

// NewFirer returns a firer that starts runs through the triggers' services.
func NewFirer(triggers *Service, log *slog.Logger) *Firer {
	host, _ := os.Hostname()
	suffix := make([]byte, config.WorkerIDBytes)
	_, _ = rand.Read(suffix)
	return &Firer{triggers: triggers, log: log, id: host + "-" + hex.EncodeToString(suffix)}
}

// fire is a queued firing this firer holds the lease of.
type fire struct {
	id, triggerID uuid.UUID
	key           string
	inputs        map[string]any
	createdAt     time.Time
}

// errPassing is a firing that failed for a reason that may pass, such as a tool not answering; it is tried again.
var errPassing = errors.New("a passing failure")

// Fire leases up to config.TriggerBatch queued firings and starts each one's run; one that fails for a passing
// reason is tried again when its lease runs out, until config.TriggerFireRetryFor.
func (f *Firer) Fire(ctx context.Context) error {
	var batch []fire
	err := f.triggers.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `UPDATE trigger_fire SET claimed_by = $1, claimed_until = now() + make_interval(secs => $2),
				attempts = attempts + 1
			FROM (SELECT id FROM trigger_fire WHERE done_at IS NULL AND (claimed_until IS NULL OR claimed_until < now())
				ORDER BY due_at LIMIT $3 FOR UPDATE SKIP LOCKED) open
			WHERE trigger_fire.id = open.id
			RETURNING trigger_fire.id, trigger_fire.trigger_id, trigger_fire.fire_key, trigger_fire.inputs, trigger_fire.created_at`,
			f.id, config.TriggerFireLease.Seconds(), config.TriggerBatch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var fr fire
			var inputs []byte
			if err := rows.Scan(&fr.id, &fr.triggerID, &fr.key, &inputs, &fr.createdAt); err != nil {
				return err
			}
			_ = json.Unmarshal(inputs, &fr.inputs)
			batch = append(batch, fr)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, fr := range batch {
		errs = append(errs, f.one(ctx, fr))
	}
	return errors.Join(errs...)
}

// one checks a firing as its trigger's owner and starts the run, or records why not.
func (f *Firer) one(ctx context.Context, fr fire) error {
	t, err := f.triggerOf(ctx, fr.triggerID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !t.Enabled {
		return f.end(ctx, fr, t, OutcomeSkipped, "the trigger is off", nil)
	}
	owner, active, err := f.owner(ctx, t.OwnerID)
	if err != nil {
		return err
	}
	if !active {
		return f.refuse(ctx, fr, t, ReasonOwnerSwitchedOff, "")
	}
	as := db.WithUser(auth.WithPrincipal(ctx, owner), owner.UserID)
	outcome, reason, runID, err := f.start(as, fr, t)
	switch {
	case errors.Is(err, errPassing):
		if time.Since(fr.createdAt) < config.TriggerFireRetryFor {
			f.log.Warn("a trigger's firing will be tried again", "trigger", t.ID, "reason", reason)
			return nil
		}
		return f.end(ctx, fr, t, OutcomeFailed, reason, nil)
	case err != nil:
		return err
	case outcome == OutcomeRefused:
		return f.refuse(ctx, fr, t, reason, "")
	}
	return f.end(ctx, fr, t, outcome, reason, runID)
}

// start checks the firing as the owner on the context and starts the run of the version it checked.
func (f *Firer) start(ctx context.Context, fr fire, t Trigger) (outcome, reason string, runID *uuid.UUID, err error) {
	checked, err := f.triggers.check(ctx, t.WorkflowID, t.Input())
	if reason, ok := refusal(err); ok {
		return OutcomeRefused, reason, nil, nil
	}
	if err != nil {
		return "", err.Error(), nil, errPassing
	}
	if t.NoOverlap && t.LastRunStatus != nil && !runnerctl.Ended(*t.LastRunStatus) {
		return OutcomeSkipped, "the last run has not ended", nil, nil
	}
	if f.Probe != nil && t.Kind == KindEvent {
		readable, err := f.Probe(ctx, t, fr.inputs)
		if err != nil {
			return "", err.Error(), nil, errPassing
		}
		if !readable {
			return OutcomeSkipped, "the owner cannot read the event's subject", nil, nil
		}
	}
	r, _, err := f.triggers.runs.Start(ctx, runs.StartRequest{WorkflowID: t.WorkflowID, Version: checked.version.Number,
		Inputs: runInputs(t.Kind, t.Inputs, fr.inputs), IdempotencyKey: "trigger:" + t.ID.String() + ":" + fr.key, TriggerID: t.ID})
	if reason, ok := refusal(err); ok {
		return OutcomeRefused, reason, nil, nil
	}
	if err != nil {
		return "", err.Error(), nil, errPassing
	}
	return OutcomeStarted, "", &r.ID, nil
}

// refusal names the reason a check's error turns the trigger off for, if it is one.
func refusal(err error) (string, bool) {
	var refused *Refused
	var needs *runs.NeedsConnections
	var inputs *runs.InputsRefused
	switch {
	case err == nil:
		return "", false
	case errors.As(err, &refused):
		for _, p := range refused.Problems {
			if p.Code == ReasonUnattendedWrite {
				return ReasonUnattendedWrite, true
			}
		}
		return ReasonInputsRefused, true
	case errors.As(err, &needs):
		return ReasonConnectionLost, true
	case errors.As(err, &inputs):
		return ReasonInputsRefused, true
	case errors.Is(err, runs.ErrArchived), errors.Is(err, workflows.ErrArchived):
		return ReasonWorkflowArchived, true
	case errors.Is(err, workflows.ErrNotFound), errors.Is(err, runs.ErrNotFound):
		return ReasonWorkflowHidden, true
	}
	return "", false
}

// refuse ends a firing as refused and turns its trigger off for the reason.
func (f *Firer) refuse(ctx context.Context, fr fire, t Trigger, reason, detail string) error {
	if err := f.triggers.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return turnOff(ctx, tx, t.ID, reason, detail)
	}); err != nil {
		return err
	}
	return f.end(ctx, fr, t, OutcomeRefused, reason, nil)
}

// end records how a firing ended, on it and as its trigger's last outcome.
func (f *Firer) end(ctx context.Context, fr fire, t Trigger, outcome, reason string, runID *uuid.UUID) error {
	err := f.triggers.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE trigger_fire SET outcome = $2, reason = $3, run_id = $4, done_at = now(),
				claimed_by = NULL, claimed_until = NULL WHERE id = $1`, fr.id, outcome, reason, runID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE trigger SET last_fired_at = now(), last_outcome = $2, last_reason = $3,
				last_run_id = COALESCE($4, last_run_id) WHERE id = $1`, t.ID, outcome, reason, runID)
		return err
	})
	if err == nil && f.Fired != nil {
		f.Fired(outcome)
	}
	return err
}

// triggerOf reads a trigger as the worker sees it, with its last run's status.
func (f *Firer) triggerOf(ctx context.Context, id uuid.UUID) (Trigger, error) {
	var t Trigger
	err := f.triggers.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		t, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM trigger t WHERE t.id = $1`, id))
		return err
	})
	return t, err
}

// owner is the trigger's owner as a request of theirs would see them, without a session.
func (f *Firer) owner(ctx context.Context, id uuid.UUID) (*auth.Principal, bool, error) {
	p := &auth.Principal{UserID: id}
	var active bool
	err := f.triggers.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT email, name, role, groups, is_active FROM app_user WHERE id = $1`, id).
			Scan(&p.Email, &p.Name, &p.Role, &p.Groups, &active)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, active, err
}
