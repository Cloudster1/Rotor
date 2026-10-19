package config

import (
	"net/url"
	"time"
)

// Workflow limits; docs/api-contract-m2.md names each.
const (
	// MaxSpecBytes bounds one workflow's YAML.
	MaxSpecBytes = 256 << 10
	// MaxCodeTimeout and MaxCodeMemory are the most a code node may ask for, as the runner enforces.
	MaxCodeTimeout = 5 * time.Minute
	MaxCodeMemory  = 1 << 30
	// MaxVersionComment bounds the note a save carries, as the table does.
	MaxVersionComment = 500
	// MaxSharedGroups bounds how many realm groups a workflow is shared with.
	MaxSharedGroups = 50
	// CatalogTTL is how long a tool's MCP tool list is kept, per tool build and person.
	CatalogTTL = time.Hour
	// MaxCatalogPages bounds how many pages of tools/list Rotor follows for one tool.
	MaxCatalogPages = 20
	// MaxCatalogBytes bounds one tools/list answer.
	MaxCatalogBytes = 4 << 20
	// DiffContextLines is how many unchanged lines a diff hunk shows around a change.
	DiffContextLines = 3
	// MaxDiffCells bounds the line alignment of two versions, beyond which a diff replaces everything.
	MaxDiffCells = 4_000_000
	// MCPProtocolVersion is the MCP protocol Rotor speaks to the tools when it reads their catalogs.
	MCPProtocolVersion = "2025-06-18"
)

// Valkey caches what can be fetched again, such as the tools' catalogs; nothing in it must survive.
type Valkey struct {
	// URL is redis://[:password@]host:port/db; empty runs without a cache, in development only.
	URL string
}

func loadValkey(l *loader) Valkey {
	return Valkey{URL: l.raw("ROTOR_VALKEY_URL")}
}

func checkValkey(l *loader, c Config) {
	if c.Env == EnvDevelopment {
		return
	}
	if c.Valkey.URL == "" {
		l.problem("Set ROTOR_VALKEY_URL to Valkey's address, such as redis://:password@valkey:6379/0; Rotor caches the tools' catalogs there.")
		return
	}
	// Anyone who reaches a Valkey without a password could read the catalogs and wake every replica (#92).
	if u, err := url.Parse(c.Valkey.URL); err == nil {
		if _, set := u.User.Password(); !set {
			l.problem("ROTOR_VALKEY_URL names no password; give Valkey one and put it in the address, as redis://:password@valkey:6379/0.")
		}
	}
}
