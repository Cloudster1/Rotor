package runs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Event is one row of a run's events: its number, what happened, at which node and what was said about it.
type Event struct {
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Node    string          `json:"node"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}

// Stream is where Follow sends a run's events, and a heartbeat when there are none for a while.
type Stream interface {
	Event(e Event) error
	Heartbeat() error
}

// Events answers the run's events after the one numbered after, at most config.EventBatch, as the caller sees them.
func (s *Service) Events(ctx context.Context, runID uuid.UUID, after int64) ([]Event, error) {
	out := []Event{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT seq, type, node, payload, at FROM run_event WHERE run_id = $1 AND seq > $2
			ORDER BY seq LIMIT $3`, runID, after, config.EventBatch)
		if err != nil {
			return err
		}
		events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
			var e Event
			err := row.Scan(&e.Seq, &e.Type, &e.Node, &e.Payload, &e.At)
			return e, err
		})
		out = append(out, events...)
		return err
	})
	return out, err
}

// Follow sends a run's events after the one numbered after, then each new one as it is stored, until the run has
// ended and nothing is left to send or ctx ends. A signal from any replica wakes it; it also reads on its own.
func (s *Service) Follow(ctx context.Context, runID uuid.UUID, after int64, to Stream) error {
	if _, err := s.Get(ctx, runID); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Listening starts before the first read, so an event stored between the two is never missed.
	var signals <-chan []byte
	if s.signals != nil {
		signals = s.signals.Subscribe(ctx, runnerctl.EventsChannel(runID))
	}
	poll := time.NewTicker(config.EventPoll)
	defer poll.Stop()
	beat := time.NewTicker(config.EventHeartbeat)
	defer beat.Stop()
	for {
		sent, ended, err := s.sendNew(ctx, runID, &after, to)
		if err != nil || ended {
			return err
		}
		if sent {
			beat.Reset(config.EventHeartbeat)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-signals:
		case <-poll.C:
		case <-beat.C:
			if err := to.Heartbeat(); err != nil {
				return err
			}
		}
	}
}

// sendNew sends every event after *after, moving it on, and says whether the run has ended with nothing left.
func (s *Service) sendNew(ctx context.Context, runID uuid.UUID, after *int64, to Stream) (sent, ended bool, err error) {
	for {
		// The state is read before the events, so a run that ended has its last event among them.
		r, err := s.Get(ctx, runID)
		if err != nil {
			return sent, false, err
		}
		events, err := s.Events(ctx, runID, *after)
		if err != nil {
			return sent, false, err
		}
		for _, e := range events {
			if err := to.Event(e); err != nil {
				return sent, false, err
			}
			*after, sent = e.Seq, true
		}
		if len(events) < config.EventBatch {
			return sent, runnerctl.Ended(r.Status), nil
		}
	}
}

// Graph is the graph of the run's pinned version, for its page; the caller must be allowed to see the run.
func (s *Service) Graph(ctx context.Context, r Run) (spec.Graph, error) {
	var source string
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT spec_yaml FROM workflow_version WHERE workflow_id = $1 AND number = $2`,
			r.WorkflowID, r.Version).Scan(&source)
	})
	if err != nil {
		return spec.Graph{}, err
	}
	parsed, _ := spec.Parse([]byte(source))
	if parsed == nil {
		return spec.Graph{}, errors.New("the run's version no longer reads")
	}
	return parsed.Graph(), nil
}
