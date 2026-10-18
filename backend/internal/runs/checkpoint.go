// Package runs keeps what the control plane holds for a run beyond its row: its checkpoints (#19), and from M3 its
// lifecycle. A checkpoint is reachable only through the stream of the run it belongs to.
package runs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// listLimit bounds the checkpoints one list answers, which the runner asks for newest first.
const listLimit = 100

// Sealer encrypts and decrypts checkpoint values with a key it never hands out; *vault.Client is the real one.
type Sealer interface {
	Encrypt(ctx context.Context, values [][]byte) ([]string, error)
	Decrypt(ctx context.Context, ciphertexts []string) ([][]byte, error)
}

// Checkpoints answers a runner's checkpoint requests for the run its stream is bound to.
type Checkpoints struct {
	db   *db.Cluster
	seal Sealer
}

// NewCheckpoints returns the store.
func NewCheckpoints(cluster *db.Cluster, sealer Sealer) *Checkpoints {
	return &Checkpoints{db: cluster, seal: sealer}
}

// Handle answers one checkpoint request; a failure is said in the answer's error, never by ending the stream.
func (c *Checkpoints) Handle(ctx context.Context, runID uuid.UUID, req *runnerpb.ConnectRequest) *runnerpb.ConnectResponse {
	switch m := req.Message.(type) {
	case *runnerpb.ConnectRequest_CheckpointGet:
		r := m.CheckpointGet
		found, err := c.get(ctx, runID, r.CheckpointNs, r.CheckpointId)
		answer := &runnerpb.CheckpointGetResponse{RequestId: r.RequestId, Found: found != nil, Checkpoint: found, Error: text(err)}
		return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_CheckpointGet{CheckpointGet: answer}}
	case *runnerpb.ConnectRequest_CheckpointList:
		r := m.CheckpointList
		listed, err := c.list(ctx, runID, r.CheckpointNs, r.BeforeCheckpointId, int(r.Limit))
		answer := &runnerpb.CheckpointListResponse{RequestId: r.RequestId, Checkpoints: listed, Error: text(err)}
		return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_CheckpointList{CheckpointList: answer}}
	case *runnerpb.ConnectRequest_CheckpointPut:
		r := m.CheckpointPut
		answer := &runnerpb.CheckpointPutResponse{RequestId: r.RequestId, Error: text(c.put(ctx, runID, r.Checkpoint))}
		return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_CheckpointPut{CheckpointPut: answer}}
	case *runnerpb.ConnectRequest_CheckpointWrites:
		r := m.CheckpointWrites
		answer := &runnerpb.CheckpointWritesResponse{RequestId: r.RequestId, Error: text(c.writes(ctx, runID, r.CheckpointNs, r.CheckpointId, r.Writes))}
		return &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_CheckpointWrites{CheckpointWrites: answer}}
	}
	return &runnerpb.ConnectResponse{}
}

func text(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// put stores a checkpoint once; storing the same one again, as a runner does after a reconnect, changes nothing.
func (c *Checkpoints) put(ctx context.Context, runID uuid.UUID, cp *runnerpb.Checkpoint) error {
	if cp == nil || cp.Checkpoint == nil || cp.Metadata == nil || cp.CheckpointId == "" {
		return errors.New("a checkpoint needs an id, its value and its metadata")
	}
	if size := len(cp.Checkpoint.Data) + len(cp.Metadata.Data); size > config.MaxCheckpointBytes {
		return fmt.Errorf("the checkpoint is %d bytes, over the %d a run may keep; keep less in the run's state", size, config.MaxCheckpointBytes)
	}
	sealed, err := c.seal.Encrypt(ctx, [][]byte{cp.Checkpoint.Data, cp.Metadata.Data})
	if err != nil {
		return err
	}
	return c.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO run_checkpoint (run_id, checkpoint_ns, checkpoint_id, parent_checkpoint_id,
				checkpoint_type, checkpoint, metadata_type, metadata) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (run_id, checkpoint_ns, checkpoint_id) DO NOTHING`,
			runID, cp.CheckpointNs, cp.CheckpointId, cp.ParentCheckpointId, cp.Checkpoint.Type, sealed[0], cp.Metadata.Type, sealed[1])
		return err
	})
}

// writes stores a task's pending writes: an ordinary write once, a special one (negative idx) over the last, as
// LangGraph's own savers do.
func (c *Checkpoints) writes(ctx context.Context, runID uuid.UUID, ns, checkpointID string, writes []*runnerpb.PendingWrite) error {
	values, size := make([][]byte, len(writes)), 0
	for i, w := range writes {
		if w.Value == nil {
			return errors.New("a pending write needs its value")
		}
		values[i], size = w.Value.Data, size+len(w.Value.Data)
	}
	if size > config.MaxCheckpointBytes {
		return fmt.Errorf("the writes are %d bytes, over the %d a run may keep", size, config.MaxCheckpointBytes)
	}
	sealed, err := c.seal.Encrypt(ctx, values)
	if err != nil {
		return err
	}
	return c.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		for i, w := range writes {
			conflict := "DO NOTHING"
			if w.Idx < 0 {
				conflict = "DO UPDATE SET channel = EXCLUDED.channel, value_type = EXCLUDED.value_type, value = EXCLUDED.value"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO run_checkpoint_write (run_id, checkpoint_ns, checkpoint_id, task_id, idx, channel,
					value_type, value, task_path) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (run_id, checkpoint_ns, checkpoint_id, task_id, idx) `+conflict,
				runID, ns, checkpointID, w.TaskId, w.Idx, w.Channel, w.Value.Type, sealed[i], w.TaskPath); err != nil {
				return err
			}
		}
		return nil
	})
}

