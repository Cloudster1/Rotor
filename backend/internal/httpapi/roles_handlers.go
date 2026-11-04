package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// AccessAdmin keeps roles, groups and grants for Admin's Access and Users pages (#203); *perm.Store is the real one.
type AccessAdmin interface {
	Roles(ctx context.Context) ([]perm.Definition, error)
	CreateRole(ctx context.Context, in perm.RoleInput, actor uuid.UUID) (*perm.Definition, error)
	UpdateRole(ctx context.Context, in perm.RoleInput, actor uuid.UUID) (*perm.Definition, error)
	DeleteRole(ctx context.Context, key perm.Role, actor uuid.UUID) error
	Groups(ctx context.Context) ([]perm.Group, error)
	GroupByID(ctx context.Context, id uuid.UUID) (*perm.Group, error)
	CreateGroup(ctx context.Context, in perm.GroupInput, actor uuid.UUID) (*perm.Group, error)
	DeleteGroup(ctx context.Context, id, actor uuid.UUID) error
	AddToGroup(ctx context.Context, groupID, userID, actor uuid.UUID) (*perm.Group, error)
	RemoveFromGroup(ctx context.Context, groupID, userID, actor uuid.UUID) (*perm.Group, error)
	ListAssignments(ctx context.Context, f perm.AssignmentFilter) (perm.AssignmentPage, error)
	WorkflowAssignments(ctx context.Context, workflow uuid.UUID) ([]perm.Assignment, error)
	Assignment(ctx context.Context, id uuid.UUID) (*perm.Assignment, error)
	Grant(ctx context.Context, in perm.GrantInput, actor uuid.UUID) (uuid.UUID, error)
	Revoke(ctx context.Context, id, actor uuid.UUID) error
	People(ctx context.Context, text string, limit int) ([]perm.Person, error)
	Users(ctx context.Context, f perm.UserFilter) (perm.UserPage, error)
	User(ctx context.Context, id uuid.UUID) (*perm.User, error)
}

// accessReady answers 503 when the server was built without access administration.
func (s *Server) accessReady(w http.ResponseWriter, r *http.Request) bool {
	if s.AccessAdmin == nil {
		respondError(w, r, errAccessUnavailable)
		return false
	}
	return true
}

func actorOf(r *http.Request) uuid.UUID { return auth.PrincipalFrom(r.Context()).UserID }

func toRoleView(d perm.Definition) roleView {
	out := roleView{Key: string(d.Key), Name: d.Name, Description: d.Description, InstallationWideOnly: d.InstallationWideOnly,
		Builtin: d.Builtin, Permissions: make([]string, 0, len(d.Permissions)), InUse: d.InUse}
	for _, p := range d.Permissions {
		out.Permissions = append(out.Permissions, string(p))
	}
	return out
}

func permissionsOf(names []string) []perm.Permission {
	out := make([]perm.Permission, 0, len(names))
	for _, n := range names {
		out = append(out, perm.Permission(n))
	}
	return out
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if !s.accessReady(w, r) {
		return
	}
	roles, err := s.AccessAdmin.Roles(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	out := make([]roleView, 0, len(roles))
	for _, d := range roles {
		out = append(out, toRoleView(d))
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"roles": out})
}

func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	out := make([]permissionView, 0, len(perm.AllPermissions))
	for _, p := range perm.AllPermissions {
		out = append(out, permissionView{Permission: string(p), Words: perm.PermissionWords[p]})
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"permissions": out})
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req createRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	made, err := s.AccessAdmin.CreateRole(r.Context(), perm.RoleInput{
		Key: perm.Role(req.Key), Name: &req.Name, Description: &req.Description,
		InstallationWideOnly: &req.InstallationWideOnly, Permissions: permissionsOf(req.Permissions),
	}, actorOf(r))
	if err != nil {
		respondError(w, r, asValidationError(accessError(err)))
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"role": toRoleView(*made)})
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req updateRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !s.accessReady(w, r) {
		return
	}
	in := perm.RoleInput{Key: perm.Role(chi.URLParam(r, "roleKey")), Name: req.Name, Description: req.Description,
		InstallationWideOnly: req.InstallationWideOnly}
	if req.Permissions != nil {
		in.Permissions = permissionsOf(*req.Permissions)
	}
	saved, err := s.AccessAdmin.UpdateRole(r.Context(), in, actorOf(r))
	if err != nil {
		respondError(w, r, asValidationError(accessError(err)))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"role": toRoleView(*saved)})
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if !s.accessReady(w, r) {
		return
	}
	if err := s.AccessAdmin.DeleteRole(r.Context(), perm.Role(chi.URLParam(r, "roleKey")), actorOf(r)); err != nil {
		respondError(w, r, asValidationError(accessError(err)))
		return
	}
	respondNoContent(w)
}
