// Package oidc signs people in through the suite's identity provider and ends their sessions
// when the provider says, through OpenID Connect Back-Channel Logout.
package oidc

import (
	"errors"
	"time"
)

// Fixed parts of the flow; its timings live in package config.
const (
	// randomTokenBytes makes a state and a nonce unguessable.
	randomTokenBytes = 32
	// groupsClaim is the claim the realm's groups mapper writes.
	groupsClaim = "groups"
	// backchannelLogoutEvent marks a JWT as a logout token in its events claim.
	backchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"
)

// scopes are what Rotor asks the provider for: who the person is and their address.
var scopes = []string{"openid", "profile", "email"}

// Errors a sign-in or a logout can end in, each answered in its own sentence.
var (
	// ErrNotConfigured is a process started without ROTOR_OIDC_ISSUER.
	ErrNotConfigured = errors.New("no identity provider is configured")
	// ErrUnreachable is a provider that did not answer discovery.
	ErrUnreachable = errors.New("the identity provider cannot be reached")
	// ErrUnknownLogin is a callback whose state was never issued, already used or expired.
	ErrUnknownLogin = errors.New("this sign-in was not started here, or has expired")
	// ErrNoEmail is a token without an address, which a person is known by across the suite.
	ErrNoEmail = errors.New("the identity provider sent no email address")
	// ErrEmailUnverified is an address the provider says it never checked.
	ErrEmailUnverified = errors.New("the identity provider has not verified this email address")
	// ErrBadLogoutToken is a logout token that fails any check of the specification.
	ErrBadLogoutToken = errors.New("the logout token is not valid")
)

// Identity is what a verified id token said about somebody.
type Identity struct {
	// Issuer and Subject together are the provider's stable name for a person.
	Issuer  string
	Subject string
	Email   string
	Name    string
	Groups  []string
	// SID is the provider's session, which a back-channel logout names.
	SID string
}

// Logout is what a verified logout token asks to end: the provider's session, the person, or both.
type Logout struct {
	Issuer  string
	Subject string
	SID     string
	JTI     string
	Expires time.Time
}
