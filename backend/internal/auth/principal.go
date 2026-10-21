// Package auth keeps signed-in browsers: opaque session tokens whose SHA-256 alone is stored,
// and the person each one acts for.
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Roles a person holds in Rotor, copied from the realm's groups at each sign-in.
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
	Role      string
	Groups    []string
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

// IsAdmin says whether the person administers Rotor.
func (p *Principal) IsAdmin() bool { return p != nil && p.Role == RoleAdmin }

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
