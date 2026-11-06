# API contract for M18: run tracing

M18 makes a run's trace something to debug with. The runner traces every chain, LangGraph node, tool
and model call with OpenInference; Rotor keeps each run's spans in Postgres and shows them on the
run's page; a LangChain app or a tool service sends its own spans to Rotor over OTLP; and
administrators send runs' traces on to the OTLP tools their teams use.
- a span for every chain, node, tool and model call, with inputs and outputs where allowed (#241)
- each run's trace kept in Rotor, read by whoever reads the run (#242)
- an OTLP/HTTP endpoint any LangChain app can send spans to (#243)
- a run's trace through the API and MCP (#244)
- the trace on the run's page (#245)
- OTLP destinations, the LangSmith setting carried over as one (#246)

This file is #240. It builds on `docs/api-contract-m7-telemetry.md` (#37), whose run trace, runner
span messages and checks stay unless this file says otherwise. The route table in
`backend/internal/httpapi/operations_m18.go` is the source of truth for the HTTP operations and
`proto/rotor/runner/v1/runner.proto` for the runner protocol; the new routes carry `pending: true`
and answer 501 until the issue that builds them merges.

Conventions are M2's: paths under `/api/v1`; refusals are sentences saying what to do; a page of a
listing takes `limit` (50, at most 200) and `offset` and answers `total`. Trace ids are 32 lowercase
hex characters and span ids 16, as W3C trace context writes them.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | What |
  |---|---|---|
  | 01500-01519 | #242 | `trace`, `trace_span`, `trace_setting`, `run.trace_id`, the policies and the `trace_span_cap` trigger |
  | 01520-01529 | #246 | `trace_destination`, and the LangSmith setting carried over into it |
  | 01530-01539 | #243 | anything the endpoint needs beyond #242, if any |

- Packages: `internal/traces` (the store, the cap, sealing, the tree and the sweep; #242, #244),
  `internal/traces/otlp` (decoding OTLP, #243), `internal/tracedest` (destinations, replacing
  `internal/langsmith`; #246), `internal/runnerctl` and `internal/observability` (checking and
  fanning out spans), the runner's `telemetry.py` (#241), `web/src/features/runs/trace/` (#245) and
  `web/src/features/settings/TraceDestinationsSection.tsx` (#246).
- `api/trace-content.json` (#241) lists the attribute keys and prefixes that carry text a person, a
  tool or a model wrote. Go and Python both read it, and a test on each side refuses a copy that
  differs, as for `api/secret-patterns.json`.
- Settings, `ROTOR_` variables: none new. `ROTOR_OTLP_ENDPOINT` and `ROTOR_TRACE_SAMPLE_RATIO` keep
  their meaning for the operator's collector. `ROTOR_LANGSMITH_ENDPOINT` is read only to carry
  LangSmith over (#246) and goes when `langsmith_setting` is dropped.

### Limits

In `internal/config/traces.go`, mirrored in the runner's `config.py` where it applies them first.
Each replaces or joins #37's.

| Constant | Value | Why |
|---|---|---|
| `MaxSpanNameChars` | 128 | unchanged |
| `MaxSpanAttributes` | 128, was `MaxRunnerSpanAttributes` = 32 | OpenInference spreads one model call over many keys; non-text keys are kept first, then text, each in key order |
| `MaxSpanAttrChars` | 512 | unchanged, for attributes that carry no text |
| `MaxSpanContentChars` | 16384 | an input, output or message: long enough to read a tool's answer, short enough that a trace stays small |
| `MaxSpanStackChars` | 4096 | `exception.stacktrace`, which shows where a run failed |
| `MaxSpanEvents` | 16 | exceptions and retries; more are dropped |
| `MaxSpanBytes` | 64 KiB | one span's name, attributes and events; text attributes are dropped, longest first, until it fits, and `rotor.content_cut` says so |
| `MaxRunnerSpansPerBatch` | 256 | unchanged |
| `MaxTraceSpans` | 10000 | a trace's spans: hundreds of agent steps, each with its model and tool calls |
| `MaxTraceBytes` | 32 MiB | a trace's stored bytes, text included: what 14 days of a busy installation's runs may hold in Postgres |
| `MaxSenderSpans` | 100000 | spans one sender keeps across its own traces, which belong to no run that bounds them |
| `MaxOTLPBodyBytes` | 4 MiB | one request as sent: four of the OpenTelemetry SDK's default 512-span batches |
| `MaxOTLPDecodedBytes` | 16 MiB | the same after gzip, so a small body cannot unpack into a large one |
| `MaxOTLPSpansPerRequest` | 2048 | spans past it in one request are not taken |
| `OTLPRequestsPerMinute` | 300 | per sender, per api replica |
| `OTLPSpansPerMinute` | 20000 | per sender, per api replica |
| `MaxTraceStoreWrites` | 32 | span batches being stored at once, apart from `MaxRunnerSpanExports`, so a slow destination never costs the store |
| `TraceStoreTimeout` | 10 s | storing one batch, as `RunnerSpanExportTimeout` bounds sending it |
| `DefaultTraceRetentionDays` | 14 | the user's choice; an administrator changes it |
| `MaxTraceRetentionDays` | 90 | Postgres keeps traces for debugging; a destination is the archive |
| `TraceSweepBatch`, `TraceSweepMaxBatches` | 5000, 20 | as `RetentionBatch` and `RetentionMaxBatches` bound the run sweep |
| `TracePreviewChars` | 200 | an input or output in the tree; the span route answers it whole |
| `MaxTraceDestinations` | 8 | each is one more export per batch |
| `MaxDestinationNameChars` | 64 | |
| `MaxDestinationHeaders`, `MaxDestinationHeaderChars` | 16, 4096 | headers carry keys and basic credentials |
| `MaxDestinationURLChars` | 2048 | the endpoint and the trace link |
| `TraceSettingTTL` | 30 s, was `LangSmithSettingTTL` | how long a replica trusts what it read of the setting and the destinations |
| `MCPTraceMaxSpans` | 300 | spans in `get_run_trace`'s answer |
| `MCPTraceValueChars` | 1000 | an input or output in `get_run_trace` |
| `MCPTraceBudgetBytes` | 48 KiB | where `get_run_trace` stops adding values, under `MCPMaxToolResultBytes` |

The runner adds `SPAN_BATCH_BYTES` = 2 MiB: it sends a batch at 64 spans, 2 MiB or 2 seconds,
whichever comes first, so a batch of large spans stays under `MaxRunnerMessageBytes`.

## The spans a run makes (#241)

- **Instrumentation:** the runner calls `openinference-instrumentation-langchain`'s
  `LangChainInstrumentor().instrument(tracer_provider=...)` with the run's own tracer provider, in
  place of `LlmSpans`. One sandbox runs one run, so the instrumentor's process-wide hooks see that
  run alone. Without a sampled traceparent it is not installed and no span is made, as today. The
  dependency is the newest release at least 14 days old when #241 is built, pinned exactly, with its
  `openinference-semantic-conventions` and `openinference-instrumentation` pins.
- **The tree:** `run` (the root, a child of the run's traceparent), `node <id>` for each node,
  `mcp <call>` for each MCP call, and under them OpenInference's spans for every chain, LangGraph
  node, agent, tool and model call, nested as LangChain's callbacks nest them. Rotor's own spans
  carry an `openinference.span.kind` too: `run` and `node <id>` are `CHAIN` (an agent node's is
  `AGENT`), `mcp <call>` is `TOOL`.
- **On every span:** `rotor.run` and `rotor.workflow` (ids), set by a span processor as each span
  starts. The control plane sets both again from the run, and drops every other `rotor.*` key a span
  brings that Rotor does not write, since the sandbox runs the author's code.
- **Model spans** keep `gen_ai.request.model`, `gen_ai.usage.input_tokens` and
  `gen_ai.usage.output_tokens`: the runner copies them from `llm.model_name`,
  `llm.token_count.prompt` and `llm.token_count.completion` when it turns a span into a
  `RunnerSpan`, since a finished span cannot be changed.
- **Attributes kept without text** (cut to `MaxSpanAttrChars`): `openinference.span.kind`,
  `llm.model_name`, `llm.provider`, `llm.system`, `llm.invocation_parameters`, `llm.token_count.*`,
  `tool.name`, `tool.description`, `tool_call.id`, `input.mime_type`, `output.mime_type`, `metadata`
  (LangGraph's node, step and checkpoint names), `session.id`, the `gen_ai.*` copies above,
  `exception.type` and `exception.message` on events, and #37's `rotor.*` and `mcp.tool`.
- **Text, recorded only when the run may keep it** (`api/trace-content.json`; cut to
  `MaxSpanContentChars`): `input.value`, `output.value`, `llm.input_messages.*`,
  `llm.output_messages.*`, `llm.prompts.*`, `llm.prompt_template.*`, `llm.tools.*`,
  `tool.parameters`, `tool_call.function.arguments`, `message.*`, `retrieval.documents.*`,
  `reranker.*`, `embedding.embeddings.*`, `document.*`, and #37's `gen_ai.prompt.*` and
  `gen_ai.completion.*`. A key the list does not know is kept as an attribute without text only when
  it starts with `openinference.`, `llm.`, `gen_ai.`, `tool.`, `rotor.`, `mcp.`, `session.` or
  `exception.`; any other key is treated as text.
- **Never recorded:** images and embedding vectors. The instrumentor's `TraceConfig` hides them
  always, and hides inputs, outputs, messages and their text while the run's `trace_content` is off;
  the runner drops what it hid rather than send OpenInference's placeholder.
- **Redaction:** the runner scrubs every name, attribute, event attribute and status message with
  `api/secret-patterns.json`, cuts it to its limit, and keeps at most the last 16 messages of a
  model call's `llm.input_messages.*`, saying how many it left out in `rotor.messages_omitted`. The
  control plane checks and redacts every span again, the runner's and the endpoint's alike, before
  it stores or sends it.
- **Exceptions:** span events. `exception.message` and `exception.stacktrace` are kept whether or
  not text is, redacted, the stack cut to `MaxSpanStackChars`, so a failure shows even with text
  off. A message may quote an input; the threat model accepts that.

### Runner messages

- `RunnerSpan` gains `repeated RunnerSpanEvent events = 10`, with
  `RunnerSpanEvent { string name = 1; google.protobuf.Timestamp time = 2; map<string, string> attributes = 3; }`.
  Attributes stay strings, as #37 sends them.
- `StartRun.trace_content` (10) keeps its number and now means: the run may keep text somewhere. It
  is set when Rotor keeps text (`trace_setting.keep_content`, #242) or an enabled destination
  receives text (#246; until then, while LangSmith is on, as today). Whatever the run recorded, the
  control plane strips the text from each place that may not have it: the store while `keep_content`
  is off, each destination without its acknowledgement, and the operator's collector always.

### As built (#241)

- **Pins:** `openinference-instrumentation-langchain` 0.1.79 (2026-10-09),
  `openinference-instrumentation` 0.1.71 (2026-10-07) and `openinference-semantic-conventions`
  0.1.41 (2026-10-01). Of what they pull in, `opentelemetry-instrumentation` is 0.66b1
  (2026-10-06) and `wrapt` is held at 2.5.1 (2026-10-14), since 2.5.2 was under two weeks old.
- **The run's own handler, not the instrumentor:** `LangChainInstrumentor().instrument()` patches
  every callback manager of the process and keeps one tracer, so two runs in one process, as in the
  runner's tests, would send each other's spans. The runner makes the handler the instrumentor would
  install, `OpenInferenceTracer`, on the run's tracer provider and passes it in the run's callbacks,
  as `LlmSpans` was passed. The class sits in the package's `_tracer` module; the exact pin and the
  runner's tests catch a release that moves it.
- **One span per node:** OpenInference already makes a span for each LangGraph node, so `node <id>`
  is that span, renamed and given `rotor.node` and `rotor.node.type`, rather than a second span
  beside it; where no LangChain run is current, Rotor makes its own. Rotor's spans start under the
  innermost LangChain run's span, so `mcp <call>` nests in the tool call that made it, or in the
  tool node. A run's tree reads: `run` (CHAIN) > `LangGraph` (CHAIN) > `node <id>` (AGENT or
  CHAIN) > the agent's `LangGraph` > `model` (CHAIN) > the model class, such as `ChatOpenAI`
  (LLM), and `tools` (CHAIN) > the tool, such as `whoami` (TOOL) > `mcp whoami` (TOOL).
- **`rotor.workflow`:** `StartRun` carries no workflow id, so the runner stamps `rotor.run` alone
  and the control plane sets both from the run. Besides them it keeps the `rotor.*` keys the runner
  writes: `rotor.node`, `rotor.node.type`, `rotor.binding`, `rotor.interrupted`,
  `rotor.messages_omitted` and `rotor.content_cut`.
- **`gen_ai.request.model`:** a model call without `llm.model_name`, such as a test's fake model,
  takes the span's name, the model's class.
- **`api/trace-content.json`:** `text`, `plain` and `never` lists of names, a name covering the key
  itself and every key below it after a dot; `make trace-content` copies it. `tool_call.id`,
  `input.mime_type`, `output.mime_type` and `metadata` are plain names; `never` holds `image` and
  `embedding.vector`, matched as whole segments anywhere in a key.
- **Limits:** the span limits live in `internal/config/traces.go`, `MaxRunnerSpanAttributes` renamed
  `MaxSpanAttributes`; #242 onwards add theirs there. Over `MaxSpanBytes` a span loses its text,
  longest first, then its oldest events, then its longest other attributes but Rotor's;
  `rotor.content_cut` is `true` and comes on top of `MaxSpanAttributes`. Rotor's own keys come before
  the other attributes without text when `MaxSpanAttributes` cuts, so the run and the node stay.
- **Events:** the latest `MaxSpanEvents` are kept; an exception LangGraph raises for its control flow,
  such as an approval's `GraphInterrupt`, is dropped, as it is no failure. The status message is
  LangChain's error up to its stack, which the exception event carries.
- **Messages:** the last 16 of `llm.input_messages.*` are numbered from 0 again.
- **Destinations until #242 and #246:** the collector gets every span without its text and LangSmith
  gets it with its text; `trace_content` is still set while LangSmith is on.

## Keeping traces (#242)

### Tables

- `run.trace_id`: `text GENERATED ALWAYS AS (substr(traceparent, 4, 32)) STORED`, indexed, so a
  span's trace id finds its run.
- `trace`: `id` (uuidv7), `trace_id`, `run_id` (cascades with the run) or `owner_id` (the sender,
  for a trace of no run; cascades with the account), exactly one of the two; unique `trace_id` among
  run traces and `(owner_id, trace_id)` among owned ones. `data_key` (an owned trace's, made by
  OpenBao transit as a run's is; a run's trace seals with `run.data_key`), `spans`, `bytes`,
  `dropped`, `cut_at`, `content` (whether any text was kept), `started_at` (its first span taken),
  `last_span_at`, `expired_at`.
- `trace_span`: `(trace, span_id)` primary key, `parent_span_id` (empty for none), `name`, `kind`
  (OpenInference's kinds or `UNKNOWN`), `service` (`rotor-runner`, or the sender's `service.name`),
  `source` (`runner` or `otlp`), `sender_id` and `sender_name` (null for the runner), `started_at`,
  `ended_at`, `status` (`unset`, `ok`, `error`), `status_message`, `attributes jsonb` (without
  text), `events jsonb`, `sealed_content bytea` (the text attributes as one JSON object, sealed;
  null when none was kept), `bytes`, `received_at`. Immutable: `rotor_app` holds no `UPDATE` or
  `DELETE`, and `rotor_admin` deletes only through the sweep.
- `trace_span_cap`, a `SECURITY DEFINER` trigger before each insert into `trace_span`, computes
  `bytes` from the row itself, adds it to its trace, and skips a span past `MaxTraceSpans`, past
  `MaxTraceBytes`, or past `MaxSenderSpans` for an owned trace, counting it in `dropped` and setting
  `cut_at` once. The cap then holds whoever inserts; a Go test holds the SQL numbers to the
  constants. A span id already in the trace is skipped too.
- `trace_setting`, one row: `retention_days` (1 to `MaxTraceRetentionDays`, default 14),
  `keep_content` (default false), `changed_by`, `updated_at`.
- Text at rest is sealed as #41 seals run data: with the run's key, or the owned trace's, bound to
  the trace, the span id and the column. Deleting the run deletes its key and its trace.

### Who reads what

- A trace of a run is read exactly when the run's events are: `trace_read` asks
  `EXISTS (SELECT 1 FROM run r WHERE r.id = trace.run_id)`, as `run_event_read` does, so
  `run_visible` decides both. A sender's own trace is read by its sender and by administrators of
  Rotor. `trace_span` is read when its trace is. Each is tried straight through SQL as `rotor_app`
  in the tests.
- `rotor_app` inserts a `trace` only for a run it reads (`owner_id` null) or as its own
  (`owner_id = current_actor_id()`, `run_id` null), and a `trace_span` only with `source = 'otlp'`
  and `sender_id = current_actor_id()`, into a trace it reads that is a run's or its own. The
  runner's spans are written through `rotor_admin` after the runner protocol's checks.
- `trace_setting` is read by everyone signed in, so the run page can say how long traces are kept,
  and changed by administrators alone.

### Every run is traced

A run's trace is what Rotor keeps, so every run gets one. When the request that starts a run carries
no sampled trace, or the api has no collector, `run.traceparent` is a fresh sampled context of the
run's own (new trace id, random span id, flags `01`), and `ROTOR_TRACE_SAMPLE_RATIO` no longer
decides whether a run has spans. The operator's collector still gets a run's runner spans only when
`TraceIDRatioBased(ROTOR_TRACE_SAMPLE_RATIO)` keeps the trace id, so its volume stays as the ratio
says. Destinations get every run.

### Writing and the sweep

- The control plane stores each checked runner batch beside exporting it, on its own bounded pool
  (`MaxTraceStoreWrites`, `TraceStoreTimeout`): the stream never waits, and a batch that finds the
  pool full is dropped and logged at debug level, as #234 drops an export.
- The worker sweeps beside the run retention sweep: spans of a trace that has taken none for
  `retention_days` are deleted; a run's trace keeps its row with `expired_at` set, so the API can
  say it expired, and an owned trace goes whole. A shorter retention applies to traces already kept;
  a run deleted sooner by its own retention takes its trace with it.

### Endpoints

| Method and path | Who | What |
|---|---|---|
| `GET /settings/traces` | signed in | `{retentionDays, keepContent, changedBy, updatedAt}`, so the run page can say how long traces are kept |
| `PUT /settings/traces` | admin, session | `{retentionDays, keepContent}`; a day count out of range is 422 with `fields` |

### As built (#242)

- **Migration:** one file, `01500_keep_traces.sql`. `trace_due(last_span_at)` says a trace took no span for
  `retention_days`; the sweep asks it, and so do the policies that let `rotor_admin` delete a span or an owned trace,
  so it deletes nothing the retention keeps. A run's trace goes with its run by cascade.
- **Trace rows:** `started_at` and `last_span_at` are the row's making, which comes with its first span, and the trigger
  moves `last_span_at`. A span taken into an expired run's trace clears `expired_at`: the trace holds spans again, and
  its counts still include the swept ones. `data_key` may be null on an owned trace; only a run's trace is refused one,
  so #243 makes a key when it first seals text.
- **What a span holds:** the runner's spans are `error` or `unset`, since the runner protocol carries only whether a
  span failed. `sealed_content` is the span's text attributes as one JSON object of key to value, sealed with the
  run's box for the column `trace_span.content` and the row `<trace.id>/<span_id>` (`envelope.ColumnTraceContent`,
  `envelope.TraceSpanRow`). An event keeps its attributes without text; `exception.*` are such, and an event's text
  attributes, which no runner span carries, are dropped. The store leaves out a span id twice in one batch and a span
  ending before it starts, drops NUL, which Postgres refuses, and names an unnamed span `span`, so one odd span does
  not cost its batch.
- **Text:** `StartRun.trace_content` is set while `keep_content` is on or LangSmith is; the store keeps text while
  `keep_content` is on when it writes, as read within `TraceSettingTTL`, which replaces `LangSmithSettingTTL` now.
- **Every run is traced:** a run carries the sampled context it was started in, the api's `run.start` span when the
  collector sampled it, else a fresh sampled one. A caller's sampled `traceparent` is continued when the api has no
  collector too, rather than replaced, so a tool calling back with it joins the run. A trace id another run holds
  already, as when a caller starts two runs in one trace, gives the later run a fresh trace, since `trace_id` is unique
  among runs' traces; runs from before #242 that share one keep the earlier's spans only, the later's refused and
  logged.
- **The collector:** the runner's spans go to it only when `TraceIDRatioBased(ROTOR_TRACE_SAMPLE_RATIO)` keeps the
  trace id, and the worker's spans of a run follow, made unsampled when it does not.
- **The pool and the sweep:** a batch the store fails is logged at warn, one dropped for a full pool at debug.
  `TraceSweepBatch` counts spans deleted in one transaction; `TraceSweepEvery` (an hour) and `TraceSweepLockKey` join
  the limits, as for the run sweep.
- **Not stored:** the api's and the worker's own spans of a run. `source` is `runner` or `otlp`, so the issue's "from
  the api" is the OTLP endpoint (#243).
- **Web:** no control; the admin page shows the trace setting beside destinations (#246). `traces.changed` has its
  label in the audit log.

## Spans from any LangChain app (#243)

`POST /api/v1/otlp/v1/traces`, so an app sets
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://rotor.example/api/v1/otlp/v1/traces`, or
`OTEL_EXPORTER_OTLP_ENDPOINT=https://rotor.example/api/v1/otlp`, to which the SDK adds `/v1/traces`,
and `OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer <token>`. The nginx in front already passes
`/api/`, so nothing changes in compose or the chart.

- **Who:** a bearer token, nothing else:
  - a Rotor access token from Rotor's authorization server (#47). The route is the second, after
    `/mcp`, that takes one; a grant limited to reading is refused with 403 `read_only`, since
    sending spans writes into a trace.
  - an access token of the realm (#168): the same checks of issuer, audience, expiry and account,
    but any account Rotor lets in, not only `rotor-administrators`, since sending spans administers
    nothing. An account Rotor has not let in is refused with 403.
  - A session cookie is refused with 401: a browser sends no OTLP, and taking none leaves nothing
    for another site to forge.
  - The row gains a flag that says it takes both tokens, and
    `TestARealmTokenIsCheckedAtTheServiceAccountRoutesAlone` names it.
- **Bodies:** `ExportTraceServiceRequest` from `go.opentelemetry.io/proto/otlp`, as
  `application/x-protobuf` or `application/json`, with `Content-Encoding: gzip` or none. OTLP's JSON
  writes trace and span ids as hex, not base64, and enums as numbers; the decoder follows that.
  Another type or encoding answers 415.
- **Where each span goes**, by its trace id, judged as the sender:
  - a trace id of a run the sender reads: the span joins that run's trace, with `rotor.run` and
    `rotor.workflow` set from the run;
  - any other trace id, a run the sender may not read among them: the span joins the sender's own
    trace of that id, made on its first span. A run the sender may not read is thus answered exactly
    as a trace id no run has, and its trace never sees the span.
- **What a span carries:** `rotor.sender` (the sender's user id), `source = otlp` and its resource's
  `service.name`; other resource attributes, links and `rotor.*` keys the sender sent are dropped.
- **Checks per span:** a trace id of 16 bytes and a span id of 8, neither all zero; a name; an end
  not before its start. Names, attributes and events are held to the limits above and redacted. Text
  is kept only while `keep_content` is on, for a run's trace and an owned one alike.
- **Sent on:** spans that join a run go to the run's destinations and to the collector as the
  runner's do (#246); a sender's own traces stay in Rotor.
- **Tools that call back:** #243 has the runner send a `traceparent` header with each MCP request,
  from its `mcp <call>` span, so a tool that traces with OpenTelemetry continues the run's trace.
  Its spans join the run when the account it sends with reads the run, such as a service account in
  `rotor-administrators`.

### Answers

| Status | When | Body |
|---|---|---|
| 200 | the request was read; some or all spans may not have been taken | `ExportTraceServiceResponse` in the request's encoding; `partial_success` with `rejected_spans` and one sentence naming each reason and its count when any were not taken |
| 400 | the body is not an `ExportTraceServiceRequest` | `google.rpc.Status` in the request's encoding |
| 401 | no token, an unknown or expired one, or a cookie | Rotor's error envelope, from the shared middleware |
| 403 | a read-only grant, or an account Rotor has not let in | Rotor's error envelope |
| 413 | over `MaxOTLPBodyBytes` as sent or `MaxOTLPDecodedBytes` unpacked | `google.rpc.Status` |
| 415 | another content type or encoding | `google.rpc.Status` |
| 429 | the sender is over `OTLPRequestsPerMinute` or `OTLPSpansPerMinute` | `google.rpc.Status`, with `Retry-After` in seconds to the window's end |

Spans past `MaxOTLPSpansPerRequest`, past a trace's cap or past `MaxSenderSpans`, and spans that
fail a check, are rejected in `partial_success`; OTLP clients do not retry them. 413 and 429 refuse
the whole request.

The route table declares the protobuf body and answer as binary (`binaryRequest` and
`binaryResponse`) and the refusals as `OtlpStatus`; the JSON encoding is said in the row's
description, since a row names one media type. This section is the full description.

## Reading a trace (#244)

| Method and path | Who | What |
|---|---|---|
| `GET /runs/{runID}/trace` | reads the run's events | the run's trace as a tree |
| `GET /runs/{runID}/trace/spans/{spanID}` | reads the run's events | one span whole |
| `GET /traces` | signed in | senders' own traces the caller reads: their own, everyone's for an administrator; `sender`, `limit`, `offset`; newest first |
| `GET /traces/{traceID}` | the sender, or an administrator with `?sender=` | an own trace as a tree |
| `GET /traces/{traceID}/spans/{spanID}` | as above | one span of it whole |

A run or trace the caller may not read answers 404, as the run itself does; nobody learns that it
exists.

```json
{"trace": {
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736", "runId": "0193...", "sender": null,
  "state": "kept", "content": true, "cut": false, "dropped": 0, "spans": 41, "bytes": 183220,
  "startedAt": "...", "endedAt": "...", "expiresAt": "...",
  "links": [{"destination": "Phoenix", "url": "https://phoenix.example/projects/rotor/traces/4bf9..."}],
  "roots": [{
    "spanId": "00f067aa0ba902b7", "parentSpanId": "a3ce929d0e0e4736", "name": "run", "kind": "CHAIN",
    "service": "rotor-runner", "source": "runner", "sender": null,
    "startedAt": "...", "endedAt": "...", "durationMs": 5120, "status": "ok", "statusMessage": "",
    "model": "", "tokens": null, "attributes": {"rotor.run": "0193...", "rotor.workflow": "0192..."},
    "input": {"value": "{\"story\": \"As a ...", "mimeType": "application/json", "cut": true},
    "output": null, "events": [], "omitted": 0,
    "children": [{"name": "node analyse", "kind": "AGENT", "children": [{"name": "ChatAnthropic", "kind": "LLM",
      "model": "claude-...", "tokens": {"prompt": 1840, "completion": 312, "total": 2152}, "children": []}]}]
  }]
}}
```

- `state` is `kept`, `expired` (the sweep removed the spans; `roots` is empty, the counts stay) or
  `none` (no span yet). `content` says text was kept; `cut` and `dropped` say the trace reached its
  cap; `expiresAt` is `last_span_at` plus the retention.
- `roots` are the spans whose parent Rotor does not hold, the runner's `run` among them; siblings
  are in start order. `attributes` carry no text; `input` and `output` are `input.value` and
  `output.value` cut to `TracePreviewChars`, null when not kept. `model` and `tokens` read
  OpenInference's keys, else the `gen_ai.*` ones.
- The span routes answer `{span}` with the same fields, no `children`, the whole `input` and
  `output`, and `content`: every text attribute kept.
- `links` are the enabled destinations' trace links, `{traceId}` and `{runId}` filled in (#246);
  empty for an own trace, and until #246 lands.
- **A run's trace id:** `GET /runs/{runID}` and the items of `GET /runs` gain `traceId`, from
  `run.trace_id`.
- **MCP:** `get_run_trace` on `GET /runs/{runID}/trace` and `get_run_trace_span` on its span route,
  both reads. Through MCP the tree is shortened for a model: attributes only `rotor.node`,
  `rotor.node.type`, `rotor.binding`, `mcp.tool`, `tool.name` and `rotor.interrupted`; events only
  exceptions, the stack cut to `MCPTraceValueChars`; at most `MCPTraceMaxSpans` spans, failed spans
  and their ancestors first, then by start, a parent counting what it lost in `omitted`; inputs and
  outputs cut to `MCPTraceValueChars`, filled for failed spans first, then model and tool spans
  latest first, until the answer reaches `MCPTraceBudgetBytes`, null after. `get_run_trace_span`
  cuts each value so the answer fits `MCPMaxToolResultBytes`. Own traces are not tools. The pending
  rows carry no `tool:` yet; #244 adds them with their `toolHelp`.

## The trace on the run's page (#245)

- A Trace section on the run's page: the spans as a nested timeline, each a row with its name, kind,
  duration and a bar placed on the run's time, and a details pane with kind, timing, status and
  message, model, tokens, attributes, events and exceptions, and input and output where kept, the
  whole value fetched from the span route on demand.
- A failed span is marked by an icon and words, not colour alone; when the run failed, the first
  failed span is open.
- Notices for `none`, `expired`, `cut` and text not kept, saying the retention, and the
  destinations' links, opening in a new tab.
- Span text is shown as text: JSON is indented, nothing is rendered as markdown or HTML, and no
  address in it becomes a link.
- The timeline is a tree to the keyboard (arrows move and open, Enter shows the details) and to a
  screen reader; the accessibility check passes. Strings live in `web/src/strings_telemetry.ts`.

## Destinations (#246)

### Table

`trace_destination`: `id`, `name` (unique, 1 to `MaxDestinationNameChars`), `endpoint` (an `http` or
`https` URL with no user, query or fragment, the whole OTLP/HTTP traces URL), `header_names text[]`,
`headers_set_at`, `enabled`, `content`, `content_acknowledged_by` and `content_acknowledged_at`
(`CHECK (NOT content OR content_acknowledged_at IS NOT NULL)`), `trace_link` (null, or a URL holding
`{traceId}` and maybe `{runId}`), `carried_over`, `changed_by`, `updated_at`. Administrators read
and write it, `rotor_admin` for the api's sending; a trigger refuses a row past
`MaxTraceDestinations`. Header values live in OpenBao at
`rotor/settings/trace-destinations/<id>/headers`, never in Postgres, a log line or an answer.

### Endpoints

| Method and path | Who | What |
|---|---|---|
| `GET /settings/trace-destinations` | admin | `{destinations}`, by name |
| `POST /settings/trace-destinations` | admin, session | `{name, endpoint, headers, enabled, content, acknowledgeContentLeaves, traceLink}` adds one; 201 |
| `GET /settings/trace-destinations/{destinationID}` | admin | `{destination}` |
| `PUT /settings/trace-destinations/{destinationID}` | admin, session | changes it; `headers` left out keeps them, `{}` removes them |
| `DELETE /settings/trace-destinations/{destinationID}` | admin, session | removes it and its headers; 204 |
| `POST /settings/trace-destinations/{destinationID}/test` | admin, session | sends one span without text; `{delivered, status, message}` |

- A destination answers `id`, `name`, `endpoint`, `headerNames`, `headersSetAt`, `enabled`,
  `content`, `contentAcknowledgedBy`, `contentAcknowledgedAt`, `traceLink`, `carriedOver`,
  `changedBy` and `updatedAt`.
- `content: true` without `acknowledgeContentLeaves: true` is 422 `content_leaves_unacknowledged`; a
  name taken is 409; a bad endpoint, header or link is 422 with `fields`. A header name that is
  `Content-Type`, `Content-Encoding`, `Content-Length` or `Host` is refused.
- An administrator chooses the endpoint, so it is not held to `netguard`: an in-cluster Jaeger or
  Phoenix is the common case, as `ROTOR_OTLP_ENDPOINT` is.

### Sending

- Each enabled destination gets every run's trace, the runner's spans and the spans that joined the
  run, as the service each came from, without text unless its `content` is on. Senders' own traces
  go nowhere.
- As #234 made export: beside the run, on the export pool, each destination side by side within
  `RunnerSpanExportTimeout`, a failure costing that destination that batch alone. A replica rebuilds
  a destination's exporter when it changes there, and others within `TraceSettingTTL`.

### LangSmith carried over

- Migration 01520 makes `trace_destination` and, where `langsmith_setting` ever held a key, a row
  named `LangSmith`: the default LangSmith endpoint, `header_names` `x-api-key` and
  `Langsmith-Project`, `enabled` and `content` as LangSmith was on, acknowledged by whoever turned
  it on, `carried_over`. A carried-over row may be enabled before its headers are set; nothing is
  sent to it until they are.
- The api's start finishes the carry-over once, since a migration cannot reach OpenBao: it writes
  the key from `rotor/settings/langsmith/key` and the project as the row's headers, takes
  `ROTOR_LANGSMITH_ENDPOINT` as its endpoint when set, deletes the old key, and audits it. Without a
  key the row stays disabled.
- `GET`/`PUT /settings/langsmith` and `DELETE /settings/langsmith/key` stay, `deprecated: true`,
  acting on the carried-over destination (a PUT makes it where none was carried over) with their
  shapes and refusals, and are removed one minor release later, with `langsmith_setting`. The admin
  page shows destinations and the trace setting in place of the LangSmith section.

## Audit

| Action | Detail | Issue |
|---|---|---|
| `traces.changed` | `{retentionDays, keepContent}` | #242 |
| `trace_destination.added` | `{name, endpoint, headers: [names], enabled, content, acknowledged}` | #246 |
| `trace_destination.changed` | `{name, endpoint, headers: set\|kept\|removed, enabled, content, acknowledged}` | #246 |
| `trace_destination.removed` | `{name}` | #246 |
| `trace_destination.carried_over` | `{name, enabled, content}`, by the api's start | #246 |

Never a header's value. Spans taken, cut or swept are not audited: they are data, and
`trace.dropped` counts what a cap refused.

## Problems

| Code | Status | Where | Issue |
|---|---|---|---|
| `content_leaves_unacknowledged` | 422 | a destination's `content` | #246 |
| `trace_destination_taken` | 409 | another destination has the name | #246 |
| `trace_destinations_full` | 422 | an add past `MaxTraceDestinations` | #246 |
| `read_only` | 403 | the OTLP endpoint with a grant to read | #243 |

## The order the issues land in

1. #241 and #242, side by side. Before #242 the runner's spans are exported as today; before #241
   Rotor keeps #37's spans.
2. #243, #244 and #246, side by side, once #242 has landed (#246 also after #241). #243 reads back
   through #244's routes once both are in, and through the store as the sender before.
3. #245, after #244.

## Choices made here

- The endpoint sits under `/api/v1/otlp`, so the nginx and ingress that pass `/api/` serve it and
  the SDK's own path suffix applies.
- Text is kept in Rotor only while an administrator turns `keep_content` on: the user asked for text
  by opt-in.
- A span into a run its sender may not read joins the sender's own trace, so the endpoint answers it
  as a trace id of no run, and no run's existence leaks.
- The realm token at the endpoint needs any account Rotor lets in, not an administrator: sending
  spans administers nothing.
- Every run is traced; the sample ratio bounds the collector, not Rotor.
- Owned traces are read by their sender and administrators, so a service account's traces have
  someone to read them.
- Text at rest is sealed like run events (#41); attributes without text stay plain for the tree.
- Retention runs from a trace's last span, so a run waiting days for an approval keeps its early
  spans.
- LangSmith becomes a plain OTLP destination with two headers, its key moved by the api's start.
- The endpoint's rates are counted in each replica's memory, not Valkey: no round trip per export,
  and n replicas take at most n times the rate.
- A second MCP tool, `get_run_trace_span`, so a model can read one span whole after the shortened
  tree.

## What is left for later

- Long-lived ingest keys for the endpoint, in place of tokens an app refreshes.
- Sibling tools sending their spans, and trace context in Exciter, Armature and Stator.
- Text kept per workflow rather than per installation, and per-workflow sampling for destinations.
- A page for senders' own traces in the web app, and own traces as MCP tools.
- Span links, OTLP metrics and logs, and gRPC OTLP.
- Searching spans across runs, and evaluations or scores on spans.
- Dropping `langsmith_setting`, `ROTOR_LANGSMITH_ENDPOINT` and the `/settings/langsmith` routes, one
  minor release after #246.
