package runnerctl

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
	"github.com/cloudster1/rotor/backend/internal/usage"
)

// Stop ends a run that passed a limit as failed with the reason, and tells its runner to stop; the runner's own
// Done that follows finds the run ended and changes nothing. The caller publishes ControlChannel once committed.
func Stop(ctx context.Context, tx db.DBTX, keys *envelope.Keys, runID uuid.UUID, reason, message string, detail map[string]any) error {
	current, err := lockStatus(ctx, tx, runID)
	if err != nil || Ended(current) {
		return err
	}
	payload := map[string]any{"message": message}
	for k, v := range detail {
		payload[k] = v
	}
	if err := endLocked(ctx, tx, keys, runID, StatusFailed, reason, payload, nil); err != nil {
		return err
	}
	if current != StatusStarting && current != StatusRunning {
		return nil
	}
	cancel := &runnerpb.ConnectResponse{Message: &runnerpb.ConnectResponse_Cancel{Cancel: &runnerpb.Cancel{Reason: message}}}
	_, err = enqueueTx(ctx, tx, keys, runID, cancel)
	return err
}

// recordUsage counts an llm.usage event toward the run's and its person's limits and stops the run past one; it
// answers whether it stopped it.
func recordUsage(ctx context.Context, tx db.DBTX, keys *envelope.Keys, r *run, payload []byte) (bool, error) {
	totals, err := usage.Add(ctx, tx, r.ID, payload)
	if err != nil {
		return false, err
	}
	limit, err := usage.Limits(ctx, tx)
	if err != nil {
		return false, err
	}
	reason := usage.Over(totals, r.spec.Limits, limit)
	if reason == "" {
		return false, nil
	}
	return true, Stop(ctx, tx, keys, r.ID, reason, usage.Sentence(reason), usage.Detail(totals))
}

// openUsage keeps the prices the run's model has as its stream opens, once; a run without a model counts tokens only.
func (s *Server) openUsage(ctx context.Context, r *run) error {
	var provider *uuid.UUID
	model := ""
	if r.model != nil {
		provider, model = &r.model.Provider.ID, r.model.Model
	}
	return s.DB.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return usage.Open(ctx, tx, r.ID, provider, model)
	})
}
