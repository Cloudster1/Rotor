package config

import "time"

// The runner protocol (#18); docs/api-contract-m2.md describes it.
const (
	DefaultRunnerAddr = ":9443"
	// RunnerHost is the name sandboxes dial and the server certificate names, unless ROTOR_RUNNER_HOST says another,
	// such as the api's Service in a cluster.
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

// Runner is where the control plane serves the runner protocol, and where sandboxes dial it.
type Runner struct {
	Addr string
	// Host is the name the control plane's certificate carries; Dial is the address a sandbox dials, which must
	// name Host. The worker hands Dial to the sandboxes.
	Host string
	Dial string
}

func loadRunner(l *loader) Runner {
	host := l.str("ROTOR_RUNNER_HOST", RunnerHost)
	return Runner{Addr: l.str("ROTOR_RUNNER_ADDR", DefaultRunnerAddr), Host: host, Dial: l.str("ROTOR_RUNNER_DIAL", host+DefaultRunnerAddr)}
}

// MaxCheckpointBytes bounds one checkpoint, its metadata included, before encryption (#19).
const MaxCheckpointBytes = 4 << 20
