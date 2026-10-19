package config

import "time"

// Retention (#41): how long a finished run's data is kept, per installation and per workflow, and the pass that
// deletes it; docs/api-contract-m8.md describes them.
const (
	// DefaultRunDataRetentionDays is the installation's retention until an administrator sets one; migration 00801
	// gives the same default.
	DefaultRunDataRetentionDays = 90
	// MaxRunDataRetentionDays bounds a retention, the installation's or a workflow's: ten years.
	MaxRunDataRetentionDays = 3650
	// RetentionSweepEvery is how often the worker looks for runs past their retention.
	RetentionSweepEvery = time.Hour
	// RetentionBatch bounds the runs one sweep deletes in one transaction; a full batch is followed by another.
	RetentionBatch = 500
	// RetentionMaxBatches bounds the batches of one sweep, so a backlog never holds up the worker's other passes.
	RetentionMaxBatches = 20
	// RetentionLockKey is the advisory lock one worker holds while it deletes, "rotor" and the milestone in hex.
	RetentionLockKey = 0x726f746f72_0800
)
