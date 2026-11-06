package traces

import (
	"context"
	"errors"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/redact"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// sourceOTLP marks the spans a sender posted to the OTLP endpoint.
const sourceOTLP = "otlp"

// Why the store did not keep a span the endpoint's checks passed, as partial_success names it.
var (
	ReasonCapped  = "past their trace's cap or the spans one sender keeps, or whose span id the trace holds already"
	ReasonRunHeld = "naming a run whose trace id an earlier run holds"
)

// Sender is who posted spans, as the request's token names them.
type Sender struct {
	ID   uuid.UUID
	Name string
	// Key is the ingest key the spans were sent with (#258), uuid.Nil for a token.
	Key uuid.UUID
}

// Forwarder sends a run's spans on as the runner's go, to the collector and the destinations; *tracedest.Forwarder
// is the real one.
type Forwarder interface {
	Spans(ctx context.Context, runID uuid.UUID, spans []observability.RunnerSpan)
}

// seenRun is a run a sender reads, found by its trace id.
type seenRun struct {
	id, workflow uuid.UUID
	dataKey      string
}

// findRuns answers the runs among traceIDs that the sender on ctx reads, judged by row level security as the
// sender, so a run it may not read is found exactly as a trace id of no run; the earliest run holds a shared id.
const findRuns = `SELECT DISTINCT ON (trace_id) trace_id, id, workflow_id, coalesce(data_key, '') FROM run
	WHERE trace_id = ANY($1) ORDER BY trace_id, created_at, id`

// insertSent writes a sender's spans as rotor_app, whose policy holds the sender and the trace to the sender's own.
const insertSent = `INSERT INTO trace_span (trace, span_id, parent_span_id, name, kind, service, source, sender_id, sender_name,
		started_at, ended_at, status, status_message, attributes, events, sealed_content)
	SELECT $1, s.span_id, s.parent, s.name, s.kind, s.service, $2, $3, $4, s.started_at, s.ended_at, s.status, s.message,
		s.attributes::jsonb, s.events::jsonb, s.sealed
	FROM unnest($5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::timestamptz[], $11::timestamptz[],
		$12::text[], $13::text[], $14::text[], $15::text[], $16::bytea[])
		WITH ORDINALITY AS s(span_id, parent, name, kind, service, started_at, ended_at, status, message, attributes, events, sealed, n)
	ORDER BY s.n`

// Receive keeps a sender's checked spans as the sender on ctx: a span whose trace id is a run's the sender reads joins
// that run's trace, any other the sender's own trace of that id. It answers the spans the store did not keep, and why.
func (s *Service) Receive(ctx context.Context, sender Sender, spans []observability.RunnerSpan) (otlp.Rejections, error) {
	var rejected otlp.Rejections
	var order []trace.TraceID
	byTrace := map[trace.TraceID][]observability.RunnerSpan{}
	for _, sp := range spans {
		if _, ok := byTrace[sp.TraceID]; !ok {
			order = append(order, sp.TraceID)
		}
		byTrace[sp.TraceID] = append(byTrace[sp.TraceID], sp)
	}
	runs, err := s.runsOf(ctx, order)
	if err != nil {
		return rejected, err
	}
	keep := s.Keys != nil && s.KeepContent(ctx)
	for _, id := range order {
		run, joins := runs[id]
		batch := held(byTrace[id], sender, run, joins)
		var kept int
		if joins {
			kept, err = s.intoRun(ctx, sender, run, id, batch, keep)
		} else {
			kept, err = s.intoOwn(ctx, sender, id, batch, keep)
		}
		if errors.Is(err, ErrTraceTaken) {
			rejected.Add(ReasonRunHeld, len(batch))
			continue
		}
		if err != nil {
			return rejected, err
		}
		rejected.Add(ReasonCapped, len(batch)-kept)
		if joins && kept > 0 {
			s.forward(ctx, run.id, batch)
		}
	}
	return rejected, nil
}

// held stamps each span with what Rotor says of it, then redacts it and holds it to the limits as a runner's span is.
func held(spans []observability.RunnerSpan, sender Sender, run seenRun, joins bool) []observability.RunnerSpan {
	out := make([]observability.RunnerSpan, len(spans))
	for i, sp := range spans {
		attrs := maps.Clone(sp.Attributes)
		if attrs == nil {
			attrs = map[string]string{}
		}
		attrs["rotor.sender"] = sender.ID.String()
		if sender.Key != uuid.Nil {
			attrs["rotor.ingest_key"] = sender.Key.String()
		}
		if joins {
			attrs["rotor.run"], attrs["rotor.workflow"] = run.id.String(), run.workflow.String()
		}
		sp.Attributes = attrs
		sp = observability.Hold(sp)
		sp.Service = cutRunes(redact.Text(sp.Service), config.MaxSpanNameChars)
		out[i] = sp
	}
	return out
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func hasText(spans []observability.RunnerSpan) bool {
	return slices.ContainsFunc(spans, func(sp observability.RunnerSpan) bool {
		for k := range sp.Attributes {
			if observability.IsText(k) {
				return true
			}
		}
		return false
	})
}

// runsOf finds the runs among ids that the sender on ctx reads.
func (s *Service) runsOf(ctx context.Context, ids []trace.TraceID) (map[trace.TraceID]seenRun, error) {
	hex := make([]string, len(ids))
	for i, id := range ids {
		hex[i] = id.String()
	}
	out := map[trace.TraceID]seenRun{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, findRuns, hex)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var traceID string
			var r seenRun
			if err := rows.Scan(&traceID, &r.id, &r.workflow, &r.dataKey); err != nil {
				return err
			}
			if id, err := trace.TraceIDFromHex(traceID); err == nil {
				out[id] = r
			}
		}
		return rows.Err()
	})
	return out, err
}

