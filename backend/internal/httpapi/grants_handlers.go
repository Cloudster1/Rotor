package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

var (
	errNoSuchWorkflow           = ErrNotFound("There is no such workflow, or it is not shared with you.")
	errNotWorkflowAdministrator = ErrForbidden("Only an administrator of this workflow or of Rotor may change who holds its roles. " +
		"Ask one of them to do it.")
)

func idString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func toAssignmentView(a perm.Assignment) assignmentView {
	return assignmentView{ID: a.ID.String(), Role: string(a.Role), RoleName: a.RoleName, WorkflowID: idString(a.WorkflowID),
		WorkflowName: a.WorkflowName, SkillID: idString(a.SkillID), SkillName: a.SkillName, UserID: idString(a.UserID), UserName: a.UserName, UserEmail: a.UserEmail,
		GroupID: idString(a.GroupID), GroupName: a.GroupName, GroupBuiltin: a.GroupBuiltin, CreatedAt: a.CreatedAt}
}

// queryInt reads a whole number from the query, held between low and high; anything else is the fallback.
func queryInt(r *http.Request, name string, fallback, low, high int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || n < low || n > high {
		return fallback
	}
	return n
}

// optionalUUID parses an id a request may leave out; a malformed one is the field's to fix.
func optionalUUID(raw *string, field, what string) (*uuid.UUID, *APIError) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return nil, ErrValidation(map[string]string{field: "That is not a valid " + what + " id. Choose it again from the list."})
	}
	return &id, nil
}

func (s *Server) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	if !s.accessReady(w, r) {
		return
	}
	q := r.URL.Query()
	filter := perm.AssignmentFilter{Text: q.Get("q"), Role: perm.Role(q.Get("role")),
		Limit:  queryInt(r, "limit", config.AccessPageSize, 1, config.AccessMaxPage),
		Offset: queryInt(r, "offset", 0, 0, config.AccessMaxOffset)}
	if raw := q.Get("workflow"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("That is not a valid workflow id. Choose the workflow again."))
			return
		}
		filter.Workflow = id
	}
	page, err := s.AccessAdmin.ListAssignments(r.Context(), filter)
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	out := assignmentPage{Assignments: make([]assignmentView, 0, len(page.Assignments)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, a := range page.Assignments {
		out.Assignments = append(out.Assignments, toAssignmentView(a))
	}
	respondJSON(w, r, http.StatusOK, out)
}

// grant gives the role and answers with the grant as a listing shows it, under key.
func (s *Server) grant(w http.ResponseWriter, r *http.Request, in perm.GrantInput, key string) {
	id, err := s.AccessAdmin.Grant(r.Context(), in, actorOf(r))
	if err != nil {
		respondError(w, r, asValidationError(accessError(err)))
		return
	}
	made, err := s.AccessAdmin.Assignment(r.Context(), id)
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{key: toAssignmentView(*made)})
}

// subjectOf reads whom a grant goes to; the store refuses both or neither in a sentence of its own.
func subjectOf(userID, groupID *string) (perm.GrantInput, *APIError) {
	user, perr := optionalUUID(userID, "userId", "person")
	if perr != nil {
		return perm.GrantInput{}, perr
	}
	group, perr := optionalUUID(groupID, "groupId", "group")
	if perr != nil {
		return perm.GrantInput{}, perr
	}
	return perm.GrantInput{UserID: user, GroupID: group}, nil
}

func (s *Server) handleGrantRole(w http.ResponseWriter, r *http.Request) {
	var req grantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in, perr := subjectOf(req.UserID, req.GroupID)
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	workflow, perr := optionalUUID(req.WorkflowID, "workflowId", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if workflow != nil {
		in.Workflow = *workflow
	}
	in.Role = perm.Role(req.Role)
	if !s.accessReady(w, r) {
		return
	}
	s.grant(w, r, in, "assignment")
}

func (s *Server) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "assignmentID", "grant")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	if err := s.AccessAdmin.Revoke(r.Context(), id, actorOf(r)); err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondNoContent(w)
}

// workflowAccess reads the workflow in the path and refuses one the caller may not see, or may see but not administer
// when administer is asked for; what the grants say decides, as the policies do.
func workflowAccess(w http.ResponseWriter, r *http.Request, administer bool) (uuid.UUID, bool) {
	id, perr := pathUUID(r, "workflowID", "workflow")
	if perr != nil {
		respondError(w, r, perr)
		return uuid.Nil, false
	}
	set := auth.PrincipalFrom(r.Context()).Perms
	switch {
	case !set.Can(perm.WorkflowRead, id):
		respondError(w, r, errNoSuchWorkflow)
		return uuid.Nil, false
	case administer && !set.Can(perm.WorkflowAdminister, id):
		respondError(w, r, errNotWorkflowAdministrator)
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleListWorkflowGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := workflowAccess(w, r, false)
	if !ok || !s.accessReady(w, r) {
		return
	}
	grants, err := s.AccessAdmin.WorkflowAssignments(r.Context(), id)
	if errors.Is(err, perm.ErrForbidden) {
		err = errNoSuchWorkflow
	}
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	out := make([]assignmentView, 0, len(grants))
	for _, a := range grants {
		out = append(out, toAssignmentView(a))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": out})
}

func (s *Server) handleGrantOnWorkflow(w http.ResponseWriter, r *http.Request) {
	var req workflowGrantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	id, ok := workflowAccess(w, r, true)
	if !ok || !s.accessReady(w, r) {
		return
	}
	in, perr := subjectOf(req.UserID, req.GroupID)
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	in.Role, in.Workflow = perm.Role(req.Role), id
	s.grant(w, r, in, "grant")
}

func (s *Server) handleRevokeOnWorkflow(w http.ResponseWriter, r *http.Request) {
	id, ok := workflowAccess(w, r, true)
	if !ok || !s.accessReady(w, r) {
		return
	}
	grantID, perr := pathUUID(r, "grantID", "grant")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	found, err := s.AccessAdmin.Assignment(r.Context(), grantID)
	if err == nil && (found.WorkflowID == nil || *found.WorkflowID != id) {
		err = perm.ErrNotFound
	}
	if err == nil {
		err = s.AccessAdmin.Revoke(r.Context(), grantID, actorOf(r))
	}
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleFindPeople(w http.ResponseWriter, r *http.Request) {
	if !mayGrant(r) {
		respondError(w, r, errMayNotGrant)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	people, err := s.AccessAdmin.People(r.Context(), r.URL.Query().Get("q"),
		queryInt(r, "limit", config.PeopleLookupSize, 1, config.PeopleLookupMax))
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	out := make([]personView, 0, len(people))
	for _, p := range people {
		out = append(out, personView{ID: p.ID.String(), Name: p.Name, Email: p.Email})
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"people": out})
}
