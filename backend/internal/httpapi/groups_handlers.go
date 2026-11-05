package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

func toGroupView(g perm.Group, withMembers bool) groupView {
	out := groupView{ID: g.ID.String(), Name: g.Name, Description: g.Description, Source: groupSource(g.Source),
		ExternalRef: g.ExternalRef, Builtin: g.Builtin, MemberCount: g.MemberCount, RoleCount: g.RoleCount}
	if withMembers {
		out.Members = make([]memberView, 0, len(g.Members))
		for _, m := range g.Members {
			out.Members = append(out.Members, memberView{UserID: m.UserID.String(), Name: m.Name, Email: m.Email})
		}
		out.MemberCount = len(g.Members)
	}
	return out
}

// mayGrant says whether the caller administers Rotor or some workflow or skill, which is who looks people and groups up.
func mayGrant(r *http.Request) bool {
	set := auth.PrincipalFrom(r.Context()).Perms
	return set.CanSomewhere(perm.WorkflowAdminister) || set.CanSomewhere(perm.SkillAdminister)
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if !mayGrant(r) {
		respondError(w, r, errMayNotGrant)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	groups, err := s.AccessAdmin.Groups(r.Context())
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	out := make([]groupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupView(g, false))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groups": out})
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "groupID", "group")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	found, err := s.AccessAdmin.GroupByID(r.Context(), id)
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": toGroupView(*found, true)})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	made, err := s.AccessAdmin.CreateGroup(r.Context(), perm.GroupInput{Name: req.Name, Description: req.Description,
		ExternalRef: req.ExternalRef}, actorOf(r))
	if err != nil {
		respondError(w, r, asValidationError(accessError(err)))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"group": toGroupView(*made, true)})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "groupID", "group")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	if err := s.AccessAdmin.DeleteGroup(r.Context(), id, actorOf(r)); err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondNoContent(w)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "groupID", "group")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req addMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	person, err := uuid.Parse(req.UserID)
	if err != nil {
		respondError(w, r, ErrValidation(map[string]string{"userId": "Choose the person to add to the group."}))
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	changed, err := s.AccessAdmin.AddToGroup(r.Context(), id, person, actorOf(r))
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": toGroupView(*changed, true)})
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "groupID", "group")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	person, perr := pathUUID(r, "userID", "person")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	changed, err := s.AccessAdmin.RemoveFromGroup(r.Context(), id, person, actorOf(r))
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": toGroupView(*changed, true)})
}
