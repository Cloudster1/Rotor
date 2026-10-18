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
	UserID    uuid.UUID
	SessionID uuid.UUID
	Email     string
	Name      string
	Role      string
	Groups    []string
}

// IsAdmin says whether the person administers Rotor.
func (p *Principal) IsAdmin() bool { return p != nil && p.Role == RoleAdmin }

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
