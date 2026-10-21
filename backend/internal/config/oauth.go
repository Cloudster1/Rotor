package config

import "time"

// Assistants (#47): Rotor as the authorization server of its own MCP endpoint, adapted from Exciter's at 3b340ca;
// docs/api-contract-m10.md describes them.
const (
	// OAuthAccessTTL is short because an access token is sent on every call; the refresh token renews it.
	OAuthAccessTTL = time.Hour
	// OAuthRefreshTTL ends a grant nobody used for that long.
	OAuthRefreshTTL = 30 * 24 * time.Hour
	// OAuthCodeTTL and OAuthRequestTTL bound the redirect and the consent page: long enough to sign in, no longer.
	OAuthCodeTTL    = time.Minute
	OAuthRequestTTL = 10 * time.Minute
	// OAuthUnusedClientTTL removes a registration that never led to a grant.
	OAuthUnusedClientTTL = 24 * time.Hour
	// OAuthMaxRegistrationsPerHour is per caller's address, as registering needs nobody signed in.
	OAuthMaxRegistrationsPerHour = 10
	// OAuthMaxRedirectURIs and OAuthMaxClientName bound a registration; migration 01000 holds the same.
	OAuthMaxRedirectURIs = 10
	OAuthMaxClientName   = 100
	// OAuthMaxFormBytes bounds the token and revocation forms, which carry a few short values.
	OAuthMaxFormBytes = 16 << 10
	// OAuthMaxRegistrationBytes bounds a registration's JSON.
	OAuthMaxRegistrationBytes = 16 << 10
	// OAuthLastUsedEvery is how often a grant's use is noted, so a busy assistant writes once a minute at most.
	OAuthLastUsedEvery = time.Minute
	// OAuthSweepEvery is how often the worker removes what has run out; nothing waits on it, since every read
	// checks expiry.
	OAuthSweepEvery = 10 * time.Minute
	// MCPMaxToolResultBytes bounds what one tool call answers, so a list too long to read is narrowed instead.
	MCPMaxToolResultBytes = 64 << 10
	// MCPMaxRequestBytes bounds one JSON-RPC message.
	MCPMaxRequestBytes = 1 << 20
	// OAuthMaxClientMetadataBytes bounds a client ID metadata document fetched from its address (#114).
	OAuthMaxClientMetadataBytes = 5 << 10
	// OAuthMetadataFetchTimeout bounds fetching a document, which a person waits for at the consent page.
	OAuthMetadataFetchTimeout = 5 * time.Second
	// OAuthMaxMetadataAge is the longest a document is used without fetching it again, whatever its cache headers.
	OAuthMaxMetadataAge = 24 * time.Hour
)

// The outbound guard of netguard, for addresses somebody else chose (#114).
const (
	// GuardDialTimeout bounds one connection attempt and its TLS handshake; the client's own timeout bounds the call.
	GuardDialTimeout = 5 * time.Second
	GuardKeepAlive   = 30 * time.Second
	GuardIdleTimeout = 30 * time.Second
	// GuardMaxIdleConns keeps a few connections for documents fetched again.
	GuardMaxIdleConns = 20
	// GuardMaxRedirects bounds the walk; each redirect is checked like the first address.
	GuardMaxRedirects = 3
)
