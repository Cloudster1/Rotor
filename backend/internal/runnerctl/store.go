package runnerctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// errGap is a runner message whose seq skips one the control plane never got, which a correct runner never sends.
var errGap = errors.New("a message of the runner's is missing")

// Event types the control plane writes itself.
const (
	eventRunFinished = "run.finished"
	eventRunnerLost  = "runner.lost"
)

// outcomes maps how a run ended in its sandbox to the run's status.
var outcomes = map[runnerpb.RunOutcome]string{
	runnerpb.RunOutcome_RUN_OUTCOME_SUCCEEDED:   "succeeded",
	runnerpb.RunOutcome_RUN_OUTCOME_FAILED:      "failed",
	runnerpb.RunOutcome_RUN_OUTCOME_CANCELLED:   "cancelled",
	runnerpb.RunOutcome_RUN_OUTCOME_INTERRUPTED: "waiting",
}

// store keeps a run's protocol state in Postgres, as rotor_admin: the control plane acts for the run.
type store struct {
	db *db.Cluster
}

// appendEvent stores the next event for the run, numbering it after the last, with every credential cut out.
func appendEvent(ctx context.Context, tx db.DBTX, runID uuid.UUID, kind, node string, payload []byte) error {
	payload = redact(payload)
	if len(payload) > config.MaxEventBytes {
		payload, _ = json.Marshal(map[string]any{"truncated": true, "bytes": len(payload)})
	}
	if !json.Valid(payload) {
		payload = []byte(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO run_event (run_id, seq, type, node, payload)
		VALUES ($1, (SELECT COALESCE(max(seq), 0) + 1 FROM run_event WHERE run_id = $1), $2, $3, $4)`, runID, kind, node, payload)
	return err
}

// received stores a runner message numbered seq once: a seq already stored is ignored, a gap refused. apply runs in
// the same transaction, so a message is acknowledged only when what it changes is stored too.
func (s *store) received(ctx context.Context, runID uuid.UUID, seq uint64, apply func(ctx context.Context, tx db.DBTX) error) (bool, error) {
	fresh := false
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var last uint64
		if err := tx.QueryRow(ctx, `SELECT runner_seq FROM run WHERE id = $1 FOR UPDATE`, runID).Scan(&last); err != nil {
			return err
		}
		switch {
		case seq <= last:
			return nil
		case seq != last+1:
			return fmt.Errorf("%w: got %d after %d", errGap, seq, last)
		}
		fresh = true
		if err := apply(ctx, tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE run SET runner_seq = $2 WHERE id = $1`, runID, seq)
		return err
	})
	return fresh, err
}

// lastReceived is the runner's highest stored seq, which a HelloAck acknowledges.
func (s *store) lastReceived(ctx context.Context, runID uuid.UUID) (uint64, error) {
	var last uint64
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT runner_seq FROM run WHERE id = $1`, runID).Scan(&last)
	})
	return last, err
}

// enqueueTx numbers a control message and keeps it until the runner acknowledges it.
func enqueueTx(ctx context.Context, tx db.DBTX, runID uuid.UUID, msg *runnerpb.ConnectResponse) (*runnerpb.ConnectResponse, error) {
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) + 1 FROM run_control WHERE run_id = $1`, runID).Scan(&msg.Seq); err != nil {
		return nil, err
	}
	encoded, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO run_control (run_id, seq, message) VALUES ($1, $2, $3)`, runID, msg.Seq, encoded)
	return msg, err
}

// supersede starts a fresh sandbox's conversation: what an earlier sandbox did not acknowledge is dropped, since the
// new StartRun carries all of it, and the run starts running with a deadline of its own timeout.
func (s *store) supersede(ctx context.Context, runID uuid.UUID, start *runnerpb.ConnectResponse, deadline time.Time) (*runnerpb.ConnectResponse, error) {
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE run_control SET acked_at = now() WHERE run_id = $1 AND acked_at IS NULL`, runID); err != nil {
			return err
		}
		if _, err := enqueueTx(ctx, tx, runID, start); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE run SET status = 'running', reason = '', runner_lost_at = NULL, deadline_at = $2 WHERE id = $1`, runID, deadline)
		return err
	})
	return start, err
}

// unsent is what the runner has not acknowledged, numbered after the last message this stream sent.
func (s *store) unsent(ctx context.Context, runID uuid.UUID, after uint64) ([]*runnerpb.ConnectResponse, error) {
	pending, _, err := s.unacknowledged(ctx, runID)
	if err != nil {
		return nil, err
	}
	var out []*runnerpb.ConnectResponse
	for _, msg := range pending {
		if msg.Seq > after {
			out = append(out, msg)
		}
	}
	return out, nil
}

// seen records that the run's runner is back, so the worker does not replace its sandbox.
func (s *store) seen(ctx context.Context, runID uuid.UUID) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE run SET runner_lost_at = NULL WHERE id = $1 AND runner_lost_at IS NOT NULL`, runID)
		return err
	})
}

// dropped records that a running run's stream ended without the run ending; the worker replaces its sandbox if the
// runner does not come back within config.LostGrace.
func (s *store) dropped(ctx context.Context, runID uuid.UUID) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE run SET runner_lost_at = now() WHERE id = $1 AND status = 'running' AND runner_lost_at IS NULL`, runID)
		return err
	})
}

// unacknowledged is every control message the runner has not acknowledged, oldest first, and whether any was ever sent.
func (s *store) unacknowledged(ctx context.Context, runID uuid.UUID) ([]*runnerpb.ConnectResponse, bool, error) {
	var out []*runnerpb.ConnectResponse
	sent := false
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM run_control WHERE run_id = $1)`, runID).Scan(&sent); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT message FROM run_control WHERE run_id = $1 AND acked_at IS NULL ORDER BY seq`, runID)
		if err != nil {
			return err
		}
		raw, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
		if err != nil {
			return err
		}
		for _, r := range raw {
			msg := &runnerpb.ConnectResponse{}
			if err := proto.Unmarshal(r, msg); err != nil {
				return err
			}
			out = append(out, msg)
		}
		return nil
	})
	return out, sent, err
}

// acknowledge marks the control messages up to seq as handled by the runner.
func (s *store) acknowledge(ctx context.Context, runID uuid.UUID, seq uint64) error {
	if seq == 0 {
		return nil
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE run_control SET acked_at = now() WHERE run_id = $1 AND seq <= $2 AND acked_at IS NULL`, runID, seq)
		return err
	})
}

// setStatus moves a run along; the run_status_move trigger refuses a move the contract does not allow.
func setStatus(ctx context.Context, tx db.DBTX, runID uuid.UUID, status, reason string, output []byte) error {
	_, err := tx.Exec(ctx, `UPDATE run SET status = $2, reason = $3, output = COALESCE($4::jsonb, output) WHERE id = $1`,
		runID, status, reason, nullJSON(output))
	return err
}

func nullJSON(b []byte) any {
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return string(b)
}

// lost records that a run's runner stopped answering; the worker replaces its sandbox after config.LostGrace.
func (s *store) lost(ctx context.Context, runID uuid.UUID) error {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		payload, _ := json.Marshal(map[string]any{"missedHeartbeats": config.MissedHeartbeats})
		if err := appendEvent(ctx, tx, runID, eventRunnerLost, "", payload); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE run SET runner_lost_at = COALESCE(runner_lost_at, now()) WHERE id = $1 AND status = 'running'`, runID)
		return err
	})
}
