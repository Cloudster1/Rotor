package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/triggers"
)

// Triggers keeps the triggers of the person on the context (#34); *triggers.Service is the real one.
type Triggers interface {
	List(ctx context.Context, workflowID uuid.UUID) ([]triggers.Trigger, error)
	Get(ctx context.Context, id uuid.UUID) (triggers.Trigger, error)
	Create(ctx context.Context, workflowID uuid.UUID, in triggers.Input) (triggers.Trigger, error)
	Change(ctx context.Context, id uuid.UUID, in triggers.Input) (triggers.Trigger, error)
	Remove(ctx context.Context, id uuid.UUID) error
	Enable(ctx context.Context, id uuid.UUID) (triggers.Trigger, error)
	Disable(ctx context.Context, id uuid.UUID) (triggers.Trigger, error)
	Preview(cron, zone string) ([]time.Time, error)
}

var (
	errTriggersUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "triggers_unavailable",
		Message: "Triggers are not set up on this installation. An operator connects Rotor to OpenBao and starts its worker."}
	errTriggerNotFound = ErrNotFound("There is no such trigger, or it is not yours.")
	errNotTriggerOwner = &APIError{Status: http.StatusForbidden, Code: "not_trigger_owner",
		Message: "Only the trigger's owner may change it or turn it on, since it starts runs as them. You may turn it off or remove it."}
)

// triggerError says what went wrong with a trigger in the words its caller needs.
func triggerError(err error) error {
	var refused *triggers.Refused
	var schedule *triggers.ScheduleProblem
	switch {
	case errors.Is(err, triggers.ErrNotFound):
		return errTriggerNotFound
	case errors.Is(err, triggers.ErrNotOwner):
		return errNotTriggerOwner
	case errors.As(err, &schedule):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "trigger_invalid", Fields: map[string]string{"schedule/" + schedule.Field: schedule.Message},
			Message: "The schedule does not read. " + schedule.Message}
	case errors.As(err, &refused):
		fields, code := map[string]string{}, "trigger_invalid"
		for _, p := range refused.Problems {
			if p.Code == triggers.ReasonUnattendedWrite {
				code = triggers.ReasonUnattendedWrite
			}
			if _, taken := fields[p.Field]; !taken {
				fields[p.Field] = p.Message
			}
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: code, Fields: fields,
			Message: "The trigger cannot start this workflow yet. " + refused.Error()}
	}
	return runError(err)
}

func (s *Server) triggersReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Triggers == nil {
		respondError(w, r, errTriggersUnavailable)
		return false
	}
	return true
}

func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	s.listTriggers(w, r, uuid.Nil)
}

func (s *Server) handleListWorkflowTriggers(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if s.Workflows != nil {
		if _, _, err := s.Workflows.Get(r.Context(), id); err != nil {
			respondError(w, r, workflowError(err))
			return
		}
	}
	s.listTriggers(w, r, id)
}

func (s *Server) listTriggers(w http.ResponseWriter, r *http.Request, workflowID uuid.UUID) {
	if !s.triggersReady(w, r) {
		return
	}
	list, err := s.Triggers.List(r.Context(), workflowID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]triggerResponse, 0, len(list))
	for _, t := range list {
		out = append(out, toTriggerResponse(t))
	}
	respondJSON(w, r, http.StatusOK, env{"triggers": out})
}

func (s *Server) handleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req triggerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.triggersReady(w, r) {
		return
	}
	t, err := s.Triggers.Create(r.Context(), id, toTriggerInput(req))
	s.answerTrigger(w, r, http.StatusCreated, t, err)
}

func (s *Server) handleGetTrigger(w http.ResponseWriter, r *http.Request) {
	s.onTrigger(w, r, func(ctx context.Context, id uuid.UUID) (triggers.Trigger, error) { return s.Triggers.Get(ctx, id) })
}

func (s *Server) handleChangeTrigger(w http.ResponseWriter, r *http.Request) {
	var req triggerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	s.onTrigger(w, r, func(ctx context.Context, id uuid.UUID) (triggers.Trigger, error) {
		return s.Triggers.Change(ctx, id, toTriggerInput(req))
	})
}

