package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
)

// ErrNotOwner is a trigger the caller sees, as an administrator, but is not theirs to change or turn on.
var ErrNotOwner = errors.New("only the trigger's owner may do this")

// Create makes a trigger on a workflow the caller may see, as theirs, turned on.
func (s *Service) Create(ctx context.Context, workflowID uuid.UUID, in Input) (Trigger, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Trigger{}, auth.ErrNoSession
	}
	if _, err := s.check(ctx, workflowID, in); err != nil {
		return Trigger{}, err
	}
	v := values(in, s.now())
	var id uuid.UUID
	err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `INSERT INTO trigger (workflow_id, owner_id, name, kind, cron, time_zone, no_overlap, next_run_at,
				tool_id, topics, projects, conditions, inputs, allow_unattended)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
			workflowID, p.UserID, in.Name, in.Kind, v.cron, v.zone, in.NoOverlap, v.next, v.tool, v.topics, v.projects, v.conditions, v.inputs,
			v.unattended).Scan(&id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerCreated, TargetType: "trigger", TargetID: id.String(), Actor: p.UserID,
			Data: map[string]any{"workflow": workflowID.String(), "kind": in.Kind, "name": in.Name}})
	})
	if err != nil {
		return Trigger{}, err
	}
	return s.Get(ctx, id)
}

// Change rewrites one of the caller's triggers, checked as when it was made; its kind and whether it is on stay.
func (s *Service) Change(ctx context.Context, id uuid.UUID, in Input) (Trigger, error) {
	t, err := s.own(ctx, id)
	if err != nil {
		return Trigger{}, err
	}
	if in.Kind != t.Kind {
		return Trigger{}, &Refused{Problems: []Problem{{Field: "kind", Code: CodeKind, Message: "A trigger keeps its kind; make a new one instead."}}}
	}
	if _, err := s.check(ctx, t.WorkflowID, in); err != nil {
		return Trigger{}, err
	}
	v := values(in, s.now())
	if !t.Enabled {
		v.next = nil
	}
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE trigger SET name = $2, cron = $3, time_zone = $4, no_overlap = $5, next_run_at = $6,
				tool_id = $7, topics = $8, projects = $9, conditions = $10, inputs = $11, allow_unattended = $12 WHERE id = $1`,
			id, in.Name, v.cron, v.zone, in.NoOverlap, v.next, v.tool, v.topics, v.projects, v.conditions, v.inputs, v.unattended); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerChanged, TargetType: "trigger", TargetID: id.String(), Actor: t.OwnerID,
			Data: map[string]any{"workflow": t.WorkflowID.String(), "name": in.Name}})
	})
	if err != nil {
		return Trigger{}, err
	}
	return s.Get(ctx, id)
}

// Enable turns one of the caller's triggers on, checked again; a schedule fires next at its next time from now.
func (s *Service) Enable(ctx context.Context, id uuid.UUID) (Trigger, error) {
	t, err := s.own(ctx, id)
	if err != nil {
		return Trigger{}, err
	}
	in := t.Input()
	if _, err := s.check(ctx, t.WorkflowID, in); err != nil {
		return Trigger{}, err
	}
	next := values(in, s.now()).next
	err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE trigger SET enabled = true, disabled_reason = '', disabled_detail = '', disabled_at = NULL,
				next_run_at = $2 WHERE id = $1`, id, next); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerEnabled, TargetType: "trigger", TargetID: id.String(), Actor: t.OwnerID,
			Data: map[string]any{"workflow": t.WorkflowID.String(), "name": t.Name}})
	})
	if err != nil {
		return Trigger{}, err
	}
	return s.Get(ctx, id)
}

// Disable turns a trigger off, for its owner or an administrator; it carries no reason, since a person chose it.
func (s *Service) Disable(ctx context.Context, id uuid.UUID) (Trigger, error) {
	p := auth.PrincipalFrom(ctx)
	t, err := s.Get(ctx, id)
	if err != nil || p == nil {
		return Trigger{}, errors.Join(err, noSession(p))
	}
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE trigger SET enabled = false, disabled_reason = '', disabled_detail = '', disabled_at = NULL,
				next_run_at = NULL WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerDisabled, TargetType: "trigger", TargetID: id.String(), Actor: p.UserID,
			Data: map[string]any{"workflow": t.WorkflowID.String(), "name": t.Name, "owner": t.OwnerID.String()}})
	})
	if err != nil {
		return Trigger{}, err
	}
	return s.Get(ctx, id)
}

// Remove deletes a trigger, for its owner or an administrator; the runs it started stay.
func (s *Service) Remove(ctx context.Context, id uuid.UUID) error {
	p := auth.PrincipalFrom(ctx)
	t, err := s.Get(ctx, id)
	if err != nil || p == nil {
		return errors.Join(err, noSession(p))
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM trigger WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionTriggerRemoved, TargetType: "trigger", TargetID: id.String(), Actor: p.UserID,
			Data: map[string]any{"workflow": t.WorkflowID.String(), "name": t.Name, "owner": t.OwnerID.String()}})
	})
}

// Preview is when a schedule fires next, or the problem with it.
func (s *Service) Preview(cron, zone string) ([]time.Time, error) {
	schedule, err := ParseSchedule(cron, zone)
	if err != nil {
		return nil, err
	}
	return schedule.NextRuns(s.now(), config.SchedulePreviewRuns), nil
}

// own is a trigger of the caller's; an administrator's view of someone else's is ErrNotOwner.
func (s *Service) own(ctx context.Context, id uuid.UUID) (Trigger, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Trigger{}, auth.ErrNoSession
	}
	t, err := s.Get(ctx, id)
	if err == nil && t.OwnerID != p.UserID {
		return Trigger{}, ErrNotOwner
	}
	return t, err
}

func noSession(p *auth.Principal) error {
	if p == nil {
		return auth.ErrNoSession
	}
	return nil
}

// Input is the trigger as it was made, to check it again.
func (t Trigger) Input() Input {
	in := Input{Name: t.Name, Kind: t.Kind, Cron: t.Cron, TimeZone: t.TimeZone, NoOverlap: t.NoOverlap, Topics: t.Topics,
		Projects: t.Projects, Conditions: t.Conditions, Inputs: t.Inputs, AllowUnattended: t.AllowUnattended}
	if t.ToolID != nil {
		in.ToolID = *t.ToolID
	}
	return in
}

// stored is an input as the trigger table keeps it; the other kind's columns stay empty.
type stored struct {
	cron, zone, next, tool any
	topics, projects       []string
	conditions, inputs     []byte
	unattended             []string
}

func values(in Input, now time.Time) stored {
	v := stored{topics: []string{}, projects: []string{}, conditions: []byte("[]"), inputs: objectOr(in.Inputs),
		unattended: nonNil(in.AllowUnattended)}
	switch in.Kind {
	case KindSchedule:
		v.cron, v.zone = in.Cron, in.TimeZone
		if schedule, err := ParseSchedule(in.Cron, in.TimeZone); err == nil {
			v.next = schedule.Next(now)
		}
	case KindEvent:
		v.tool, v.topics, v.projects = in.ToolID, nonNil(in.Topics), nonNil(in.Projects)
		if len(in.Conditions) > 0 && json.Valid(in.Conditions) && string(in.Conditions) != "null" {
			v.conditions = in.Conditions
		}
	}
	return v
}

func objectOr(raw json.RawMessage) []byte {
	var object map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &object) != nil || object == nil {
		return []byte("{}")
	}
	return raw
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
