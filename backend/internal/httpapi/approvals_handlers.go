package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/approvals"
	"github.com/cloudster1/rotor/backend/internal/config"
)

// Approvals lists and decides approvals as the person on the context (#31); *approvals.Service is the real one.
type Approvals interface {
	List(ctx context.Context, f approvals.Filter) ([]approvals.Approval, error)
	Count(ctx context.Context) (int, error)
	Get(ctx context.Context, id uuid.UUID) (approvals.Approval, error)
	Decide(ctx context.Context, id uuid.UUID, d approvals.Decision) (approvals.Approval, error)
}

var (
	errApprovalsUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "approvals_unavailable",
		Message: "Approvals are not set up on this installation. An operator connects Rotor to OpenBao and starts its worker."}
	errApprovalNotFound = ErrNotFound("There is no such approval, or you may not see it.")
	errRunNotWaiting    = &APIError{Status: http.StatusConflict, Code: "run_not_waiting",
		Message: "The run no longer waits for this approval, perhaps because it was cancelled. Open the run to see how it ended."}
	errNoDecision = ErrValidation(map[string]string{"decision": "Say approve or reject."})
)

// approvalError says what went wrong with an approval in the words its caller needs.
func approvalError(err error) error {
	var notApprover *approvals.NotApproverError
	var decided *approvals.DecidedError
	var refused *approvals.ValuesRefused
	switch {
	case errors.Is(err, approvals.ErrNotFound):
		return errApprovalNotFound
	case errors.As(err, &notApprover):
		return &APIError{Status: http.StatusForbidden, Code: "not_approver", Message: notApprover.Error()}
	case errors.As(err, &decided):
		return &APIError{Status: http.StatusConflict, Code: "approval_decided", Message: decided.Error()}
	case errors.Is(err, approvals.ErrRunNotWaiting):
		return errRunNotWaiting
	case errors.As(err, &refused):
		fields := map[string]string{}
		for _, p := range refused.Problems {
			where := "values" + p.Path
			if p.Path == "comment" {
				where = "comment"
			}
			if _, taken := fields[where]; !taken {
				fields[where] = p.Message
			}
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "values_invalid", Fields: fields,
			Message: "The decision does not fit the approval. " + refused.Problems[0].Message}
	}
	return err
}

func (s *Server) approvalsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.Approvals == nil {
		respondError(w, r, errApprovalsUnavailable)
		return false
	}
	return true
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.approvalsReady(w, r) {
		return
	}
	q := r.URL.Query()
	list, err := s.Approvals.List(r.Context(), approvals.Filter{Status: q.Get("status"), WaitingOnMe: q.Get("waitingOnMe") == "true"})
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]approvalSummary, 0, len(list))
	for _, a := range list {
		out = append(out, toApprovalSummary(a))
	}
	respondJSON(w, r, http.StatusOK, env{"approvals": out})
}

func (s *Server) handleCountApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.approvalsReady(w, r) {
		return
	}
	n, err := s.Approvals.Count(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, approvalCount{WaitingOnMe: n})
}

func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "approvalID", "approval")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.approvalsReady(w, r) {
		return
	}
	a, err := s.Approvals.Get(r.Context(), id)
	if err != nil {
		respondError(w, r, approvalError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toApprovalResponse(a))
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "approvalID", "approval")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req decisionRequest
	if err := decodeJSONWithin(w, r, &req, config.MaxApprovalValueBytes); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Decision != "approve" && req.Decision != "reject" {
		respondError(w, r, errNoDecision)
		return
	}
	if !s.approvalsReady(w, r) {
		return
	}
	a, err := s.Approvals.Decide(r.Context(), id, approvals.Decision{Approve: req.Decision == "approve", Comment: req.Comment, Values: req.Values})
	if err != nil {
		respondError(w, r, approvalError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, toApprovalResponse(a))
}

func toApprovalSummary(a approvals.Approval) approvalSummary {
	return approvalSummary{ID: a.ID.String(), Node: a.Node, Message: a.Message, Status: approvalStatus(a.Status),
		Run: approvalRun{ID: a.RunID.String(), Workflow: personRef{ID: a.WorkflowID.String(), Name: a.WorkflowName},
			StartedBy: personRef{ID: a.StarterID.String(), Name: a.StartedByName}},
		Approvers: approvers{Group: a.Group}, CanDecide: a.CanDecide, RequestedAt: a.RequestedAt, ExpiresAt: a.ExpiresAt, DecidedAt: a.DecidedAt}
}

func toApprovalResponse(a approvals.Approval) approvalResponse {
	out := approvalResponse{approvalSummary: toApprovalSummary(a), Preview: []previewWrite{}}
	if len(a.Preview) > 0 {
		_ = json.Unmarshal(a.Preview, &out.Preview)
	}
	if len(a.FormSchema) > 0 {
		out.Form = &approvalForm{Schema: a.FormSchema, Values: orObject(a.FormValues)}
	}
	if a.DecidedBy != nil && (a.Status == approvals.StatusApproved || a.Status == approvals.StatusRejected) {
		decision := approvalDecision("approve")
		if a.Status == approvals.StatusRejected {
			decision = "reject"
		}
		at := a.RequestedAt
		if a.DecidedAt != nil {
			at = *a.DecidedAt
		}
		out.Decision = &approvalDecided{Decision: decision, By: personRef{ID: a.DecidedBy.String(), Name: a.DecidedByName},
			Comment: a.Comment, Values: orObject(a.ApprovedValues), At: at}
	}
	return out
}

func orObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}
