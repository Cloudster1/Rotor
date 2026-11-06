package config

import "time"

// The tool test double's lifetimes and limits (#8), Exciter's where it has them (backend/internal/oauth/oauth.go).
const (
	// ToolStubAccessTTL is a stub tenant's access token lifetime unless the tenant asks for another.
	ToolStubAccessTTL = time.Hour
	// ToolStubRefreshTTL ends a grant nobody refreshed for that long.
	ToolStubRefreshTTL = 30 * 24 * time.Hour
	// ToolStubCodeTTL and ToolStubRequestTTL bound the redirect and the consent page.
	ToolStubCodeTTL    = time.Minute
	ToolStubRequestTTL = 10 * time.Minute
	// ToolStubMaxExchangedTTL is the longest an Armature tenant's exchanged token lives, as at Armature (#45).
	ToolStubMaxExchangedTTL = time.Hour
	// ToolStubMaxTenants bounds what callers can make the stub remember, as tenants are made on first use.
	ToolStubMaxTenants = 1000
	// ToolStubMaxFormBytes bounds a token, revocation, registration or consent request.
	ToolStubMaxFormBytes = 16 << 10
	// ToolStubMaxMCPBytes bounds one JSON-RPC request.
	ToolStubMaxMCPBytes = 1 << 20
	// ToolStubMaxRedirectURIs and ToolStubMaxClientName are Exciter's registration limits.
	ToolStubMaxRedirectURIs = 10
	ToolStubMaxClientName   = 100
	// ToolStubSecretBytes is the randomness in every token, code and client id the stub hands out.
	ToolStubSecretBytes = 32
	// ToolStubAddr serves the tools; ToolStubControlAddr the control API, which the stack never publishes.
	ToolStubAddr        = ":8080"
	ToolStubControlAddr = ":8081"
	// ToolStubMaxLLMDelay bounds how long a scripted model answer may be held back.
	ToolStubMaxLLMDelay = time.Minute
	// ToolStubPromptTokens and ToolStubCompletionTokens are the usage every stub model answer reports.
	ToolStubPromptTokens     = 10
	ToolStubCompletionTokens = 5
	// ToolStubMaxLLMRequests is how many chat requests a tenant keeps for tests to read, the oldest dropped (#222).
	ToolStubMaxLLMRequests = 50
	// ToolStubMaxOTLPBytes bounds one export an OTLP sink takes, ToolStubMaxOTLPCalls how
	// many each keeps, and ToolStubMaxOTLPSinks how many sinks are kept before all are forgotten (#246).
	ToolStubMaxOTLPBytes = 4 << 20
	ToolStubMaxOTLPCalls = 200
	ToolStubMaxOTLPSinks = 64
	// ToolStubMaxPushBytes is the largest push Armature's git proxy forwards, and ToolStubMaxObjectBytes the largest
	// object the stand-in inflates, so a hostile pack cannot fill the stub's memory (#137).
	ToolStubMaxPushBytes   = 32 << 20
	ToolStubMaxObjectBytes = 16 << 20
	// ToolStubLabelPage and ToolStubMaxLabelPage are Armature's pages of labels, ToolStubMaxBulkSelection the most
	// issues one bulk change takes and ToolStubMaxLabelName the longest label name, in runes (#270).
	ToolStubLabelPage        = 50
	ToolStubMaxLabelPage     = 200
	ToolStubMaxBulkSelection = 200
	ToolStubMaxLabelName     = 40
	// ToolStubIssuePage and ToolStubMaxIssuePage are Armature's pages of a project's issues (#271).
	ToolStubIssuePage    = 50
	ToolStubMaxIssuePage = 200
)
