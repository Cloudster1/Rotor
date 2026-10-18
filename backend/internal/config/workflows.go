package config

import "time"

// Workflow limits; docs/api-contract-m2.md names each.
const (
	// MaxSpecBytes bounds one workflow's YAML.
	MaxSpecBytes = 256 << 10
	// MaxCodeTimeout and MaxCodeMemory are the most a code node may ask for, as the runner enforces.
	MaxCodeTimeout = 5 * time.Minute
	MaxCodeMemory  = 1 << 30
)
