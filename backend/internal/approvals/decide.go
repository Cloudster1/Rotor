package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
)

// Decision is an approver's answer; Values are read only when approving.
type Decision struct {
	Approve bool
	Comment string
	Values  json.RawMessage
}

// ErrRunNotWaiting is a decision for a run that no longer waits for it, such as one cancelled meanwhile.
var ErrRunNotWaiting = errors.New("the run no longer waits for this approval")

// NotApproverError is a caller who may see an approval but not decide it.
type NotApproverError struct{ Group *string }

func (e *NotApproverError) Error() string {
	if e.Group != nil {
		return fmt.Sprintf("Only members of the group %s decide this approval.", *e.Group)
	}
	return "Only the person who started the run decides this approval."
}

// DecidedError is an approval that is no longer pending, saying how it ended.
type DecidedError struct{ Approval Approval }

func (e *DecidedError) Error() string {
	a := e.Approval
	switch {
	case a.Status == StatusApproved || a.Status == StatusRejected:
		return fmt.Sprintf("This approval was already %s by %s at %s.", a.Status, a.DecidedByName, stamp(a.DecidedAt))
	case a.Status == StatusPending:
		return fmt.Sprintf("This approval expired at %s, so it can no longer be decided.", a.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("This approval is %s, since its run ended, so it can no longer be decided.", a.Status)
}

// ValuesRefused is a decision whose comment or values the approval refuses, each problem placed by a JSON pointer.
type ValuesRefused struct{ Problems []spec.Problem }

func (e *ValuesRefused) Error() string { return "the values do not fit the approval's form" }

// Decide approves or rejects an approval the caller may decide, and moves its run on: approved, it resumes in a
// fresh sandbox; rejected, it ends cancelled. The decision is audited, never with the values.
func (s *Service) Decide(ctx context.Context, id uuid.UUID, d Decision) (Approval, error) {
	p := auth.PrincipalFrom(ctx)
	if p == nil {
		return Approval{}, auth.ErrNoSession
	}
	// A person decides in Rotor's pages, never an assistant acting for them; the database holds the same (#47).
	if p.ThroughAssistant() {
		return Approval{}, ErrInPerson
	}
	a, err := s.Get(ctx, id)
	if err != nil {
		return Approval{}, err
	}
	if a.Status != StatusPending || !a.ExpiresAt.After(time.Now()) {
		return Approval{}, &DecidedError{Approval: a}
	}
	if !a.CanDecide {
		return Approval{}, &NotApproverError{Group: a.Group}
	}
	if utf8.RuneCountInString(d.Comment) > config.MaxApprovalCommentChars {
		return Approval{}, &ValuesRefused{Problems: []spec.Problem{{Path: "comment", Code: spec.CodeValues,
			Message: fmt.Sprintf("A comment may be %d characters at most.", config.MaxApprovalCommentChars)}}}
	}
	status, values, edited := StatusRejected, json.RawMessage(nil), []string{}
	if d.Approve {
		status = StatusApproved
		if values, edited, err = approvedValues(a, d.Values); err != nil {
			return Approval{}, err
		}
	}
	// The approved values are sealed with the run's key, bound to the interrupt they answer (#91).
	var sealed []byte
	if values != nil {
		box, err := s.keys.For(ctx, a.RunID, a.dataKey)
		if err != nil {
			return Approval{}, err
		}
		sealed = box.Seal(envelope.ColumnApprovalApproved, a.interrupt, values)
	}
	err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE approval SET status = $2, decided_by = $3, decided_by_name = $4, comment = $5,
				sealed_approved_values = $6, decided_at = now()
			WHERE id = $1 AND status = 'pending' AND expires_at > now()`, id, status, p.UserID, p.Name, d.Comment, sealed)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errRaced
		}
		err = runnerctl.Decide(ctx, tx, s.keys, a.RunID, runnerctl.Decided{ApprovalID: id, Approve: d.Approve, By: p.UserID, ByName: p.Name, Comment: d.Comment})
		if errors.Is(err, runnerctl.ErrNotWaiting) {
			return ErrRunNotWaiting
		}
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionApprovalDecided, TargetType: "approval", TargetID: id.String(), Actor: p.UserID,
			Data: map[string]any{"run": a.RunID.String(), "workflow": a.WorkflowID.String(), "node": a.Node, "decision": decisionName(d.Approve),
				"startedBy": a.StarterID.String(), "edited": edited}})
	})
	if errors.Is(err, errRaced) {
		if again, gerr := s.Get(ctx, id); gerr == nil {
			return Approval{}, &DecidedError{Approval: again}
		}
	}
	if err != nil {
		return Approval{}, err
	}
	if s.signals != nil {
		s.signals.Publish(context.WithoutCancel(ctx), runnerctl.ControlChannel(a.RunID), nil)
		s.signals.Publish(context.WithoutCancel(ctx), runnerctl.EventsChannel(a.RunID), nil)
	}
	return s.Get(ctx, id)
}

// errRaced is an approval someone else decided, or that expired, between reading and deciding it.
var errRaced = errors.New("the approval changed while it was decided")

// approvedValues merges what the approver sent over the proposed values and checks them against the form; it names
// the fields that changed, for the audit log.
func approvedValues(a Approval, sent json.RawMessage) (json.RawMessage, []string, error) {
	proposed := map[string]any{}
	_ = json.Unmarshal(a.FormValues, &proposed)
	if len(a.FormSchema) == 0 {
		return json.RawMessage("{}"), []string{}, nil
	}
	if len(sent) > config.MaxApprovalValueBytes {
		return nil, nil, &ValuesRefused{Problems: []spec.Problem{{Code: spec.CodeValues, Message: "The values are too large to keep."}}}
	}
	changes := map[string]any{}
	if len(sent) > 0 && string(sent) != "null" {
		if err := json.Unmarshal(sent, &changes); err != nil {
			return nil, nil, &ValuesRefused{Problems: []spec.Problem{{Code: spec.CodeValues, Message: "The values are not a JSON object. Send them field by field."}}}
		}
	}
	fields, _ := spec.FormFields(a.FormSchema)
	var problems []spec.Problem
	merged, edited := map[string]any{}, []string{}
	for k, v := range proposed {
		merged[k] = v
	}
	for k, v := range changes {
		if _, ok := fields[k]; !ok {
			problems = append(problems, spec.Problem{Path: "/" + k, Code: spec.CodeValues, Message: k + " is not a field of this approval's form."})
			continue
		}
		if !reflect.DeepEqual(proposed[k], v) {
			edited = append(edited, k)
		}
		merged[k] = v
	}
	if len(problems) > 0 {
		return nil, nil, &ValuesRefused{Problems: problems}
	}
	out, _ := json.Marshal(merged)
	form := spec.Form{Schema: a.FormSchema}
	if problems := form.CheckValues(out); len(problems) > 0 {
		return nil, nil, &ValuesRefused{Problems: problems}
	}
	sort.Strings(edited)
	return out, edited, nil
}

func decisionName(approve bool) string {
	if approve {
		return "approve"
	}
	return "reject"
}

func stamp(t *time.Time) string {
	if t == nil {
		return "an unknown time"
	}
	return t.UTC().Format(time.RFC3339)
}
