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

# The runner protocol (#18), as the control plane's config names it.
HEARTBEAT_INTERVAL_S = 10
# KEEPALIVE_MS is how often the stream is pinged and how long a ping may go unanswered; the server allows it.
KEEPALIVE_MS = 5000
RECONNECT_MIN_S = 0.5
RECONNECT_MAX_S = 10.0
# A token is asked for again this long before it expires, and a grant waited for this long before asking again.
TOKEN_REFRESH_BEFORE_S = 60
TOKEN_WAIT_S = 30
MAX_MESSAGE_BYTES = 8 << 20
# EXIT_REFUSED is the exit code of a runner whose credential the control plane refused.
EXIT_REFUSED = 2

# Wrapped tokens and keys (#23): how long the runner waits for OpenBao, and the shortest text it takes for a secret
# it holds, so a stray short value never blanks ordinary text; what stands in for one is in secret-patterns.json.
UNWRAP_TIMEOUT_S = 10
MIN_SECRET_CHARS = 8
# How the runner's log lines read; each passes the redactor (#41).
LOG_FORMAT = "%(asctime)s %(levelname)s %(message)s"

# Approvals (#31), as the control plane's config names them: the longest an approval may wait, the keys its output
# keeps for itself, and the calls that would decide a review in a tool, which no workflow may bind (Exciter #182).
APPROVAL_MAX_EXPIRY_S = 168 * 3600
RESERVED_FORM_FIELDS = ("approved", "comment", "decidedBy")
REVIEW_DECIDING_CALLS = r"(?i)^(approve|reject|decide|sign|submit)_?\w*review"
# APPROVAL_PREVIEW_ENTRIES bounds the writes one approval lists; the control plane cuts by size too.
APPROVAL_PREVIEW_ENTRIES = 50

# Usage and rate (#38): the tool calls a run may make in a minute unless its limits say, and that minute.
DEFAULT_TOOL_CALLS_PER_MINUTE = 60
TOOL_RATE_WINDOW_S = 60
# Tracing (#37): a run's spans leave over its channel in batches of SPAN_BATCH, or after SPAN_FLUSH_S, each value cut to
# SPAN_ATTR_CHARS as the control plane cuts them; an LLM span shows LangSmith the last SPAN_PROMPT_MESSAGES messages.
SPAN_BATCH = 64
SPAN_FLUSH_S = 2.0
SPAN_ATTR_CHARS = 512
SPAN_PROMPT_MESSAGES = 4
SPAN_FLUSH_TIMEOUT_MS = 30_000
# W3C trace context's sizes: a trace id is 16 bytes, a span id 8.
TRACE_ID_BYTES = 16
SPAN_ID_BYTES = 8

# prctl options from <linux/prctl.h>; the runner clears its dumpable flag so code nodes cannot read it (#89).
PR_GET_DUMPABLE = 3
PR_SET_DUMPABLE = 4
