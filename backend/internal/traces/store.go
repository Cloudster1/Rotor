package traces

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/observability"
)

// ErrTraceTaken is a run whose trace id another run's trace holds, as two runs started under one caller's trace
// before #242 may share; its spans are not kept.
var ErrTraceTaken = errors.New("another run's trace has this run's trace id")

// insertSpans writes a batch in its order, so the cap's trigger takes the earliest spans first.
const insertSpans = `INSERT INTO trace_span (trace, span_id, parent_span_id, name, kind, service, source, started_at, ended_at,
		status, status_message, attributes, events, sealed_content)
	SELECT $1, s.span_id, s.parent, s.name, s.kind, $2, $3, s.started_at, s.ended_at, s.status, s.message,
		s.attributes::jsonb, s.events::jsonb, s.sealed
	FROM unnest($4::text[], $5::text[], $6::text[], $7::text[], $8::timestamptz[], $9::timestamptz[], $10::text[],
		$11::text[], $12::text[], $13::text[], $14::bytea[])
		WITH ORDINALITY AS s(span_id, parent, name, kind, started_at, ended_at, status, message, attributes, events, sealed, n)
	ORDER BY s.n`

// Keep stores a run's checked spans in its trace, made with the first of them, beside their export. Their text is
// sealed with the run's data key while Rotor keeps text, and dropped otherwise.
func (s *Service) Keep(ctx context.Context, runID uuid.UUID, dataKey string, spans []observability.RunnerSpan) error {
	if len(spans) == 0 {
		return nil
	}
	var box *envelope.Box
	if s.Keys != nil && s.KeepContent(ctx) {
		var err error
		if box, err = s.Keys.For(ctx, runID, dataKey); err != nil {
			return err
		}
	}
	traceID := spans[0].TraceID.String()
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO trace (trace_id, run_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, traceID, runID); err != nil {
			return err
		}
		var trace uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM trace WHERE run_id = $1 AND trace_id = $2`, runID, traceID).Scan(&trace)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTraceTaken
		}
		if err != nil {
			return err
		}
		rows, err := rowsOf(trace, spans, box)
		if err != nil || len(rows.ids) == 0 {
			return err
		}
		_, err = tx.Exec(ctx, insertSpans, trace, observability.RunnerService, sourceRunner, rows.ids, rows.parents, rows.names,
			rows.kinds, rows.starts, rows.ends, rows.statuses, rows.messages, rows.attributes, rows.events, rows.sealed)
		return err
	})
}
