package runs

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
)

// ErrEnded is a run that has already ended, which a cancel no longer changes.
var ErrEnded = runnerctl.ErrEnded

// ErrNotManager is a group approver who sees a run but may not cancel it; rejecting its approval stops it.
var ErrNotManager = errors.New("only the run's starter, its workflow's owner or an administrator may cancel it")

// Cancel ends a run that has not ended, for whoever may see it: the person who started it, the workflow's owner or
// an administrator. Its runner is told on whichever api replica serves it; the worker destroys its sandbox.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) (Run, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Run{}, connections.ErrNoSession
	}
	r, err := s.Get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if !r.Manages {
		return Run{}, ErrNotManager
	}
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := runnerctl.Cancel(ctx, tx, id, p.UserID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionRunCancelled, TargetType: "run", TargetID: id.String(), Actor: p.UserID,
			Data: map[string]any{"workflow": r.WorkflowID.String(), "status": r.Status, "startedBy": r.UserID.String()}})
	})
	if errors.Is(err, runnerctl.ErrEnded) {
		return Run{}, ErrEnded
	}
	if err != nil {
		return Run{}, err
	}
	if s.signals != nil {
		s.signals.Publish(context.WithoutCancel(ctx), runnerctl.ControlChannel(id), nil)
		s.signals.Publish(context.WithoutCancel(ctx), runnerctl.EventsChannel(id), nil)
	}
	return s.Get(ctx, id)
}
