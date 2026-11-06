package traces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// OwnedFilter narrows a page of senders' own traces; a nil Sender is every sender the caller reads.
type OwnedFilter struct {
	Sender        *uuid.UUID
	Limit, Offset int
}

// OwnedSummary is a sender's own trace in a list: its first root span names it.
type OwnedSummary struct {
	TraceID           string
	Sender            Sender
	Service, RootName string
	Spans             int
	Failed, Cut       bool
	StartedAt         time.Time
	EndedAt           *time.Time
}

// OwnedPage is a page of own traces and how many match in all.
type OwnedPage struct {
	Traces []OwnedSummary
	Total  int
}

// ownedRoot is the earliest span of a trace whose parent the trace does not hold.
const ownedRoot = `LEFT JOIN LATERAL (SELECT s.service, s.name FROM trace_span s WHERE s.trace = t.id
		AND (s.parent_span_id = '' OR NOT EXISTS (SELECT 1 FROM trace_span p WHERE p.trace = s.trace AND p.span_id = s.parent_span_id))
		ORDER BY s.started_at, s.span_id LIMIT 1) root ON true`

// ListOwned is a page of senders' own traces, newest first, as the caller reads them: their own, everyone's for an
// administrator. Row level security decides, so a filter on another sender finds nothing for a member.
func (s *Service) ListOwned(ctx context.Context, f OwnedFilter) (OwnedPage, error) {
	out := OwnedPage{Traces: []OwnedSummary{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM trace t WHERE t.owner_id IS NOT NULL AND ($1::uuid IS NULL OR t.owner_id = $1)`,
			f.Sender).Scan(&out.Total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT t.trace_id, t.owner_id, `+ownerName+`, coalesce(root.service, ''), coalesce(root.name, ''),
				t.spans, EXISTS (SELECT 1 FROM trace_span e WHERE e.trace = t.id AND e.status = 'error'), t.cut_at IS NOT NULL,
				t.started_at, (SELECT max(e.ended_at) FROM trace_span e WHERE e.trace = t.id)
			FROM trace t `+ownedRoot+`
			WHERE t.owner_id IS NOT NULL AND ($1::uuid IS NULL OR t.owner_id = $1)
			ORDER BY t.started_at DESC, t.id DESC LIMIT $2 OFFSET $3`, f.Sender, f.Limit, f.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o OwnedSummary
			if err := rows.Scan(&o.TraceID, &o.Sender.ID, &o.Sender.Name, &o.Service, &o.RootName, &o.Spans, &o.Failed, &o.Cut,
				&o.StartedAt, &o.EndedAt); err != nil {
				return err
			}
			out.Traces = append(out.Traces, o)
		}
		return rows.Err()
	})
	return out, err
}
