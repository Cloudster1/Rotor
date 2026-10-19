package runs

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// atCapacity says, in SQL over run r, whether its workflow's maxConcurrent or its person's concurrent runs are all
// taken (#38); runs starting or running count, and one waiting for an approval holds no sandbox, so it does not.
const atCapacity = `(EXISTS (SELECT 1 FROM workflow_version v WHERE v.workflow_id = r.workflow_id AND v.number = r.version
		AND (v.spec -> 'limits' ->> 'maxConcurrent')::int <= (SELECT count(*) FROM run x
			WHERE x.workflow_id = r.workflow_id AND x.status IN ('starting', 'running') AND x.id <> r.id))
	OR COALESCE((SELECT l.concurrent_runs FROM usage_limit l) <= (SELECT count(*) FROM run x
			WHERE x.user_id = r.user_id AND x.status IN ('starting', 'running') AND x.id <> r.id), false))`

// errHeld is a queued run left queued: a place frees up when another of its workflow's or person's runs ends.
var errHeld = errors.New("the run waits for a place")

// holdForCapacity counts again, under a lock per workflow and per person, whether the queued run may start, so one
// pass of several workers never starts more than the limits allow.
func holdForCapacity(ctx context.Context, tx db.DBTX, runID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('concurrency:workflow:' || workflow_id::text, 0)),
			pg_advisory_xact_lock(hashtextextended('concurrency:person:' || user_id::text, 0))
		FROM run WHERE id = $1`, runID); err != nil {
		return err
	}
	var full bool
	if err := tx.QueryRow(ctx, `SELECT `+atCapacity+` FROM run r WHERE r.id = $1`, runID).Scan(&full); err != nil {
		return err
	}
	if full {
		return errHeld
	}
	return nil
}
