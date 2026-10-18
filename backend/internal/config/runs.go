package config

import "time"

// Worker is the run worker's own settings.
type Worker struct {
	// MetricsAddr is where the worker serves /metrics (#28); empty serves none.
	MetricsAddr string
}

// Runs and the worker that carries them (#22); docs/api-contract-m2.md describes their states.
const (
	// DefaultWorkerMetricsAddr is where the worker serves /metrics, on the stack's network alone.
	DefaultWorkerMetricsAddr = ":9464"
	// WorkerTick is how often the worker looks for runs to start, check and end.
	WorkerTick = 2 * time.Second
	// WorkerLease is how long a worker holds a run it is acting on; one that dies delays the run this long at most.
	WorkerLease = time.Minute
	// WorkerAliveFile is touched after every pass; the container's health check reads how fresh it is.
	WorkerAliveFile = "/tmp/rotor-worker-alive"
	// WorkerAliveWithin is how recent the last pass must be for the worker to count as healthy.
	WorkerAliveWithin = 30 * time.Second
	// WorkerIDBytes is the randomness that tells two workers on one host apart in a run's lease.
	WorkerIDBytes = 4
	// WorkerBatch bounds the runs one tick claims, so one worker never takes every queued run.
	WorkerBatch = 10
	// StartTimeout is how long a started sandbox has to say Hello before it is replaced.
	StartTimeout = 2 * time.Minute
	// LostGrace is how long a runner that missed its heartbeats has to come back before its sandbox is replaced.
	LostGrace = 2 * time.Minute
	// MaxRunAttempts bounds the sandboxes one run is started in; a lost one resumes from its last checkpoint.
	MaxRunAttempts = 3
	// RunDeadlineGrace is how long past its own timeout a run may take before the worker ends it.
	RunDeadlineGrace = time.Minute
	// WaitingTTL is how long a run may wait for an answer before it expires.
	WaitingTTL = 7 * 24 * time.Hour
	// MaxIdempotencyKey bounds the Idempotency-Key a client sends.
	MaxIdempotencyKey = 200
	// MaxRunInputBytes bounds the inputs a run starts with.
	MaxRunInputBytes = 256 << 10
	// RunListLimit bounds the runs one list answers.
	RunListLimit = 200
	// WrapTTL is how long a wrapped token or key waits for its sandbox to unwrap it, once.
	WrapTTL = 2 * time.Minute
	// SignalBuffer is how many pub/sub signals a subscriber may fall behind by; more are dropped, since a signal only
	// says "look again" and the next one says the same.
	SignalBuffer = 16
)

// Following a run's events over server-sent events (#24).
const (
	// EventHeartbeat is how often a quiet stream sends a comment, so proxies and browsers keep it open.
	EventHeartbeat = 15 * time.Second
	// EventPoll is how often a stream reads new events without a signal, in case one was lost.
	EventPoll = 2 * time.Second
	// EventBatch bounds the events one read answers; a longer run is read in several.
	EventBatch = 500
	// EventRetryMillis is how long a browser waits before it reconnects a dropped stream.
	EventRetryMillis = 2000
)
