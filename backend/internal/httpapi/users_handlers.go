package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// Switches lets people in and out of Rotor for Admin's Users page; *auth.Service is the real one.
type Switches interface {
	SwitchOff(ctx context.Context, actor, userID uuid.UUID) error
	SwitchOn(ctx context.Context, actor, userID uuid.UUID) error
}

func toUserView(u perm.User) userView {
	out := userView{ID: u.ID.String(), Name: u.Name, Email: u.Email, IsActive: u.Active, CreatedAt: u.CreatedAt,
		Groups: make([]userGroupView, 0, len(u.Groups)), Grants: make([]userGrantView, 0, len(u.Grants))}
	for _, g := range u.Groups {
		out.Groups = append(out.Groups, userGroupView{ID: g.ID.String(), Name: g.Name, Source: groupSource(g.Source), Builtin: g.Builtin})
	}
	for _, g := range u.Grants {
		out.Grants = append(out.Grants, userGrantView{Role: string(g.Role), RoleName: g.RoleName, WorkflowID: idString(g.WorkflowID),
			WorkflowName: g.WorkflowName, Group: g.Group})
	}
	return out
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !s.accessReady(w, r) {
		return
	}
	page, err := s.AccessAdmin.Users(r.Context(), perm.UserFilter{Text: r.URL.Query().Get("q"),
		Limit:  queryInt(r, "limit", config.AccessPageSize, 1, config.AccessMaxPage),
		Offset: queryInt(r, "offset", 0, 0, config.AccessMaxOffset)})
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	out := userPage{Users: make([]userView, 0, len(page.Users)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, u := range page.Users {
		out.Users = append(out.Users, toUserView(u))
	}
	respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, perr := pathUUID(r, "userID", "person")
	if perr != nil {
		respondError(w, r, perr)
		return
	}
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	if req.IsActive != nil {
		if s.Switches == nil {
			respondError(w, r, errAccessUnavailable)
			return
		}
		var err error
		switch {
		case *req.IsActive:
			err = s.Switches.SwitchOn(r.Context(), actorOf(r), id)
		case id == actorOf(r):
			err = errOwnAccount
		default:
			err = s.Switches.SwitchOff(r.Context(), actorOf(r), id)
		}
		if err != nil {
			respondError(w, r, accessError(err))
			return
		}
	}
	found, err := s.AccessAdmin.User(r.Context(), id)
	if err != nil {
		respondError(w, r, accessError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"user": toUserView(*found)})
}
