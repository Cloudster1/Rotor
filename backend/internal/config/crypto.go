package config

import "time"

// Run data at rest (#41): every run has a data key of its own, made and wrapped by OpenBao transit, and its data is
// sealed with it in the control plane; docs/api-contract-m8.md describes the format.
const (
	// RunDataTransitKey is the installation's transit key, which wraps every run's data key and never leaves OpenBao.
	RunDataTransitKey = "rotor-run-data"
	// DataKeyBits is the length of a run's data key, for AES-256-GCM.
	DataKeyBits = 256
	// DataKeyCacheTTL is how long an unwrapped data key stays in memory after it was last used.
	DataKeyCacheTTL = 5 * time.Minute
	// DataKeyCacheEntries bounds the data keys held in memory at once; the least recently used goes first.
	DataKeyCacheEntries = 4096
)
