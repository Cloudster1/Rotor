package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// Access resolves what a person may do from their grants and their groups'; *perm.Store is the real one.
type Access interface {
	ResolveFor(ctx context.Context, userID uuid.UUID) (perm.Set, error)
}

// The rows of #202: what the caller may do, as their grants say.
func init() {
	operations = append(operations, accessOperations...)
}

var accessOperations = []operation{
	{method: "GET", path: "/access/me", handler: "handleMyAccess", tag: "auth",
		summary:     "What you may do in Rotor: your grants and the permissions they give, over the installation and per workflow.",
		description: "A grant to a group of yours counts as yours. A workflow named here is one granted to you over it alone.",
		responses:   ok(accessResponse{})},
}

// accessResponse is the caller's grants and what they add up to.
type accessResponse struct {
	Grants      []accessGrant     `json:"grants"`
	Permissions accessPermissions `json:"permissions"`
}

// accessGrant is one role the caller holds, over one workflow or skill or, without either, the whole installation.
type accessGrant struct {
	Role       string  `json:"role"`
	WorkflowID *string `json:"workflowId,omitempty"`
	SkillID    *string `json:"skillId,omitempty"`
}

// accessPermissions are the permissions the grants give: installation-wide, and per workflow or skill named explicitly.
type accessPermissions struct {
	Installation []string            `json:"installation"`
	Workflows    map[string][]string `json:"workflows"`
	Skills       map[string][]string `json:"skills"`
}

// resolveAccess puts what the person may do on their principal for this request.
func (s *Server) resolveAccess(ctx context.Context, p *auth.Principal) error {
	if s.Access == nil {
		return nil
	}
	set, err := s.Access.ResolveFor(ctx, p.UserID)
	if err != nil {
		return err
	}
	p.Perms = set
	return nil
}

func (s *Server) handleMyAccess(w http.ResponseWriter, r *http.Request) {
	set := auth.PrincipalFrom(r.Context()).Perms
	out := accessResponse{Grants: []accessGrant{}, Permissions: accessPermissions{Installation: []string{}, Workflows: map[string][]string{},
		Skills: map[string][]string{}}}
	for _, g := range set.Grants() {
		grant := accessGrant{Role: string(g.Role)}
		if g.Workflow != uuid.Nil {
			grant.WorkflowID = idString(&g.Workflow)
		}
		if g.Skill != uuid.Nil {
			grant.SkillID = idString(&g.Skill)
		}
		out.Grants = append(out.Grants, grant)
	}
	for _, p := range set.InstallationPermissions() {
		out.Permissions.Installation = append(out.Permissions.Installation, string(p))
	}
	addScoped(out.Permissions.Workflows, set.WorkflowPermissions())
	addScoped(out.Permissions.Skills, set.SkillPermissions())
	respondJSON(w, r, http.StatusOK, out)
}

// addScoped writes what is held on each workflow or skill, by its id.
func addScoped(into map[string][]string, held map[uuid.UUID][]perm.Permission) {
	for id, perms := range held {
		list := make([]string, 0, len(perms))
		for _, p := range perms {
			list = append(list, string(p))
		}
		into[id.String()] = list
	}
}
