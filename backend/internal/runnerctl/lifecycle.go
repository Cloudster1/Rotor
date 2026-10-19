package runnerctl

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// Run states, as the run table names them.
const (
	StatusQueued    = "queued"
	StatusStarting  = "starting"
	StatusRunning   = "running"
	StatusWaiting   = "waiting"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
)

// ErrEnded is a run that has already ended, which nothing moves any more.
var ErrEnded = errors.New("the run has ended")

// Ended says whether a run in status has ended for good.
func Ended(status string) bool {
	switch status {
	case StatusSucceeded, StatusFailed, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

// ControlChannel is the pub/sub channel that tells the api holding a run's stream to send what it has not sent.
func ControlChannel(runID uuid.UUID) string { return "runctl:" + runID.String() }

// EventsChannel is the pub/sub channel that tells those following a run, on any api replica, to read its new events.
func EventsChannel(runID uuid.UUID) string { return "runevents:" + runID.String() }

// Publisher tells every api replica that something changed for a run; *cache.Cache is the real one.
type Publisher interface {
	Publish(ctx context.Context, channel string, message []byte)
}

// EventRunnerLost is the event of a runner that stopped answering, or whose sandbox was replaced.
const EventRunnerLost = eventRunnerLost

// AppendEvent stores an event of the control plane's own for the run, numbered after the last.
func AppendEvent(ctx context.Context, tx db.DBTX, runID uuid.UUID, kind, node string, payload []byte) error {
	return appendEvent(ctx, tx, runID, kind, node, payload)
}

// lockStatus reads a run's status and holds the row until the transaction ends.
func lockStatus(ctx context.Context, tx db.DBTX, runID uuid.UUID) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM run WHERE id = $1 FOR UPDATE`, runID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("no such run")
	}
	return status, err
}

// End ends a run the control plane gives up on, such as one whose sandbox never started, with a run.finished event;
// a run that has already ended is left as it is.
func End(ctx context.Context, tx db.DBTX, runID uuid.UUID, status, reason string, detail map[string]any) error {
	current, err := lockStatus(ctx, tx, runID)
	if err != nil || Ended(current) {
		return err
	}
	return endLocked(ctx, tx, runID, status, reason, detail, nil)
}

func endLocked(ctx context.Context, tx db.DBTX, runID uuid.UUID, status, reason string, detail map[string]any, output []byte) error {
	payload := map[string]any{"status": status, "reason": reason, "output": json.RawMessage(validJSON(output))}
	for k, v := range detail {
		payload[k] = v
	}
	encoded, _ := json.Marshal(payload)
	if err := appendEvent(ctx, tx, runID, eventRunFinished, "", encoded); err != nil {
		return err
	}
	if Ended(status) {
		if err := closeApprovals(ctx, tx, runID, status); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRunEnded, TargetType: "run", TargetID: runID.String(),
			Data: map[string]any{"status": status, "reason": reason}}); err != nil {
			return err
		}
	}
	return setStatus(ctx, tx, runID, status, reason, output)
}

// Cancel cancels a run that has not ended and, when a sandbox may be running it, queues a Cancel for its runner;
// the caller publishes ControlChannel once the transaction commits.
func Cancel(ctx context.Context, tx db.DBTX, runID uuid.UUID, by uuid.UUID) error {
	current, err := lockStatus(ctx, tx, runID)
	if err != nil {
		return err
	}
	if Ended(current) {
		return ErrEnded
	}
	if err := endLocked(ctx, tx, runID, StatusCancelled, StatusCancelled, map[string]any{"by": by.String()}, nil); err != nil {
		return err
	}
	if current != StatusStarting && current != StatusRunning {
		return nil
	}
	cancel := &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_Cancel{Cancel: &runnerpb.Cancel{Reason: "cancelled by a person"}}}
	_, err = enqueueTx(ctx, tx, runID, cancel)
	return err
}
