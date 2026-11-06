// Package auth keeps signed-in browsers: opaque session tokens whose SHA-256 alone is stored,
// and the person each one acts for.
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/perm"
)

// Roles a person holds in Rotor, the word their grants give them: admin is holding rotor.administer installation-wide.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Errors the session checks name, so the HTTP layer can answer each one in a sentence.
var (
	// ErrNoSession is a missing, unknown or expired token.
	ErrNoSession = errors.New("no such session")
	// ErrSwitchedOff is a person an administrator switched off, refused even with a live session.
	ErrSwitchedOff = errors.New("the account is switched off")
)

// Principal is the person a request acts for.
type Principal struct {
	UserID uuid.UUID
	// SessionID is the browser's session; it is uuid.Nil when an assistant acts for the person.
	SessionID uuid.UUID
	Email     string
	Name      string
	Groups    []string
	// Perms is what the person's grants and their groups' let them do, resolved for each request (#202).
	Perms perm.Set
	// Assistant is the grant an assistant acts through (#47), nil when the person acts in the browser.
	Assistant *Assistant
}

// Assistant is the grant a person gave an assistant at Rotor's consent page.
type Assistant struct {
	GrantID uuid.UUID
	// Client is the assistant's name, as it registered.
	Client string
	// ReadOnly is a grant of reading alone, which the person chose or the assistant asked for.
	ReadOnly bool
}

// IsAdmin says whether the person administers Rotor: holds rotor.administer over the whole installation.
func (p *Principal) IsAdmin() bool { return p != nil && p.Perms.CanInstallation(perm.RotorAdminister) }

// RoleFor is the role word a set of permissions gives, which /auth/me answers for the web client.
func RoleFor(set perm.Set) string {
	if set.CanInstallation(perm.RotorAdminister) {
		return RoleAdmin
	}
	return RoleMember
}

// ThroughAssistant says whether an assistant acts for the person rather than the person in their browser.
func (p *Principal) ThroughAssistant() bool { return p != nil && p.Assistant != nil }

type principalKey struct{}

// WithPrincipal puts the signed-in person on the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom is the signed-in person, or nil for an anonymous request.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
