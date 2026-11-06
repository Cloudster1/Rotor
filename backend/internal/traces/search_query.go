package traces

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// queryCanceled is Postgres's code for a statement its statement_timeout stopped.
const queryCanceled = "57014"

// modelOf is the model a span called, as Model reads it in Go.
const modelOf = `coalesce(s.attributes->>'llm.model_name', s.attributes->>'gen_ai.request.model', '')`

// searchColumns are a hit alone: joining what it belongs to here would make the policies weigh every trace, run and
// workflow once more, so belongings reads that for the page's traces afterwards.
const searchColumns = `SELECT s.trace, s.span_id, s.name, s.kind, s.service, s.source, s.started_at, s.ended_at, s.status,
		s.status_message, ` + modelOf + ` FROM trace_span s`

// searchOrder is newest first, the trace and span id breaking ties so a cursor names one place.
const searchOrder = ` ORDER BY s.started_at DESC, s.trace DESC, s.span_id DESC LIMIT `

// pageTraces are the run or the sender each of a page's traces belongs to, read as the caller: a workflow it may not
// read has no name.
const pageTraces = `SELECT t.id, t.trace_id, t.run_id, r.workflow_id, (SELECT w.name FROM workflow w WHERE w.id = r.workflow_id),
		t.owner_id, CASE WHEN t.owner_id IS NOT NULL THEN ` + ownerName + ` END
	FROM trace t LEFT JOIN run r ON r.id = t.run_id WHERE t.id = ANY($1)`

// SearchSpans is a page of the spans the caller reads that f matches, newest first, as row level security decides;
// past config.SpanSearchTimeout it is ErrSearchTooBroad.
func (s *Service) SearchSpans(ctx context.Context, f SpanFilter) (SpanPage, error) {
	query, args := SearchQuery(f)
	out := SpanPage{Hits: []SpanHit{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		timeout := strconv.FormatInt(config.SpanSearchTimeout.Milliseconds(), 10)
		if _, err := tx.Exec(ctx, `SELECT set_config('statement_timeout', $1, true)`, timeout); err != nil {
			return err
		}
		if err := scanHits(ctx, tx, query, args, &out); err != nil || len(out.Hits) == 0 {
			return err
		}
		return belongings(ctx, tx, out.Hits)
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == queryCanceled {
		return SpanPage{}, ErrSearchTooBroad
	}
	if err != nil {
		return SpanPage{}, err
	}
	// One more than the page was asked for, to know whether a next page holds any.
	if len(out.Hits) > f.Limit {
		out.Hits, out.More = out.Hits[:f.Limit], true
	}
	return out, nil
}

func scanHits(ctx context.Context, tx db.DBTX, query string, args []any, out *SpanPage) error {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var h SpanHit
		if err := rows.Scan(&h.At.Trace, &h.SpanID, &h.Name, &h.Kind, &h.Service, &h.Source, &h.StartedAt, &h.EndedAt,
			&h.Status, &h.StatusMessage, &h.Model); err != nil {
			return err
		}
		h.At.At, h.At.SpanID = h.StartedAt, h.SpanID
		out.Hits = append(out.Hits, h)
	}
	return rows.Err()
}

// belongings fills in the trace id and the run or sender of each hit, reading each of the page's traces once.
func belongings(ctx context.Context, tx db.DBTX, hits []SpanHit) error {
	at := map[uuid.UUID][]int{}
	var rowIDs []uuid.UUID
	for i, h := range hits {
		if _, seen := at[h.At.Trace]; !seen {
			rowIDs = append(rowIDs, h.At.Trace)
		}
		at[h.At.Trace] = append(at[h.At.Trace], i)
	}
	rows, err := tx.Query(ctx, pageTraces, rowIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row uuid.UUID
		var traceID string
		var run, workflow, owner *uuid.UUID
		var workflowName, ownerName *string
		if err := rows.Scan(&row, &traceID, &run, &workflow, &workflowName, &owner, &ownerName); err != nil {
			return err
		}
		for _, i := range at[row] {
			h := &hits[i]
			h.TraceID, h.RunID = traceID, run
			if workflow != nil {
				h.Workflow = &WorkflowRef{ID: *workflow, Name: deref(workflowName)}
			}
			if owner != nil {
				h.Owner = &Sender{ID: *owner, Name: deref(ownerName)}
			}
		}
	}
	return rows.Err()
}

// SearchQuery is the statement SearchSpans runs, with only the conditions f asks for, so the planner sees each as it
// is and picks the index that fits: the kind's, the failed spans', or the start's. The tests explain it.
func SearchQuery(f SpanFilter) (string, []any) {
	args := []any{f.Since, f.Until}
	where := []string{"s.started_at >= $1", "s.started_at < $2"}
	add := func(cond string, values ...any) {
		for _, v := range values {
			args = append(args, v)
			cond = strings.Replace(cond, "?", "$"+strconv.Itoa(len(args)), 1)
		}
		where = append(where, cond)
	}
	if c := f.After; c != nil {
		// The first comparison alone is one an index range can take ahead of the policies; the rest breaks ties.
		add("s.started_at <= ?", c.At)
		add("(s.started_at, s.trace, s.span_id) < (?, ?, ?)", c.At, c.Trace, c.SpanID)
	}
	if f.Kind != "" {
		add("s.kind = ?", f.Kind)
	}
	if f.Status != "" {
		add("s.status = ?", f.Status)
	}
	if f.Service != "" {
		add("s.service = ?", f.Service)
	}
	if f.Name != "" {
		add("strpos(lower(s.name), lower(?)) > 0", f.Name)
	}
	if f.Model != "" {
		add("strpos(lower("+modelOf+"), lower(?)) > 0", f.Model)
	}
	if f.AttrKey != "" {
		add("s.attributes->>? = ?", f.AttrKey, f.AttrValue)
	}
	if f.Workflow != nil {
		add("s.trace IN (SELECT t.id FROM trace t JOIN run r ON r.id = t.run_id WHERE r.workflow_id = ?)", *f.Workflow)
	}
	args = append(args, f.Limit+1)
	return searchColumns + " WHERE " + strings.Join(where, " AND ") + searchOrder + "$" + strconv.Itoa(len(args)), args
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
