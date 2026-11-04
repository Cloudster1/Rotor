package httpapi

import (
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// The rows of #203: Admin's Access and Users pages, and a workflow's grants; docs/api-contract-access.md says what
// they mean.
func init() {
	operations = append(operations, accessAdminOperations...)
}

var (
	pageQuery = []param{
		{name: "limit", description: "At most this many, 50 when left out and never more than 200.", schema: &openapi.Schema{Type: "integer"}},
		{name: "offset", description: "How many to skip, for the next page.", schema: &openapi.Schema{Type: "integer"}},
	}
	refused = errorEnvelope{}
)

var accessAdminOperations = []operation{
	// What the roles mean is everyone's to read: a client that cannot ask has to draw every button and refuse on click.
	{method: "GET", path: "/roles", handler: "handleListRoles", tag: "access",
		summary:   "The installation's roles, built in and made by its administrators, with what each grants and how many hold it.",
		responses: ok(env{"roles": []roleView{}})},
	{method: "GET", path: "/permissions", handler: "handleListPermissions", tag: "access",
		summary: "Every permission Rotor checks for, in the words the matrix shows, in its order.", responses: ok(env{"permissions": []permissionView{}})},
	{method: "POST", path: "/roles", handler: "handleCreateRole", tag: "access", admin: true,
		summary:     "Add a role of the installation's own.",
		description: "The key is made from the name when it is left out. A new role grants what permissions lists, and nothing else.",
		request:     createRoleRequest{},
		responses:   map[int]any{http.StatusCreated: env{"role": roleView{}}, http.StatusUnprocessableEntity: refused}},
	{method: "PATCH", path: "/roles/{roleKey}", handler: "handleUpdateRole", tag: "access", admin: true,
		summary:     "Rename a role, describe it, or set everything it grants.",
		description: "permissions is the whole list the role grants from now on, not a change to it. The Administrator keeps administering Rotor.",
		request:     updateRoleRequest{},
		responses:   map[int]any{http.StatusOK: env{"role": roleView{}}, http.StatusNotFound: notFound, http.StatusUnprocessableEntity: refused}},
	{method: "DELETE", path: "/roles/{roleKey}", handler: "handleDeleteRole", tag: "access", admin: true,
		summary:   "Delete a role an administrator added, and every grant of it; the built-in roles stay.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound, http.StatusConflict: refused}},

	// Groups: Everyone, the realm's, and those kept by hand.
	{method: "GET", path: "/groups", handler: "handleListGroups", tag: "access",
		summary:     "The groups a role may be granted to: Everyone first, then the realm's, then those made by hand.",
		description: "For anybody who administers Rotor or a workflow; anybody else is refused with 403.",
		responses:   map[int]any{http.StatusOK: env{"groups": []groupView{}}, http.StatusForbidden: refused}},
	{method: "POST", path: "/groups", handler: "handleCreateGroup", tag: "access", admin: true,
		summary:     "Make a group by hand, or one that matches a value of the realm's groups claim.",
		description: "A group with an externalRef takes its members from the claim at every sign-in; one without is kept here.",
		request:     createGroupRequest{},
		responses:   map[int]any{http.StatusCreated: env{"group": groupView{}}, http.StatusUnprocessableEntity: refused}},
	{method: "GET", path: "/groups/{groupID}", handler: "handleGetGroup", tag: "access", admin: true,
		summary: "One group with the people in it.", responses: map[int]any{http.StatusOK: env{"group": groupView{}}, http.StatusNotFound: notFound}},
	{method: "DELETE", path: "/groups/{groupID}", handler: "handleDeleteGroup", tag: "access", admin: true,
		summary:   "Delete a group and everything granted to it; Everyone stays.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound, http.StatusConflict: refused}},
	{method: "POST", path: "/groups/{groupID}/members", handler: "handleAddGroupMember", tag: "access", admin: true,
		summary:     "Put somebody in a group kept by hand.",
		description: "Everyone and the realm's groups follow sign-in alone, so they are refused with 409.",
		request:     addMemberRequest{},
		responses:   map[int]any{http.StatusOK: env{"group": groupView{}}, http.StatusNotFound: notFound, http.StatusConflict: refused}},
	{method: "DELETE", path: "/groups/{groupID}/members/{userID}", handler: "handleRemoveGroupMember", tag: "access", admin: true,
		summary:   "Take somebody out of a group kept by hand.",
		responses: map[int]any{http.StatusOK: env{"group": groupView{}}, http.StatusNotFound: notFound, http.StatusConflict: refused}},

	// Grants over the whole installation and every workflow, for Admin's Access page.
	{method: "GET", path: "/role-assignments", handler: "handleListAssignments", tag: "access", admin: true,
		summary: "Who holds which role where, a page at a time: the installation-wide grants first, then by workflow.",
		query: append([]param{
			{name: "workflow", description: "Keep the grants that answer on this workflow, the installation-wide ones included."},
			{name: "q", description: "Match the person, the group or the workflow's name."},
			{name: "role", description: "Keep the grants of this role."},
		}, pageQuery...),
		responses: ok(assignmentPage{})},
	{method: "POST", path: "/role-assignments", handler: "handleGrantRole", tag: "access", admin: true,
		summary:     "Grant a role to a person or a group, over the whole installation or one workflow.",
		description: "Granting what is already granted answers with the grant there is.",
		request:     grantRequest{},
		responses:   map[int]any{http.StatusCreated: env{"assignment": assignmentView{}}, http.StatusUnprocessableEntity: refused}},
	{method: "DELETE", path: "/role-assignments/{assignmentID}", handler: "handleRevokeRole", tag: "access", admin: true,
		summary:   "Take a grant away; the last Administrator grant stays.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound, http.StatusConflict: refused}},

	// A workflow's grants, which replace its sharing: anybody who sees it reads them, its administrators change them.
	{method: "GET", path: "/workflows/{workflowID}/grants", handler: "handleListWorkflowGrants", tag: "access",
		summary:   "The roles granted on this workflow alone, for anybody who may see it.",
		responses: map[int]any{http.StatusOK: env{"grants": []assignmentView{}}, http.StatusNotFound: notFound}},
	{method: "POST", path: "/workflows/{workflowID}/grants", handler: "handleGrantOnWorkflow", tag: "access",
		summary:     "Grant a person or a group a role on this workflow, such as Runner, Editor or Workflow administrator.",
		description: "For whoever administers the workflow or Rotor. A role that answers for the whole installation is refused with 422.",
		request:     workflowGrantRequest{},
		responses: map[int]any{http.StatusCreated: env{"grant": assignmentView{}}, http.StatusNotFound: notFound,
			http.StatusForbidden: refused, http.StatusUnprocessableEntity: refused}},
	{method: "DELETE", path: "/workflows/{workflowID}/grants/{grantID}", handler: "handleRevokeOnWorkflow", tag: "access",
		summary:   "Take a grant on this workflow away, for whoever administers the workflow or Rotor.",
		responses: map[int]any{http.StatusNoContent: nil, http.StatusNotFound: notFound, http.StatusForbidden: refused}},
	{method: "GET", path: "/people", handler: "handleFindPeople", tag: "access",
		summary:     "The people let in whose name or address matches, for a picker that grants them a role.",
		description: "For anybody who administers Rotor or a workflow; anybody else is refused with 403.",
		query: []param{
			{name: "q", description: "Part of a name or an address."},
			{name: "limit", description: "At most this many, 10 when left out and never more than 50.", schema: &openapi.Schema{Type: "integer"}},
		},
		responses: map[int]any{http.StatusOK: env{"people": []personView{}}, http.StatusForbidden: refused}},

	// Admin's Users page.
	{method: "GET", path: "/users", handler: "handleListUsers", tag: "access", admin: true,
		summary:   "Every person Rotor knows, a page at a time, with their groups and their roles.",
		query:     append([]param{{name: "q", description: "Part of a name or an address."}}, pageQuery...),
		responses: ok(userPage{})},
	{method: "PATCH", path: "/users/{userID}", handler: "handleUpdateUser", tag: "access", admin: true,
		summary:     "Switch a person off, which ends their sessions and connections, or on again.",
		description: "Your own account and the last active administrator are refused with 409.",
		request:     updateUserRequest{},
		responses:   map[int]any{http.StatusOK: env{"user": userView{}}, http.StatusNotFound: notFound, http.StatusConflict: refused}},
}
