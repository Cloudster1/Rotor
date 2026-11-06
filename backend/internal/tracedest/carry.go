package tracedest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// carryLock is held while the LangSmith setting is carried over, so api replicas starting together take turns.
const carryLock = "rotor.tracedest.langsmith"

// The headers LangSmith reads a run's spans by.
const (
	LangSmithKeyHeader     = "x-api-key"
	LangSmithProjectHeader = "Langsmith-Project"
)

// LangSmithKeys is where the LangSmith key was kept before #246; *vault.Client is the real one.
type LangSmithKeys interface {
	LangSmithKey(ctx context.Context) (vault.LangSmithKey, int, error)
	DeleteLangSmithKey(ctx context.Context) error
}

// CarryOver finishes what migration 01520 began, once, as the api starts: the LangSmith key and project become the
// carried-over destination's headers and the old key goes. It answers whether this start did it.
func (s *Service) CarryOver(ctx context.Context, keys LangSmithKeys) (bool, error) {
	var done bool
	err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		done = false
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, carryLock); err != nil {
			return err
		}
		var carried *time.Time
		var project string
		if err := tx.QueryRow(ctx, `SELECT carried_at, project FROM langsmith_setting`).Scan(&carried, &project); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		if carried != nil {
			return nil
		}
		var id uuid.UUID
		var name string
		var enabled, content bool
		err := tx.QueryRow(ctx, `SELECT id, name, enabled, content FROM trace_destination WHERE carried_over FOR UPDATE`).
			Scan(&id, &name, &enabled, &content)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// LangSmith never held a key, so nothing became a destination.
			_, err = tx.Exec(ctx, `UPDATE langsmith_setting SET carried_at = now()`)
			return err
		case err != nil:
			return err
		}
		key, _, err := keys.LangSmithKey(ctx)
		switch {
		case err == nil:
			headers := map[string]string{LangSmithKeyHeader: key.Key, LangSmithProjectHeader: project}
			setAt := key.SetAt
			if setAt.IsZero() {
				setAt = s.now().UTC()
			}
			if err := s.headers.PutDestinationHeaders(ctx, id, vault.DestinationHeaders{Headers: headers, SetAt: setAt}); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE trace_destination SET endpoint = $2, header_names = $3, headers_set_at = $4 WHERE id = $1`,
				id, s.langSmithEndpoint, names(headers), setAt)
		case errors.Is(err, vault.ErrNotFound):
			// Without its key the destination has nothing to send with, so it waits, off, for an administrator.
			enabled, content = false, false
			_, err = tx.Exec(ctx, `UPDATE trace_destination SET endpoint = $2, enabled = false, content = false,
				content_acknowledged_by = NULL, content_acknowledged_at = NULL WHERE id = $1`, id, s.langSmithEndpoint)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE langsmith_setting SET carried_at = now()`); err != nil {
			return err
		}
		done = true
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTraceDestinationCarriedOver, TargetType: "trace_destination",
			TargetID: id.String(), Data: map[string]any{"name": name, "enabled": enabled, "content": content}})
	})
	if err != nil {
		return false, err
	}
	// The old key goes only once the row holds its copy; a start that stopped between the two deletes it next time.
	return done, keys.DeleteLangSmithKey(context.WithoutCancel(ctx))
}
