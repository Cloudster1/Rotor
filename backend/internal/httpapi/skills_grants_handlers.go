package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/perm"
	"github.com/cloudster1/rotor/backend/internal/skills"
)

// skillAccess reads the skill in the path and refuses one the caller may not read, or reads but may not administer
// when administer is asked for; the skill's own row answers, as the policies do, so a built-in one is never shared.
func (s *Server) skillAccess(w http.ResponseWriter, r *http.Request, administer bool) (uuid.UUID, bool) {
	id, ok := s.skillInPath(w, r)
	if !ok || !s.accessReady(w, r) {
		return uuid.Nil, false
	}
	k, err := s.Skills.Skill(r.Context(), id)
	switch {
	case err != nil:
		respondError(w, r, skillError(err))
		return uuid.Nil, false
	case administer && k.Builtin:
		respondError(w, r, errSkillBuiltin)
		return uuid.Nil, false
	case administer && !k.CanAdminister:
		respondError(w, r, errNotSkillAdministrator)
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleListSkillGrants(w http.ResponseWriter, r *http.Request) {
	id, ok := s.skillAccess(w, r, false)
	if !ok {
		return
	}
	grants, err := s.AccessAdmin.SkillAssignments(r.Context(), id)
	if errors.Is(err, perm.ErrForbidden) {
		err = skills.ErrNotFound
	}
	if err != nil {
		respondError(w, r, skillError(accessError(err)))
		return
	}
	out := make([]assignmentView, 0, len(grants))
	for _, a := range grants {
		out = append(out, toAssignmentView(a))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"grants": out})
}

func (s *Server) handleGrantOnSkill(w http.ResponseWriter, r *http.Request) {
	var req workflowGrantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	id, ok := s.skillAccess(w, r, true)
	if !ok {
		return
	}
	in, perr := subjectOf(req.UserID, req.GroupID)
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	in.Role, in.Skill = perm.Role(req.Role), id
	s.grant(w, r, in, "grant")
}

func (s *Server) handleRevokeOnSkill(w http.ResponseWriter, r *http.Request) {
	id, ok := s.skillAccess(w, r, true)
	if !ok {
		return
	}
	grantID, perr := pathUUID(r, "grantID", "grant")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	found, err := s.AccessAdmin.Assignment(r.Context(), grantID)
	if err == nil && (found.SkillID == nil || *found.SkillID != id) {
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
