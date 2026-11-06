package traces

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// ownTrace is the sender's own trace of traceID, made when it has none, and the box to seal text with when withKey;
// the trace's key is made by OpenBao transit when it first seals text, as a run's is when the run starts.
func (s *Service) ownTrace(ctx context.Context, owner uuid.UUID, traceID string, withKey bool) (uuid.UUID, *envelope.Box, error) {
	id, wrapped, found, err := s.readOwn(ctx, owner, traceID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if !found {
		id = uuid.Must(uuid.NewV7())
		var box *envelope.Box
		var key *string
		if withKey {
			made, b, err := s.Keys.New(ctx, id)
			if err != nil {
				return uuid.Nil, nil, err
			}
			key, box = &made, b
		}
		var made bool
		err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `INSERT INTO trace (id, trace_id, owner_id, data_key) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				id, traceID, owner, key)
			made = err == nil && tag.RowsAffected() == 1
			return err
		})
		if err != nil {
			return uuid.Nil, nil, err
		}
		if made {
			return id, box, nil
		}
		// Another request of the sender's made the trace first; its row is the one to join.
		if id, wrapped, _, err = s.readOwn(ctx, owner, traceID); err != nil {
			return uuid.Nil, nil, err
		}
	}
	if !withKey {
		return id, nil, nil
	}
	if wrapped != "" {
		box, err := s.Keys.For(ctx, id, wrapped)
		return id, box, err
	}
	return s.keyOwn(ctx, owner, id)
}

// readOwn reads the sender's own trace of traceID as the sender.
func (s *Service) readOwn(ctx context.Context, owner uuid.UUID, traceID string) (uuid.UUID, string, bool, error) {
	var id uuid.UUID
	var wrapped string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id, coalesce(data_key, '') FROM trace WHERE owner_id = $1 AND trace_id = $2`, owner, traceID).
			Scan(&id, &wrapped)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", false, nil
	}
	return id, wrapped, err == nil, err
}

// keyOwn gives an owned trace made without text its key. rotor_app changes no trace row, so the trigger alone moves
// the counts; the key is set through rotor_admin, once, on a trace the sender's own read just found as theirs.
func (s *Service) keyOwn(ctx context.Context, owner, id uuid.UUID) (uuid.UUID, *envelope.Box, error) {
	made, box, err := s.Keys.New(ctx, id)
	if err != nil {
		return uuid.Nil, nil, err
	}
	var kept string
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `UPDATE trace SET data_key = coalesce(data_key, $1) WHERE id = $2 AND owner_id = $3
			RETURNING data_key`, made, id, owner).Scan(&kept)
	})
	if err != nil {
		return uuid.Nil, nil, err
	}
	if kept != made {
		// Another request keyed the trace first; its key seals this text too.
		box, err = s.Keys.For(ctx, id, kept)
	}
	return id, box, err
}

// forward sends spans that joined a run on as the runner's go, on a pool of its own and without the request waiting;
// with every slot taken the batch is dropped, as a runner's export is.
func (s *Service) forward(ctx context.Context, run seenRun, spans []observability.RunnerSpan) {
	runID := run.id
	if s.Forward == nil {
		return
	}
	s.forwardOnce.Do(func() { s.forwarding = make(chan struct{}, config.MaxRunnerSpanExports) })
	select {
	case s.forwarding <- struct{}{}:
	default:
		if s.log != nil {
			s.log.Debug("dropped a sender's spans of a run while its destinations were slow", "run", runID, "spans", len(spans))
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.RunnerSpanExportTimeout)
	go func() {
		defer func() { cancel(); <-s.forwarding }()
		s.Forward.Spans(ctx, runID, run.tracing, spans)
	}()
}
