package httpapi

import (
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

// groupSource says who keeps a group's members: the realm's groups claim, or Rotor's administrators by hand.
type groupSource string

func registerAccessSchemas(b *openapi.Builder) {
	b.Enums[reflect.TypeOf(groupSource(""))] = []string{perm.SourceLocal, perm.SourceOIDC}
}

// roleView is one of the installation's roles, as the matrix shows it.
type roleView struct {
	Key                  string   `json:"key"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	InstallationWideOnly bool     `json:"installationWideOnly"`
	Builtin              bool     `json:"builtin"`
	Permissions          []string `json:"permissions"`
	// InUse counts the grants of it, so deleting it can say what goes with it.
	InUse int `json:"inUse"`
}

// permissionView is one permission in the words the matrix shows.
type permissionView struct {
	Permission string `json:"permission"`
	Words      string `json:"words"`
}

// createRoleRequest is a new role; the key is made from the name when left out.
type createRoleRequest struct {
	Key                  string   `json:"key,omitempty"`
	Name                 string   `json:"name"`
	Description          string   `json:"description,omitempty"`
	InstallationWideOnly bool     `json:"installationWideOnly,omitempty"`
	Permissions          []string `json:"permissions"`
}

// updateRoleRequest changes what it names; permissions, when sent, is the whole list.
type updateRoleRequest struct {
	Name                 *string   `json:"name,omitempty"`
	Description          *string   `json:"description,omitempty"`
	InstallationWideOnly *bool     `json:"installationWideOnly,omitempty"`
	Permissions          *[]string `json:"permissions,omitempty"`
}

// groupView is a group with how many people it holds and how many grants it has; one read alone carries its members.
type groupView struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Source      groupSource `json:"source"`
	// ExternalRef is the value of the realm's groups claim it matches, empty for one kept by hand.
	ExternalRef string `json:"externalRef"`
	// Builtin is "everyone" for the group of every person let in, empty otherwise.
	Builtin     string       `json:"builtin"`
	MemberCount int          `json:"memberCount"`
	RoleCount   int          `json:"roleCount"`
	Members     []memberView `json:"members,omitempty"`
}

// memberView is a person in a group.
type memberView struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

// createGroupRequest is a new group; an externalRef makes the realm keep its members.
type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ExternalRef string `json:"externalRef,omitempty"`
}

// addMemberRequest names the person to put in a group.
type addMemberRequest struct {
	UserID string `json:"userId"`
}

// assignmentView is one grant with the names filled in: the role, where it answers and whom it goes to.
type assignmentView struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	RoleName string `json:"roleName"`
	// WorkflowID and SkillID are left out for a grant over the whole installation; at most one is set.
	WorkflowID   *string `json:"workflowId,omitempty"`
	WorkflowName string  `json:"workflowName,omitempty"`
	SkillID      *string `json:"skillId,omitempty"`
	SkillName    string  `json:"skillName,omitempty"`
	// A grant goes to a person or to a group: exactly one of the two is set.
	UserID    *string `json:"userId,omitempty"`
	UserName  string  `json:"userName,omitempty"`
	UserEmail string  `json:"userEmail,omitempty"`
	GroupID   *string `json:"groupId,omitempty"`
	GroupName string  `json:"groupName,omitempty"`
	// GroupBuiltin is "everyone" for a grant to Everyone.
	GroupBuiltin string    `json:"groupBuiltin,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// assignmentPage is one page of grants plus how many matched in all.
type assignmentPage struct {
	Assignments []assignmentView `json:"assignments"`
	Total       int              `json:"total"`
	Limit       int              `json:"limit"`
	Offset      int              `json:"offset"`
}

// grantRequest grants a role to one person or one group, over one workflow or, without one, the whole installation.
type grantRequest struct {
	Role       string  `json:"role"`
	WorkflowID *string `json:"workflowId,omitempty"`
	UserID     *string `json:"userId,omitempty"`
	GroupID    *string `json:"groupId,omitempty"`
}

// workflowGrantRequest grants a role on the workflow in the path to one person or one group.
type workflowGrantRequest struct {
	Role    string  `json:"role"`
	UserID  *string `json:"userId,omitempty"`
	GroupID *string `json:"groupId,omitempty"`
}

// personView is somebody a picker offers.
type personView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// userView is a person as Admin's Users page lists them.
type userView struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Email     string          `json:"email"`
	IsActive  bool            `json:"isActive"`
	CreatedAt time.Time       `json:"createdAt"`
	Groups    []userGroupView `json:"groups"`
	Grants    []userGrantView `json:"grants"`
}

// userGroupView is one group a person is in.
type userGroupView struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Source  groupSource `json:"source"`
	Builtin string      `json:"builtin"`
}

// userGrantView is one role a person holds; group names the group it came through, empty for one of their own.
type userGrantView struct {
	Role         string  `json:"role"`
	RoleName     string  `json:"roleName"`
	WorkflowID   *string `json:"workflowId,omitempty"`
	WorkflowName string  `json:"workflowName,omitempty"`
	SkillID      *string `json:"skillId,omitempty"`
	SkillName    string  `json:"skillName,omitempty"`
	Group        string  `json:"group,omitempty"`
}

// userPage is one page of people plus how many matched in all.
type userPage struct {
	Users  []userView `json:"users"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// updateUserRequest switches a person off or on.
type updateUserRequest struct {
	IsActive *bool `json:"isActive,omitempty"`
}
