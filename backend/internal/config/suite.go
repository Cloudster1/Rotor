package config

import "time"

// SuiteSeedCallTimeout bounds one call of the suite's seed (#130); the siblings answer on the same host, so a call
// slower than this is stuck.
const SuiteSeedCallTimeout = 30 * time.Second
