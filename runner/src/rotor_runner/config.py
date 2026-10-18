"""Every tunable value of the runner, named; docs/api-contract-m2.md gives the ones the control plane shares."""

# MAX_SPEC_BYTES bounds one workflow's YAML, as the control plane's config.MaxSpecBytes does.
MAX_SPEC_BYTES = 256 << 10

# SUMMARY_CHARS cuts the arguments and results an event carries.
SUMMARY_CHARS = 2000

# LLM_FLUSH_CHARS is how much of a model's streamed text one llm.output event gathers.
LLM_FLUSH_CHARS = 200

# DURATION_UNITS turns the spec's durations, such as 30s or 15m, into seconds.
DURATION_UNITS = {"s": 1, "m": 60, "h": 3600}
