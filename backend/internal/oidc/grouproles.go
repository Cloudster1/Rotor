package oidc

import (
	"slices"

	"github.com/cloudster1/rotor/backend/internal/auth"
)

// AdminGroup is the realm group whose members administer Rotor.
const AdminGroup = "rotor-administrators"

// RoleFor is the role a person's groups give them. Everyone in the realm may sign in, as a member: what they may
// do in the tools is for each tool to decide when a run acts as them.
func RoleFor(groups []string) string {
	if slices.Contains(groups, AdminGroup) {
		return auth.RoleAdmin
	}
	return auth.RoleMember
}
