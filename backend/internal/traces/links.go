package traces

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// linkedTraces finds, as the reader, the trace each linked trace id leads to: a run's first, then the reader's own,
// then another sender's an administrator reads. Row level security leaves out any the reader may not read, so a
// trace id of no trace and one of a trace the reader may not read come back alike, as none.
const linkedTraces = `SELECT DISTINCT ON (t.trace_id) t.trace_id, t.run_id, t.owner_id FROM trace t
	WHERE t.trace_id = ANY($1)
	ORDER BY t.trace_id, t.run_id IS NULL, t.owner_id IS DISTINCT FROM current_actor_id(), t.id`

// whereLinksLead says of each link of spans whose trace it leads to, where the reader may read it (#262).
func whereLinksLead(ctx context.Context, tx db.DBTX, spans []Span) error {
	var ids []string
	seen := map[string]bool{}
	for _, sp := range spans {
		for _, l := range sp.Links {
			if !seen[l.TraceID] {
				seen[l.TraceID] = true
				ids = append(ids, l.TraceID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	type target struct{ run, owner *uuid.UUID }
	found := map[string]target{}
	rows, err := tx.Query(ctx, linkedTraces, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var t target
		if err := rows.Scan(&id, &t.run, &t.owner); err != nil {
			return err
		}
		found[id] = t
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range spans {
		for j := range spans[i].Links {
			l := &spans[i].Links[j]
			if t, ok := found[l.TraceID]; ok {
				l.RunID, l.Owner = t.run, t.owner
			}
		}
	}
	return nil
}
