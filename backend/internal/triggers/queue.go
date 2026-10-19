// Package triggers keeps a person's schedule and event triggers and starts their workflows as them (#34, #35):
// the scheduler and the hook endpoint queue firings, and the worker's fire pass starts the runs.
package triggers

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// ScheduleKey is the firing key of a schedule's due time; a time fires a trigger once.
func ScheduleKey(due time.Time) string { return "schedule:" + strconv.FormatInt(due.Unix(), 10) }

// EventKey is the firing key of a tool's event; an event fires a trigger once, however often it is delivered.
func EventKey(eventID string) string { return "event:" + eventID }

// Enqueue records a firing of a trigger for the worker to start, once per key, in the caller's transaction as
// rotor_admin; it answers whether the firing is new. inputs are what the run starts with besides the trigger's own.
func Enqueue(ctx context.Context, tx db.DBTX, triggerID uuid.UUID, key string, inputs map[string]any, due time.Time) (bool, error) {
	if inputs == nil {
		inputs = map[string]any{}
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO trigger_fire (trigger_id, fire_key, inputs, due_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (trigger_id, fire_key) DO NOTHING`, triggerID, key, encoded, due)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
