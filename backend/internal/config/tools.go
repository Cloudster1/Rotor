package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// How Rotor talks to the tools it connects people to; docs/api-contract-m1.md has the flow.
const (
	// ToolTimeout bounds one call to a tool's metadata, registration, token or revocation endpoint.
	ToolTimeout = 10 * time.Second
	// MaxToolDocumentBytes bounds what Rotor reads of one answer from a tool.
	MaxToolDocumentBytes = 64 << 10
	// MintAttempts bounds how often minting starts over when a reconnect replaced the refresh token meanwhile.
	MintAttempts = 2
	// StateBytes is how much randomness a connection's state carries; only its SHA-256 is stored.
	StateBytes = 32
	// ConnectionLoginTTL is how long a person may take at a tool's consent page before the attempt is forgotten.
	ConnectionLoginTTL = 10 * time.Minute
	// ExchangedTokenLifetime is how long a run's token lives where it is exchanged from the grant's (#45); the
	// runner asks for another before it ends.
	ExchangedTokenLifetime = 15 * time.Minute
	// ConnectionCallbackPath is where a tool returns the browser after consent, under the web client's origin.
	ConnectionCallbackPath = "/api/v1/connections/callback"
)

// Tools is how the api reaches the tools an administrator adds.
type Tools struct {
	// Backchannel maps a tool's public origin to one this process reaches, as public=reachable pairs.
	Backchannel string
	// HooksURL is the base address tools deliver webhooks to; empty until Load falls back to the app URL.
	HooksURL string
}

func loadTools(l *loader) Tools {
	return Tools{
		Backchannel: l.str("ROTOR_TOOLS_BACKCHANNEL", ""),
		HooksURL:    strings.TrimSuffix(l.str("ROTOR_HOOKS_URL", ""), "/"),
	}
}

// checkTools refuses a hooks address no tool could deliver to.
func checkTools(l *loader, c Config) {
	if c.Tools.HooksURL == "" {
		return
	}
	u, err := url.Parse(c.Tools.HooksURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		l.problem(fmt.Sprintf("ROTOR_HOOKS_URL is %q; set it to an absolute http or https address, such as http://rotor-api:8080.", c.Tools.HooksURL))
	}
}

// ConnectionCallbackURL is the one redirect URI Rotor registers at every tool.
func (c Config) ConnectionCallbackURL() string { return c.AppBaseURL + ConnectionCallbackPath }
