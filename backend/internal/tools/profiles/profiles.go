// Package profiles says how each tool of the suite names its scopes and hands out tokens, which its metadata does
// not: which scope reads, which Rotor never asks for, and whether a run needs a token of its own by exchange (#45).
package profiles

import (
	"slices"
	"strings"
)

// Profile is what Rotor knows of one product beyond its metadata.
type Profile struct {
	Product string
	// ReadScopes are what a token for reading alone asks for.
	ReadScopes []string
	// Unused are scopes the tool offers that Rotor never asks for.
	Unused []string
	// Implied go along with every request, as a scope the tool requires beside any other.
	Implied []string
	// ReadSuffix marks the reading half of a scope: consenting to mcp consents to mcp:read.
	ReadSuffix string
	// SharedGrantToken is a grant with one access token, which a refresh replaces and a revocation ends with the grant,
	// so each run needs a token of its own by exchange.
	SharedGrantToken bool
	// LifetimeParam names the exchange's parameter for how many seconds the token lives, where the tool takes one.
	LifetimeParam string
	// PendingRegistration is a tool that holds a client registering itself until its administrator allows it.
	PendingRegistration bool
}

// Generic is a tool Rotor knows nothing particular of, named as Exciter names its scopes, which the suite's MCP
// servers follow; evidence reaches only CI uploads there.
var Generic = Profile{ReadScopes: []string{"mcp:read"}, Unused: []string{"evidence"}, ReadSuffix: ":read"}

var known = map[string]Profile{
	"exciter": {Product: "exciter", ReadScopes: []string{"mcp:read"}, Unused: []string{"evidence"}, ReadSuffix: ":read"},
	// Armature's scopes are in its docs/oauth.md: read is required with every other, webhooks.partner is the app's own.
	"armature": {Product: "armature", ReadScopes: []string{"read"}, Unused: []string{"webhooks.partner"}, Implied: []string{"read"},
		SharedGrantToken: true, LifetimeParam: "armature_expires_in", PendingRegistration: true},
}

// For is the profile of the product a tool's /meta names, Generic for one Rotor knows nothing particular of.
func For(product string) Profile {
	if p, ok := known[product]; ok {
		return p
	}
	g := Generic
	g.Product = product
	return g
}

// Requested is every scope the tool supports but those Rotor never uses, in the tool's order.
func (p Profile) Requested(supported []string) []string {
	out := []string{}
	for _, scope := range supported {
		if !slices.Contains(p.Unused, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// Covers says whether the consent includes every scope asked for, as minting will check it.
func (p Profile) Covers(consented, asked []string) bool {
	return len(p.Uncovered(consented, asked)) == 0
}

// Uncovered are the scopes asked for that the consent does not include.
func (p Profile) Uncovered(consented, asked []string) []string {
	var out []string
	for _, scope := range asked {
		if !p.covers(consented, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// covers says whether the consent includes scope, itself or as the reading half of a broader scope.
func (p Profile) covers(consented []string, scope string) bool {
	if slices.Contains(consented, scope) {
		return true
	}
	return p.ReadSuffix != "" && strings.HasSuffix(scope, p.ReadSuffix) && slices.Contains(consented, strings.TrimSuffix(scope, p.ReadSuffix))
}

// WithImplied is the scopes asked for and those the tool requires beside them, once each, in that order.
func (p Profile) WithImplied(asked []string) []string {
	out := slices.Clone(asked)
	for _, scope := range p.Implied {
		if !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	return out
}