func (s *Server) handleEnableTrigger(w http.ResponseWriter, r *http.Request) {
	s.onTrigger(w, r, func(ctx context.Context, id uuid.UUID) (triggers.Trigger, error) { return s.Triggers.Enable(ctx, id) })
}

func (s *Server) handleDisableTrigger(w http.ResponseWriter, r *http.Request) {
	s.onTrigger(w, r, func(ctx context.Context, id uuid.UUID) (triggers.Trigger, error) { return s.Triggers.Disable(ctx, id) })
}

func (s *Server) handleRemoveTrigger(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "triggerID", "trigger")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.triggersReady(w, r) {
		return
	}
	if err := s.Triggers.Remove(r.Context(), id); err != nil {
		respondError(w, r, triggerError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleSpec
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.triggersReady(w, r) {
		return
	}
	runs, err := s.Triggers.Preview(req.Cron, req.TimeZone)
	if err != nil {
		respondError(w, r, triggerError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, schedulePreview{NextRuns: runs})
}

// onTrigger answers an operation on the trigger the path names.
func (s *Server) onTrigger(w http.ResponseWriter, r *http.Request, do func(ctx context.Context, id uuid.UUID) (triggers.Trigger, error)) {
	id, perr := pathUUID(r, "triggerID", "trigger")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.triggersReady(w, r) {
		return
	}
	t, err := do(r.Context(), id)
	s.answerTrigger(w, r, http.StatusOK, t, err)
}

func (s *Server) answerTrigger(w http.ResponseWriter, r *http.Request, status int, t triggers.Trigger, err error) {
	if err != nil {
		respondError(w, r, triggerError(err))
		return
	}
	respondJSON(w, r, status, toTriggerResponse(t))
}

func toTriggerInput(req triggerRequest) triggers.Input {
	in := triggers.Input{Name: req.Name, Kind: string(req.Kind), Inputs: req.Inputs}
	if req.Schedule != nil {
		in.Cron, in.TimeZone, in.NoOverlap = req.Schedule.Cron, req.Schedule.TimeZone, req.Schedule.NoOverlap
	}
	if req.Event != nil {
		in.ToolID, _ = uuid.Parse(req.Event.ToolID)
		in.Topics, in.Projects = req.Event.Topics, req.Event.Projects
		in.Conditions, _ = json.Marshal(req.Event.Conditions)
	}
	return in
}

func toTriggerResponse(t triggers.Trigger) triggerResponse {
	out := triggerResponse{ID: t.ID.String(), Workflow: personRef{ID: t.WorkflowID.String(), Name: t.WorkflowName},
		Owner: personRef{ID: t.OwnerID.String(), Name: t.OwnerName}, Name: t.Name, Kind: triggerKind(t.Kind), Enabled: t.Enabled,
		Inputs: orObject(t.Inputs), NextRunAt: t.NextRunAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if t.DisabledReason != "" && t.DisabledAt != nil {
		out.Disabled = &triggerDisabled{Reason: disabledReason(t.DisabledReason), Message: t.DisabledDetail, At: *t.DisabledAt}
	}
	switch t.Kind {
	case triggers.KindSchedule:
		out.Schedule = &scheduleSpec{Cron: t.Cron, TimeZone: t.TimeZone, NoOverlap: t.NoOverlap}
	case triggers.KindEvent:
		event := &eventSpec{Topics: t.Topics, Projects: t.Projects, Conditions: []triggerCondition{}}
		if t.ToolID != nil {
			event.ToolID = t.ToolID.String()
		}
		_ = json.Unmarshal(t.Conditions, &event.Conditions)
		out.Event = event
	}
	if t.LastFiredAt != nil {
		fire := &triggerFire{Outcome: fireOutcome(t.LastOutcome), Reason: t.LastReason, At: *t.LastFiredAt}
		if t.LastRunID != nil {
			id := t.LastRunID.String()
			fire.RunID = &id
		}
		if t.LastRunStatus != nil {
			status := runStatus(*t.LastRunStatus)
			fire.RunStatus = &status
		}
		out.LastFire = fire
	}
	return out
}
