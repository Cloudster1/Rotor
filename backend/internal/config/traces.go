package config

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
