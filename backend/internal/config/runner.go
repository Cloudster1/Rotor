package config

import "time"

// The runner protocol (#18); docs/api-contract-m2.md describes it.
const (
	DefaultRunnerAddr = ":9443"
	// RunnerHost is the name sandboxes dial and the server certificate names.
	RunnerHost = "api"
	// BootstrapTTL is how long a sandbox's single-use bootstrap token lasts.
	BootstrapTTL = 5 * time.Minute
	// RunnerSessionGrace is how long past a run's own timeout its session token may still reopen the stream.
	RunnerSessionGrace = time.Hour
	// HeartbeatInterval is how often a runner says it is alive; MissedHeartbeats missed in a row lose it.
	HeartbeatInterval = 10 * time.Second
	MissedHeartbeats  = 3
	// RunnerKeepaliveMin is the shortest ping interval the server accepts; runners ping every 5 seconds.
	RunnerKeepaliveMin = 4 * time.Second
	// MaxEventBytes bounds one event's payload; MaxRunnerMessageBytes one message, a checkpoint included.
	MaxEventBytes         = 64 << 10
	MaxRunnerMessageBytes = 8 << 20
	// RunnerCertTTL is the server certificate's life; it is renewed RunnerCertRenewBefore its end.
	RunnerCertTTL         = 72 * time.Hour
	RunnerCertRenewBefore = 24 * time.Hour
)

// Runner is where the control plane serves the runner protocol.
type Runner struct {
	Addr string
}

func loadRunner(l *loader) Runner {
	return Runner{Addr: l.str("ROTOR_RUNNER_ADDR", DefaultRunnerAddr)}
}