// stored is a checkpoint row and its writes as the database holds them, still sealed.
type stored struct {
	cp       *runnerpb.Checkpoint
	sealed   []string
	writeSet []*runnerpb.PendingWrite
}

// get answers one checkpoint, or the newest in its namespace; ids are uuid6 and sort by time.
func (c *Checkpoints) get(ctx context.Context, runID uuid.UUID, ns, checkpointID string) (*runnerpb.Checkpoint, error) {
	query := `SELECT checkpoint_id FROM run_checkpoint WHERE run_id = $1 AND checkpoint_ns = $2 AND ($3 = '' OR checkpoint_id = $3)
		ORDER BY checkpoint_id DESC LIMIT 1`
	found, err := c.load(ctx, runID, ns, query, checkpointID)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// list answers a namespace's checkpoints newest first, those before an id when one is given.
func (c *Checkpoints) list(ctx context.Context, runID uuid.UUID, ns, before string, limit int) ([]*runnerpb.Checkpoint, error) {
	if limit <= 0 || limit > listLimit {
		limit = listLimit
	}
	query := fmt.Sprintf(`SELECT checkpoint_id FROM run_checkpoint WHERE run_id = $1 AND checkpoint_ns = $2
		AND ($3 = '' OR checkpoint_id < $3) ORDER BY checkpoint_id DESC LIMIT %d`, limit)
	return c.load(ctx, runID, ns, query, before)
}

// load reads the checkpoints whose ids query answers, with their writes, and opens them in one decryption.
func (c *Checkpoints) load(ctx context.Context, runID uuid.UUID, ns, query, arg string) ([]*runnerpb.Checkpoint, error) {
	var rows []*stored
	err := c.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		ids, err := tx.Query(ctx, query, runID, ns, arg)
		if err != nil {
			return err
		}
		names, err := pgx.CollectRows(ids, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range names {
			s := &stored{cp: &runnerpb.Checkpoint{CheckpointNs: ns, CheckpointId: id, Checkpoint: &runnerpb.TypedValue{}, Metadata: &runnerpb.TypedValue{}}}
			var checkpoint, metadata string
			if err := tx.QueryRow(ctx, `SELECT parent_checkpoint_id, checkpoint_type, checkpoint, metadata_type, metadata
				FROM run_checkpoint WHERE run_id = $1 AND checkpoint_ns = $2 AND checkpoint_id = $3`, runID, ns, id).Scan(
				&s.cp.ParentCheckpointId, &s.cp.Checkpoint.Type, &checkpoint, &s.cp.Metadata.Type, &metadata); err != nil {
				return err
			}
			s.sealed = []string{checkpoint, metadata}
			writes, err := tx.Query(ctx, `SELECT task_id, idx, channel, value_type, value, task_path FROM run_checkpoint_write
				WHERE run_id = $1 AND checkpoint_ns = $2 AND checkpoint_id = $3 ORDER BY task_path, task_id, idx`, runID, ns, id)
			if err != nil {
				return err
			}
			for writes.Next() {
				w, value := &runnerpb.PendingWrite{Value: &runnerpb.TypedValue{}}, ""
				if err := writes.Scan(&w.TaskId, &w.Idx, &w.Channel, &w.Value.Type, &value, &w.TaskPath); err != nil {
					writes.Close()
					return err
				}
				s.writeSet, s.sealed = append(s.writeSet, w), append(s.sealed, value)
			}
			writes.Close()
			rows = append(rows, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var sealed []string
	for _, s := range rows {
		sealed = append(sealed, s.sealed...)
	}
	opened, err := c.seal.Decrypt(ctx, sealed)
	if err != nil {
		return nil, err
	}
	out, at := make([]*runnerpb.Checkpoint, 0, len(rows)), 0
	for _, s := range rows {
		s.cp.Checkpoint.Data, s.cp.Metadata.Data = opened[at], opened[at+1]
		at += 2
		for _, w := range s.writeSet {
			w.Value.Data = opened[at]
			at++
		}
		s.cp.PendingWrites = s.writeSet
		out = append(out, s.cp)
	}
	return out, nil
}
