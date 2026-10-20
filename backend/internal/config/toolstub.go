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
	// ToolStubMaxOTLPBytes bounds one export the LangSmith stand-in takes, and ToolStubMaxOTLPCalls how many it keeps.
	ToolStubMaxOTLPBytes = 4 << 20
	ToolStubMaxOTLPCalls = 200
)