// intoRun keeps spans in the run's trace, made with the first of them as the runner's store would; their text is
// sealed with the run's key while Rotor keeps text.
func (s *Service) intoRun(ctx context.Context, sender Sender, run seenRun, id trace.TraceID, spans []observability.RunnerSpan,
	keep bool) (int, error) {
	var box *envelope.Box
	if keep && run.dataKey != "" && hasText(spans) {
		var err error
		if box, err = s.Keys.For(ctx, run.id, run.dataKey); err != nil {
			return 0, err
		}
	}
	var kept int
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO trace (trace_id, run_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id.String(), run.id); err != nil {
			return err
		}
		var traceRow uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM trace WHERE run_id = $1 AND trace_id = $2`, run.id, id.String()).Scan(&traceRow)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTraceTaken
		}
		if err != nil {
			return err
		}
		kept, err = insertRows(ctx, tx, traceRow, sender, spans, box)
		return err
	})
	return kept, err
}

// intoOwn keeps spans in the sender's own trace of id, made with the first of them; text is sealed with the trace's
// own key, made when it first seals any.
func (s *Service) intoOwn(ctx context.Context, sender Sender, id trace.TraceID, spans []observability.RunnerSpan, keep bool) (int, error) {
	traceRow, box, err := s.ownTrace(ctx, sender.ID, id.String(), keep && hasText(spans))
	if err != nil {
		return 0, err
	}
	var kept int
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		kept, err = insertRows(ctx, tx, traceRow, sender, spans, box)
		return err
	})
	return kept, err
}

// insertRows writes spans into a trace as the sender and answers how many the cap's trigger took.
func insertRows(ctx context.Context, tx db.DBTX, traceRow uuid.UUID, sender Sender, spans []observability.RunnerSpan,
	box *envelope.Box) (int, error) {
	rows, err := rowsOf(traceRow, spans, box)
	if err != nil || len(rows.ids) == 0 {
		return 0, err
	}
	tag, err := tx.Exec(ctx, insertSent, traceRow, sourceOTLP, sender.ID, clean(sender.Name), rows.ids, rows.parents, rows.names,
		rows.kinds, rows.services, rows.starts, rows.ends, rows.statuses, rows.messages, rows.attributes, rows.events, rows.sealed)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
