package config

import "time"

// What one span may hold (#241), whoever sent it; docs/api-contract-m18.md gives the reasons and the runner's config.py
// applies the same limits first.
const (
	// MaxSpanNameChars bounds a span's name and an attribute's key.
	MaxSpanNameChars = 128
	// MaxSpanAttributes bounds the attributes one span keeps: Rotor's first, then those without text, then text.
	MaxSpanAttributes = 128
	// MaxSpanAttrChars bounds an attribute that carries no text, as a database statement is cut in its span.
	MaxSpanAttrChars = 512
	// MaxSpanContentChars bounds an input, an output or a message: a tool's answer reads, a trace stays small.
	MaxSpanContentChars = 16384
	// MaxSpanStackChars bounds an exception's stack, which shows where a run failed.
	MaxSpanStackChars = 4096
	// MaxSpanEvents bounds a span's events, its exceptions and retries; the latest are kept.
	MaxSpanEvents = 16
	// MaxSpanBytes bounds one span's name, attributes and events; its text goes first, longest first, to fit.
	MaxSpanBytes = 64 << 10
	// MaxRunnerSpansPerBatch bounds the spans one message of a runner carries; more are dropped.
	MaxRunnerSpansPerBatch = 256
)

// What Rotor keeps of a trace in Postgres, and for how long (#242). Migration 01500 writes the same numbers into its
// trigger and checks, and a test holds them to these.
const (
	// MaxTraceSpans bounds a trace's spans: hundreds of agent steps, each with its model and tool calls.
	MaxTraceSpans = 10000
	// MaxTraceBytes bounds a trace's stored bytes, text included: what 14 days of a busy installation may hold.
	MaxTraceBytes = 32 << 20
	// MaxSenderSpans bounds the spans one sender keeps across its own traces, which no run bounds.
	MaxSenderSpans = 100000
	// MaxTraceStoreWrites bounds the runner batches being stored at once, apart from the exports, so a slow
	// destination never costs the store; a batch beyond them is dropped.
	MaxTraceStoreWrites = 32
	// TraceStoreTimeout bounds storing one batch, as RunnerSpanExportTimeout bounds sending it.
	TraceStoreTimeout = 10 * time.Second
	// DefaultTraceRetentionDays is how long traces are kept until an administrator says otherwise.
	DefaultTraceRetentionDays = 14
	// MaxTraceRetentionDays bounds the retention: Postgres keeps traces for debugging, a destination is the archive.
	MaxTraceRetentionDays = 90
	// TraceSweepBatch bounds the spans one transaction of the sweep deletes; a full batch is followed by another.
	TraceSweepBatch = 5000
	// TraceSweepMaxBatches bounds the batches of one sweep, so a backlog never holds up the worker's other passes.
	TraceSweepMaxBatches = 20
	// TraceSweepEvery is how often the worker looks for traces past their retention.
	TraceSweepEvery = time.Hour
	// TraceSweepLockKey is the advisory lock one worker holds while it sweeps traces, "rotor" and the issue in hex.
	TraceSweepLockKey = 0x726f746f72_0242
	// TraceSettingTTL is how long a replica trusts what it read of the trace setting and LangSmith's; one changed on
	// another replica takes at most this long to reach this one.
	TraceSettingTTL = 30 * time.Second
)

// What the OTLP endpoint takes from one sender (#243); docs/api-contract-m18.md gives the reasons.
const (
	// MaxOTLPBodyBytes bounds one request as sent: four of the OpenTelemetry SDK's default 512-span batches.
	MaxOTLPBodyBytes = 4 << 20
	// MaxOTLPDecodedBytes bounds a request after gzip, so a small body cannot unpack into a large one.
	MaxOTLPDecodedBytes = 16 << 20
	// MaxOTLPSpansPerRequest bounds the spans taken from one request; those past it are rejected.
	MaxOTLPSpansPerRequest = 2048
	// OTLPRequestsPerMinute and OTLPSpansPerMinute bound one sender at one api replica, counted in its memory.
	OTLPRequestsPerMinute = 300
	OTLPSpansPerMinute    = 20000
	// OTLPRateWindow is the window both rates are counted over; Retry-After says how long until it ends.
	OTLPRateWindow = time.Minute
)
