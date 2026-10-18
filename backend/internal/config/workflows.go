package config

// Workflow limits; docs/api-contract-m2.md names each.
const (
	// MaxSpecBytes bounds one workflow's YAML.
	MaxSpecBytes = 256 << 10
)
