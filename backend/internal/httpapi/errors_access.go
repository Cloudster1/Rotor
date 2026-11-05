package httpapi

import (
	"errors"
	"net/http"

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/perm"
)

var (
	errAccessUnavailable = &APIError{Status: http.StatusServiceUnavailable, Code: "access_unavailable",
		Message: "Roles and groups cannot be reached right now. Try again in a moment."}
	errMayNotGrant = ErrForbidden("Only an administrator of Rotor, of a workflow or of a skill may look people and groups up to grant them a role. " +
		"Ask one to grant it for you.")
	errOwnAccount = ErrConflict("That is your own account. Ask another administrator to switch it off.")
)

// accessRefusals are the domain's refusals, by the status that fits; each keeps the domain's own sentence.
var accessRefusals = []struct {
	err    error
	status int
	code   string
}{
	{perm.ErrForbidden, http.StatusForbidden, "forbidden"},
	{perm.ErrBuiltinRole, http.StatusConflict, "builtin_role"},
	{perm.ErrBuiltinGroup, http.StatusConflict, "builtin_group"},
	{perm.ErrManagedElsewhere, http.StatusConflict, "managed_elsewhere"},
	{perm.ErrLastAdministrator, http.StatusConflict, "last_administrator"},
	{auth.ErrLastActiveAdministrator, http.StatusConflict, "last_administrator"},
	{perm.ErrRoleNameTaken, http.StatusUnprocessableEntity, "name_taken"},
	{perm.ErrGroupNameTaken, http.StatusUnprocessableEntity, "name_taken"},
	{perm.ErrRoleKey, http.StatusUnprocessableEntity, "validation_failed"},
	{perm.ErrRoleName, http.StatusUnprocessableEntity, "validation_failed"},
	{perm.ErrGroupName, http.StatusUnprocessableEntity, "validation_failed"},
	{perm.ErrUnknownPermission, http.StatusUnprocessableEntity, "validation_failed"},
	{perm.ErrLockout, http.StatusUnprocessableEntity, "lockout"},
	{perm.ErrEveryoneAdministers, http.StatusUnprocessableEntity, "everyone_administers"},
	{perm.ErrScope, http.StatusUnprocessableEntity, "wrong_scope"},
	{perm.ErrSubject, http.StatusUnprocessableEntity, "validation_failed"},
	{perm.ErrOneScope, http.StatusUnprocessableEntity, "validation_failed"},
	{perm.ErrNotASkillRole, http.StatusUnprocessableEntity, "wrong_scope"},
}

// accessNotFound are the domain's missing things, each with where to look instead.
var accessNotFound = []struct {
	err     error
	message string
}{
	{perm.ErrRoleNotFound, "There is no such role. Reload the page to see the roles there are."},
	{perm.ErrGroupNotFound, "There is no such group. Reload the page to see the groups there are."},
	{perm.ErrNotFound, "There is no such grant. Reload the page to see the grants there are."},
	{perm.ErrNoSuchPerson, "There is no such person. People appear here once they have signed in to Rotor."},
	{auth.ErrNoSuchPerson, "There is no such person. Reload the page to see the people there are."},
}

// accessError maps what perm and auth refuse onto the wire shape, a sentence saying what to do.
func accessError(err error) error {
	for _, nf := range accessNotFound {
		if errors.Is(err, nf.err) {
			return ErrNotFound(nf.message)
		}
	}
	for _, r := range accessRefusals {
		if errors.Is(err, r.err) {
			return &APIError{Status: r.status, Code: r.code, Message: sentence(r.err.Error())}
		}
	}
	return err
}
