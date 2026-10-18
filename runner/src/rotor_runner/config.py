"""Every tunable value of the runner, named; docs/api-contract-m2.md gives the ones the control plane shares."""

# MAX_SPEC_BYTES bounds one workflow's YAML, as the control plane's config.MaxSpecBytes does.
MAX_SPEC_BYTES = 256 << 10

# SUMMARY_CHARS cuts the arguments and results an event carries.
SUMMARY_CHARS = 2000

# LLM_FLUSH_CHARS is how much of a model's streamed text one llm.output event gathers.
LLM_FLUSH_CHARS = 200

# DURATION_UNITS turns the spec's durations, such as 30s or 15m, into seconds.
DURATION_UNITS = {"s": 1, "m": 60, "h": 3600}

# Code nodes (#17): what a node gets unless it says otherwise, and the most it may ask for.
CODE_DEFAULT_TIMEOUT_S = 30
CODE_MAX_TIMEOUT_S = 300
MEBIBYTE = 1 << 20
CODE_DEFAULT_MEMORY = 256 * MEBIBYTE
CODE_MAX_MEMORY = 1024 * MEBIBYTE
# CODE_OUTPUT_BYTES bounds a code node's result, and CODE_LOG_BYTES what its prints may log.
CODE_OUTPUT_BYTES = 64 << 10
CODE_LOG_BYTES = 64 << 10
# CODE_OPEN_FILES bounds the files a code node may hold open; it needs few, being given its state as JSON.
CODE_OPEN_FILES = 64
# CODE_LINE_BYTES bounds one line of the child's protocol, a result or a tool call included.
CODE_LINE_BYTES = 1 << 20
