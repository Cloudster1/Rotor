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
- searching spans across the traces a person reads (#261)

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
  | 01540 | #256 | `langsmith_setting` dropped |
  | 01550 | #259 | `workflow_tracing` |
  | 01570 | #261 | the indexes a span search walks |

- Packages: `internal/traces` (the store, the cap, sealing, the tree and the sweep; #242, #244),
  `internal/traces/otlp` (decoding OTLP, #243), `internal/tracedest` (destinations, replacing
  `internal/langsmith`; #246), `internal/runnerctl` and `internal/observability` (checking and
  fanning out spans), the runner's `telemetry.py` (#241), `web/src/features/runs/trace/` (#245) and
  `web/src/features/settings/TraceDestinationsSection.tsx` (#246).
- `api/trace-content.json` (#241) lists the attribute keys and prefixes that carry text a person, a
  tool or a model wrote. Go and Python both read it, and a test on each side refuses a copy that
  differs, as for `api/secret-patterns.json`.
- Settings, `ROTOR_` variables: none new. `ROTOR_OTLP_ENDPOINT` and `ROTOR_TRACE_SAMPLE_RATIO` keep
  their meaning for the operator's collector. `ROTOR_LANGSMITH_ENDPOINT` was read only to carry
  LangSmith over (#246) and went with `langsmith_setting` (#256).

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
| `MCPTraceSpanBudgetBytes` | 32 KiB | where `get_run_trace` stops adding spans, so values have room (#244, as built) |
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
  the error alone, `Type: message` as Python prints it, made from LangChain's repr; LangChain
  glues its stack to that repr without a newline, so the message is cut at
  `Traceback (most recent call last):`. The event carries the whole stack (#254).
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

### As built (#243)

- **Pins:** `go.opentelemetry.io/proto/otlp` v1.11.1 (2026-09-30), made a direct dependency; it moved
  `grpc-gateway` to v2.31.0 (2026-09-25) and `go-openapi/swag` to v0.29.2 (2026-09-04). `google.rpc.Status` is
  `google.golang.org/genproto/googleapis/rpc` at the version already locked (2026-10-05). The runner's Python
  test pins `opentelemetry-exporter-otlp-proto-http` 1.45.1 (2026-10-06), as a dev dependency beside the SDK's
  1.45.1; it brings `googleapis-common-protos` 1.76.0 (2026-10-22).
- **The row's flag** is `tokensOnly`, and the OpenAPI document gains a `rotorToken` scheme beside `realmToken` for
  it. The sentence refusing a Rotor token elsewhere names the OTLP endpoint beside MCP. A grant to read is refused
  by the middleware that refuses its every write, with its sentence. A realm's token is checked as #168's without
  the group (`oidc.Service.AuthenticateSender`); an account nobody has let in answers 403 `forbidden` and is not
  made a person, nor does it ask to be let in, since no person is at a keyboard. No token, an unknown or expired
  token of Rotor's, and a cookie answer 401 `unauthorized`; a realm's token that fails a check, `invalid_token`.
- **The order of the checks:** the token, then the media type and encoding (415, in JSON when the type is none
  OTLP knows), then the requests a minute (429), then the body's size (413, at once when `Content-Length` says so),
  then the body (400), then the spans a minute (429), then the spans one by one. A request is counted once it
  reaches the second check; spans are counted as sent, at most `MaxOTLPSpansPerRequest`, before their checks, and
  a request whose spans would take the sender past `OTLPSpansPerMinute` is refused whole and counts none. A window
  is a minute from a sender's first request in it (`OTLPRateWindow`), and `Retry-After` the whole seconds left,
  at least one.
- **Reading:** OTLP's JSON is read through proto3's after its hex ids are written as base64; fields Rotor does not
  know are ignored, and an id that is not hex costs its span alone. Attribute values that are not text are
  written as text, lists and maps as JSON and bytes as base64. An event's text attributes are dropped, as the
  runner's are. A resource without `service.name` is `unknown_service`; a name is redacted and cut to
  `MaxSpanNameChars`. A span the app marked OK is stored `ok`.
- **Partial success** names, in one sentence with their counts: spans past `MaxOTLPSpansPerRequest`, without
  valid ids, without a name, ending before they start, with a span id twice in the request, and those the cap's
  trigger skipped. The trigger skips a span past a cap and a span id the trace holds already alike, so the last
  reason names both. JSON writes `rejectedSpans` as a string, as proto3's JSON writes 64-bit numbers.
- **Joining a run:** the sender's runs are found by `run.trace_id` in one query as the sender; where two runs from
  before #242 share a trace id, the earlier one is joined, and should its trace hold another id the spans are
  rejected as naming a run whose trace id an earlier run holds. The run's trace is made as the sender when the
  sender's span comes first.
- **An owned trace's key** is made by OpenBao transit when its trace first seals text and bound to the trace row's
  id: its text opens with `Keys.For(trace.id, trace.data_key)` and the row `envelope.TraceSpanRow(trace.id,
  span_id)`, as a run's does. A trace made before it had text is given its key through `rotor_admin`, since
  `rotor_app` holds no `UPDATE` on `trace` and the counts must move by the trigger alone; the update is held to the
  sender's own trace, which the sender's read found just before, and sets a key only where none is. No migration
  was needed, so 01530-01539 stay free.
- **Storing** happens in the request, bounded by `TraceStoreTimeout`, rather than on the pool of
  `MaxTraceStoreWrites`, since the answer says what was kept; a failure answers 500 in Rotor's envelope. Spans
  that join a run are sent on through the runner's forwarder (the collector as the sample ratio says, without text;
  LangSmith while it is on), on a pool of `MaxRunnerSpanExports` of their own, under the sender's `service.name`;
  #246 puts destinations behind the same forwarder.
- **Tools that call back:** the runner sends the `traceparent` of the span current at each MCP request: the
  `mcp <call>` span for a call, and the span current where a tool's tools are listed.
- **Headers:** the changelog writes `OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>`, percent-encoded
  as OpenTelemetry's specification has it; the Python SDK takes a plain space too.

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

### As built (#244)

- **Reading as the caller:** the trace, its spans and the run's or the own trace's key are read as `rotor_app` in one
  transaction, so `trace_read` and `run_visible` decide. Both run routes answer a run the caller may not read with the
  run routes' own 404, "There is no such run, or you may not see it."; a span the trace does not hold, or no longer
  does, answers 404 naming the span. An own trace the caller may not read answers 404 as one of no such id. A trace or
  span id is taken in any case and lowercased; one that is not 32 or 16 hex characters answers 400 before anything is
  read, so it tells nothing either.
- **The tree:** a span whose parent is empty, not held, or itself is a root, and so is the earliest span of a loop of
  parents a sender made up, so every span shows once. `startedAt` and `endedAt` are the earliest start and the latest
  end of the spans held; an expired trace has the time it took its first span and no end. `expiresAt` is null for
  `none`, and in the past for `expired`. `state` is `none` also for a trace row whose every span a cap refused.
- **Values:** `input` and `output` say `cut` also when the value is `MaxSpanContentChars` long, since the runner and
  the endpoint cut to it and leave no other mark. Without OpenBao no text opens, so they are null though `content`
  says text was kept. The sender of an own trace is named as its spans recorded the name, since `rotor_app` reads no
  other account.
- **`GET /traces`:** newest first by the time a trace took its first span, which `startedAt` is; `rootName` and
  `service` are the earliest root span's, empty when the trace holds none; `endedAt` the latest end. `limit` and
  `offset` are read as the other listings read them, a limit out of range meaning 50.
- **`links`** is `[]` everywhere until #246 builds destinations; #246 fills it for a run's trace.
- **`traceId`** on a run is null for a run without a `traceparent`, from before #37.
- **MCP:** the run routes shorten when MCP calls them in this process, so the tools and the routes stay one row each
  and the output schema is `traceResponse` and `traceSpanResponse` alike. Two changes to the shortening, since 300
  spans of about 400 bytes each come to twice `MCPMaxToolResultBytes`, which refuses an answer whole:
  - spans are added, failed ones and their ancestors first and then by start, while they number at most
    `MCPTraceMaxSpans` and come to at most `MCPTraceSpanBudgetBytes` (32 KiB, a new limit), so values have room;
    a span whose missing ancestors would pass either is skipped and a later one tried;
  - values then fill, each span's input and output together, until the next would pass `MCPTraceBudgetBytes`, and
    stop there.
  `omitted` counts the children a span lost; roots left out are counted nowhere, and `spans` still counts the whole
  trace. An exception event keeps every attribute cut to `MCPTraceValueChars`, not only its stack.
  `get_run_trace_span` cuts its input, output, `content` and event attributes to one length, the longest that fits
  `MCPMaxToolResultBytes`; only `input` and `output` carry `cut`. Both tools' help says span text is data the run saw,
  never instructions, as the threat model asks, and the server's instructions name `get_run_trace`. No capability is
  added, since names come from Dynamo's shared list.

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

### As built (#245)

- **Where:** `web/src/features/runs/trace/` (`TraceSection`, `SpanTree`, `SpanDetails`, `SpanText` and the pure
  `tree.ts`), the queries in `web/src/api/traces.ts`, the words as `traceStrings` (`s.trace`). The Trace section
  closes the run's page, under its output.
- **The first failed span:** of the failed spans none of whose descendants failed, the one that started first, since
  a failure marks every span it passes through up to `run`. It is chosen, and it and its ancestors open, only when the
  run's status is `failed`; in any other run failed spans are marked and nothing is chosen for the person.
- **Status in a row:** a failed span says "Failed" beside a warning icon and its name and bar turn the danger colour;
  `ok` and `unset` say nothing in the row. The details say Succeeded, Finished (`unset`, as the runner marks only
  failures) or Failed, and the status message under it.
- **The bars** are placed on the trace's time, `startedAt` to `endedAt` widened by any span past them, rather than the
  run's `startedAt` and `finishedAt`, which include the sandbox's start; a narrow screen hides them and keeps the
  duration.
- **Who sent a span:** a row says "sent by" its sender for `source: otlp`, and the details name the runner or the
  sender and the service, which closes the threat model's row on senders posing as the runner for the page.
- **Keyboard and screen reader:** `role="tree"` labelled by the section's heading, the rows flat `treeitem`s with
  `aria-level`, `aria-posinset`, `aria-setsize`, `aria-expanded` and `aria-selected`, one row taking Tab. Up and Down
  move, Right opens or goes to the first child, Left closes or goes to the parent, Home and End, and Enter or Space
  shows the span. Choosing does not follow focus, so walking the tree reads no span. The pointer opens a span by its
  chevron and chooses it by its row. The details are a region named "Span <name>".
- **Large traces:** no virtual list. A trace of at most 300 spans (`OPEN_ALL_SPANS`) opens whole; a larger one opens
  to its nodes (`OPEN_DEPTH`, 2) and the way to the failure. A span shows 100 children (`CHILDREN_PAGE`) and then a
  "Show more" row, itself a tree item, for the next 100; a page always reaches the child on the way to the failure.
- **The whole value:** the span route is asked when a span is chosen and only when the trace's `content` is true,
  since without text it answers nothing the tree lacks. Its text attributes (`content`) show as "Text attributes". If
  it fails, the 200 characters of the tree stay with a note saying so.
- **Text as text:** names, attributes, events, status messages, inputs and outputs are React text in `pre` and `dd`;
  JSON (a `mimeType` with `json`) is indented when it parses, else shown as it came, a cut preview included. A value
  scrolls past a screenful and takes focus to be scrolled.
- **Notices:** `kept` says the span count and "Kept until <expiresAt>, N days after its last span", N from
  `GET /settings/traces`, left out if that cannot be read; `none` says the run has no trace, or, while it runs, that no
  span has arrived yet; `expired` and `cut` (with `dropped`) are warnings; text not kept says an administrator
  chooses it, with no control until #246. `links` render as "Open this trace in <destination>" in a new tab.
- **Fresh spans:** the trace is asked again every `RUN_POLL_MS` while the run goes on and for `TRACE_SETTLE_MS`
  (10 s) after it ended, since the runner's last batch is stored after `run.finished`. It sits under the run's query
  key, so whatever asks for the run again asks for its trace.
- **`omitted`** shows on a row when above 0, though only MCP's shortening sets it today.

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

### As built (#246)

- **Where:** `internal/tracedest` (`tracedest.go` the rows, `check.go` the input, `forward.go` sending and links,
  `probe.go` the test, `carry.go` the carry-over, `langsmith.go` the deprecated routes), replacing `internal/langsmith`;
  header values through `vault/tracedest.go`. The control plane's OpenBao policy, in the compose stack and the chart,
  gains `rotor/{data,metadata}/settings/trace-destinations/*`.
- **Migration:** one file, `01520_trace_destinations.sql`. A name is unique whatever its case (`lower(name)`), and one
  row at most is `carried_over`. The cap's trigger takes an advisory lock, so two administrators adding at once cannot
  pass it between them, and refuses as `trace_destination_cap`; text unacknowledged is refused as
  `trace_destination_content_acknowledged`; a Go test holds the SQL numbers to the constants. A member is refused by the
  policy, straight through SQL as `rotor_app`.
- **Carried over, the migration:** "ever held a key" is `key_set_at IS NOT NULL`, since forgetting a key cleared it and
  nothing else remembers one; the row is acknowledged by `changed_by` at `updated_at` when it was on. `header_names` are
  kept in order, case aside, as every row keeps them. `langsmith_setting` gains `carried_at`, not in the table above:
  the api's start sets it once it has carried LangSmith over, since a carried row without headers cannot otherwise be
  told from one whose headers an administrator removed.
- **Carried over, the start:** as `rotor_admin` under the advisory lock `rotor.tracedest.langsmith`, so replicas starting
  together do it once. `ROTOR_LANGSMITH_ENDPOINT` always becomes the endpoint, as its default is LangSmith's own;
  `headers_set_at` is the time the key was given. Without a key the row is turned off and its text with its
  acknowledgement cleared. The old key is deleted after the commit, at every start, so one that stopped between the two
  deletes it next time. A start that cannot reach OpenBao logs a warning and serves; the row sends nothing meanwhile.
  `trace_destination.carried_over` has no actor.
- **Sending:** a destination is sent to while `enabled` and its headers are set or it names none. One batch is made once
  with text and once without, and each destination exports side by side on the pool the batch came on (the runner's
  `MaxRunnerSpanExports`, or #243's own). An exporter is kept per destination and made again when its `updated_at`
  moves; the old one is shut down beside the export. A failure is logged at warn, naming the destination, as LangSmith's
  was. `StartRun.trace_content` is set while `keep_content` is on or any destination sent to takes text.
- **Endpoints:** the acknowledgement is asked on a change that turns text on or moves the endpoint of a destination that
  gets text, and taken again when given, so it then names the caller; turning text off clears it. A name is trimmed and
  at most 64 characters without control characters. A header name is an RFC 9110 token, values are trimmed, one line and
  not empty, each name and value at most `MaxDestinationHeaderChars`, and a name twice in another case is refused. An
  empty `traceLink` is none; a link must be `http` or `https` once filled. Without OpenBao the routes answer 503
  `trace_destinations_unavailable`, and a trace links nowhere.
- **The test** posts one OTLP protobuf span, `rotor.test` of the service `rotor`, in a fresh trace, whether or not the
  destination is on, within `DestinationTestTimeout` (10 s), following no redirect, which would carry its headers to
  another host. `status` is 0 when it could not be reached, and `message` a sentence naming what to check.
- **Audit:** `headers` of `trace_destination.added` lists the names, and `acknowledged` of `.changed` says whether that
  change acknowledged text. The audit test's shape of an action now takes a domain in snake case.
- **The deprecated LangSmith routes** audit as `trace_destination.added` or `.changed` rather than `langsmith.changed`,
  since they change a destination. On means sending text, acknowledged by the caller, and off clears it; the key and the
  project are the headers `x-api-key` and `Langsmith-Project`, so `GET` reads OpenBao for the project, and `keySetAt` is
  `headersSetAt` while the key is held. `DELETE /settings/langsmith/key` keeps the project. With no carried-over row
  `GET` answers LangSmith off with the default project, and `PUT` makes a row named `LangSmith` at
  `ROTOR_LANGSMITH_ENDPOINT`, answering 409 `trace_destination_taken` should another destination have the name.
- **Links:** for a run's trace whose `state` is not `none`, in the browser's answer and MCP's alike: each destination sent
  to that has a link, by name, read as `rotor_admin` within `TraceSettingTTL`, so whoever reads the run sees them.
- **The tool stub** takes OTLP at `POST /otlp/{sink}/v1/traces` for any sink name and answers each sink's exports at
  `GET /control/otlp/{sink}` with every header, so each test has a destination of its own.
- **Web:** Admin, Traces (`/admin/traces`, which `/admin/langsmith` leads to) in place of the LangSmith section:
  `TraceKeepingCard` sets the retention and whether text is kept, and `TraceDestinationsSection` with `DestinationForm`
  lists, adds, changes, tests and removes destinations, from presets for Langfuse, Arize Phoenix, Jaeger and LangSmith
  that only fill the form. Editing one that holds headers chooses to keep, replace or remove them; values are password
  fields and never shown. Keeping text on for a destination that sends it leaves the acknowledgement ticked, so saving
  acknowledges it again in the editor's name. The words are `s.traceDestinations` in `web/src/strings_destinations.ts`.
  The run page's notice that text is not kept now links an administrator to Admin, Traces.

### As built (#256)

- **Removed before the first release** rather than one minor release after #246, as the user chose, since Rotor was
  never released: `GET`/`PUT /settings/langsmith`, `DELETE /settings/langsmith/key`, `tracedest/langsmith.go`, the
  start's carry-over (`tracedest/carry.go`, `vault/langsmith.go`), `ROTOR_LANGSMITH_ENDPOINT` with the chart's
  `telemetry.langsmithEndpoint`, the tool stub's LangSmith stand-in (a test sends to a sink with LangSmith's headers),
  `rotor/{data,metadata}/settings/langsmith/*` from the control plane's OpenBao policy, and `/admin/langsmith` in the
  web, which only development ever linked to. The old routes answer 404.
- **Migration 01540** drops `langsmith_setting` and `trace_destination_one_carried`, whose one reader was the old
  routes; `carried_over` stays, so the row 01520 made still shows as carried over. Its Down makes the table again as
  01520 left it, with one row whose `carried_at` is set, so the start of #246 has nothing to move and its routes find
  the row they read.
- **The key not yet moved:** SQL cannot reach OpenBao, and goose cannot run the start of #246 between two migrations,
  so 01540 refuses (`55000`, a sentence saying what to do) while a row has `key_set_at` set and `carried_at` null,
  rather than lose the key. Starting the previous version once with OpenBao reachable moves it; then the migrations
  run again. A key whose move committed but whose old copy a stopped start never deleted stays at the old path, as
  nothing reads or deletes it any more.
- **Audit:** `langsmith.changed` and `trace_destination.carried_over` stay among the actions, as entries of them stay
  in the log.

## Per-workflow tracing (#259)

An administrator of a workflow chooses whether its runs' traces keep text and where they are sent, beside its
other settings (#205).

### What a workflow chooses

- **Text:** `content` is `inherit` (the default: as the installation's `keep_content` and each destination's
  `content` say) or `none` (no text, whatever the installation says). There is no `keep`: a workflow only ever
  has less text than the installation, since an installation administrator's choice to keep none must hold.
- **What `none` means:** the runner is started with `trace_content` false, Rotor stores none of the run's text,
  and no destination gets any, spans joining the run through the OTLP endpoint included. Only then is the choice
  safe to rely on for a sensitive workflow.
- **Destinations:** `destinations` is null (the default: every enabled destination, ones added later included)
  or a list of destination ids (those of them that are enabled; an empty list is none). A list is chosen so a
  sensitive workflow does not start sending to a destination added later without its administrators knowing.
  A destination removed drops out of every list.
- **Sampling:** `sampleRatio`, 0 to 1, default 1, is the share of the workflow's runs sent to its destinations,
  decided by the trace id as the collector's ratio is, so a trace is sent whole or not at all, to each chosen
  destination alike. Rotor's own store keeps every run's trace and `ROTOR_TRACE_SAMPLE_RATIO` still bounds the
  collector alone, since the ratio is about what leaves Rotor for a shared tool.

### Storage and who changes it

- Migration 01550 makes `workflow_tracing`: `workflow_id` (the key, cascading with the workflow), `content`
  (`inherit` or `none`), `destinations uuid[]` (null for all, at most `MaxTraceDestinations`), `sample_ratio`
  (`double precision`, 0 to 1), `changed_by`, `updated_at`. A table of its own rather than columns on
  `workflow`, since `workflow_change_guard` tells columns apart by who may change them and this takes its own
  policies; a workflow without a row has the defaults, so no workflow needs filling in.
- Policies: whoever reads the workflow (`workflow.read`) reads its row, whoever administers it
  (`workflow.administer`) adds, changes and removes it, an installation administrator through the
  installation-wide grant; `rotor_admin` reads every row for the api's runs and the OTLP endpoint.
- `destinations` is not a foreign key, since Postgres has none for an array: the api refuses an id that is
  not a destination, and a removed one is left out wherever the list is read.

### Endpoints

| Method and path | Who | What |
|---|---|---|
| `GET /workflows/{workflowID}/settings/tracing` | whoever reads the workflow | `{tracing, installation: {keepContent}, destinations: [{id, name, enabled, content}], canChange}` |
| `PUT /workflows/{workflowID}/settings/tracing` | its administrators, session | `{content, destinations, sampleRatio}` changes it; answers as `GET` |

- A route of its own rather than the workflow's `PUT`, which saves a version: the choice is the
  installation's handling of runs, not part of the spec, and a version restored must not undo it.
- `tracing` is `{content, destinations, sampleRatio, changedBy, updatedAt}`, `changedBy` and `updatedAt` null
  while the workflow has the defaults. `installation.keepContent` and each destination's `content` say what
  `inherit` means for the person choosing, who need not be an installation administrator; a destination is
  answered by name alone, as a run's trace links already show it, never its endpoint or headers.
- A workflow it does not read is 404; one it reads but does not administer is 403 `forbidden`. A `content`
  other than the two, a ratio outside 0 to 1, or a destination id that is none, twice, or past
  `MaxTraceDestinations` is 422 `validation_failed` naming the field.
- A change is audited as `workflow.tracing_changed` on the workflow, naming the destinations rather than their ids.

### Applying it

- **The runner's spans:** the stream reads the workflow's row with the run, once per runner stream as
  `rotor_admin` (`loadRun`), so no batch reads the database; a change reaches runs started afterwards, and a
  running run at its runner's next stream. `StartRun.trace_content` is `content` not `none` and (`keep_content`,
  or some destination the run's trace is sent to, chosen, enabled and sampled, takes text).
- **Rotor's store** keeps no text of a `none` run's spans, as with `keep_content` off.
- **The forwarder** takes the run's choice with each batch and sends to the destinations it read within
  `TraceSettingTTL` that the choice names and the ratio keeps; the collector is untouched.
- **Spans joining a run** through the OTLP endpoint: the choices of the runs a request joins are read once per
  request beside finding the runs, and applied as to the runner's.

### As built (#259)

- **Where:** `tracedest/workflow.go` (reading, checking and changing the choice), the routes in
  `httpapi/workflow_tracing_handlers.go`, and `observability.RunTracing` (`choice.go`), the choice as a run carries it
  to the runner protocol, the store and the forwarder; its `SendsTo` decides by the trace id with the SDK's
  `TraceIDRatioBased`, as the collector's ratio does. A Go test holds the migration's cap to `MaxTraceDestinations`.
- **Who:** the api asks `actor_can('workflow.administer', id)` of a workflow the caller reads, so an administrator of
  Rotor needs no grant on it; an editor or runner reads it with `canChange` false. Like the destinations, the routes
  answer 503 `traces_unavailable` where Rotor has no OpenBao, since nothing would run there either. A destination id
  that is not a UUID is refused as one that is none.
- **Spans joining a run:** the choices are read as `rotor_admin` in one query for the workflows of the runs a request
  joins, since a run's starter may no longer read its workflow.
- **Web:** the workflow's settings gain Tracing (`/workflows/$workflowID/settings/tracing`,
  `features/workflows/settings/TracingSection.tsx`), with the words in `web/src/strings_workflow_tracing.ts`; it says
  whether the installation keeps text and which destinations take it, and is read only without `canChange`.

## Searching spans (#261)

A person searches the spans they may read across runs and their own traces, to find every run where a tool or a
model failed the same way. The same row level security as a run's trace decides (#242): the search reads as
`rotor_app`, so a span of a run the person may not read is never found, counted or named.

### The query

`GET /spans`, a route of its own rather than under `/traces`, whose ids are an own trace's, and read by anyone signed
in. Every filter narrows; none is needed.

| Parameter | Matches |
|---|---|
| `name` | the span's name holds it, whatever the case |
| `kind` | the OpenInference kind, exactly (`TOOL`, `LLM`, ...), whatever the case |
| `status` | `unset`, `ok` or `error`; `error` finds failed spans |
| `model` | the model a span called (`llm.model_name`, else `gen_ai.request.model`) holds it, whatever the case |
| `service` | the span's service, exactly |
| `workflow` | spans of that workflow's runs, by its id; never an own trace |
| `attrKey`, `attrValue` | one attribute without text equal to the value; both or neither |
| `since`, `until` | RFC 3339; the span started at or after `since` and before `until` |
| `limit`, `cursor` | a page: 50 by default, at most 200; `cursor` is the `next` of the page before |

- **The window is always bounded:** `until` defaults to now and `since` to `SpanSearchDefaultDays` (7) before it;
  a `since` more than `SpanSearchMaxDays` (30) back, or an `until` not after it, is 422 `validation_failed` naming
  the field. A month is what a person debugging reaches back for, and bounds the index range a search may walk.
- **Text is never searched:** a key `api/trace-content.json` calls text (`input.value`, messages, anything it does
  not list as plain) is 422 under `attrKey`, since its value is sealed (#242) and searching it would need it open.
  A name or a status message is plain and is searched and answered.
- A filter longer than `MaxSpanNameChars` (a value than `MaxSpanAttrChars`), a kind or status that is none, or a
  workflow that is not a UUID is 422 naming the field; a cursor no page answered is 400.

```json
{"spans": [{
  "spanId": "00f067aa0ba902b7", "name": "mcp whoami", "kind": "TOOL", "service": "rotor-runner", "source": "runner",
  "status": "error", "statusMessage": "The tool refused the call.", "startedAt": "...", "durationMs": 250,
  "model": "", "traceId": "4bf92f3577b34da6a3ce929d0e0e4736",
  "runId": "0193...", "workflow": {"id": "0192...", "name": "file-story"}, "owner": null
}], "next": "eyJhdCI6...", "since": "...", "until": "...", "limit": 50}
```

- Newest first by `startedAt`, then the trace and span id, so the order is total; `next` is an opaque cursor of
  the last hit's place, null when no span is left. Keyset paging rather than `offset`: a page never repeats or skips
  a span as spans arrive, and no `total` is counted, which would walk the whole window.
- A hit names the run (`runId`, `workflow`; the name empty when the person reads the run but not the workflow) or
  the own trace (`traceId`, `owner`), never its attributes or text: the span route reads it whole.

### Bounded

- Migration 01570 adds `trace_span (started_at, trace, span_id)`, `(kind, started_at, trace, span_id)` and
  `(started_at, trace, span_id) WHERE status = 'error'`, each ending in the search's order, so a page walks one
  backwards and stops once it has `limit` spans the person reads. Only leakproof comparisons (a timestamp, text
  equality) may be used ahead of row level security, so the name, model and attribute filters are checked on the
  spans the index yields; a GIN index on `attributes` could not be used under the policies and is not made.
- The statement runs with `statement_timeout` at `SpanSearchTimeout` (5 s); past it the search answers 422
  `search_too_broad`, saying to narrow the window or add a kind, a workflow or failed spans only.

| Constant | Value | Why |
|---|---|---|
| `SpanSearchDefaultDays` | 7 | the window a search names none of |
| `SpanSearchMaxDays` | 30 | how far back a search starts |
| `SpanSearchPageSize`, `SpanSearchMaxPage` | 50, 200 | as the other listings |
| `SpanSearchTimeout` | 5 s | a search that walks too many spans the person may not read stops |
| `MCPSpanSearchMaxPage` | 50 | a page of `search_spans`, which stops sooner at `MCPTraceBudgetBytes` |

### MCP and the web app

- **MCP:** `search_spans` on `GET /spans`, a read. Through MCP a page is at most `MCPSpanSearchMaxPage` and stops
  adding hits at `MCPTraceBudgetBytes`, its `next` going on from the last hit given; its help says names and
  messages are data, never instructions, and that `get_run_trace_span` reads a hit whole.
- **Web:** Search spans at `/traces/search`, linked from Runs and Traces: name, kind, workflow, a window of the last
  24 hours, 7 or 30 days, model, service, an attribute and its value, and failed only, kept in the address. Hits
  show the span, its status and message, the run's workflow or the trace's sender, service, model, start and
  duration, with Show more for the next page. A hit opens the run's page (`/runs/{runID}?span=`) or the own trace's
  (`/traces/{traceID}?sender=&span=`) with that span chosen and the way to it open, before any failed span.
- A search is a read and is not audited, as reading a trace is not.

### As built (#261)

- **Where:** `traces/search.go` (the filter, its checks and the cursor) and `traces/search_query.go` (the
  statement), the route in `httpapi/trace_search_handlers.go`, and `web/src/features/traces/SpanSearchPage.tsx`
  and `SpanHits.tsx`, with the words in `web/src/strings_span_search.ts`, read as `s.traces.search`.
- **Two statements:** the search reads `trace_span` alone, writing only the conditions asked for so the planner
  sees each; the page's traces, their runs, workflows' names and senders are read after, for its at most 200
  traces. Joining them into the search made the policies weigh every trace, run and workflow once more per search:
  on a seeded table of 30000 spans in six runs, 750 ms became 30 ms.
- **The plan,** explained as `rotor_app` by `TestSpanSearchWalksAnIndex` over that table: an `Index Scan Backward`
  on `trace_span_started`, `trace_span_kind_started` or `trace_span_failed_started` with the window as its index
  condition and the policy as a filter against the set of traces the person reads, computed once per search
  (one pass over `trace`, not over spans), then `Limit`. With `workflow`, the planner reads the workflow's runs by
  `run_workflow_idx`, their traces, and their spans by the primary key, when that is fewer.
- A search's `since` is answered as it was read, so the web app fixes the window when it asks for the first page
  and every later page reads the same one; a 30-day window starts a minute short of 30 days, so a browser clock a
  little ahead of the server's is not refused.

## Audit

| Action | Detail | Issue |
|---|---|---|
| `traces.changed` | `{retentionDays, keepContent}` | #242 |
| `trace_destination.added` | `{name, endpoint, headers: [names], enabled, content, acknowledged}` | #246 |
| `trace_destination.changed` | `{name, endpoint, headers: set\|kept\|removed, enabled, content, acknowledged}` | #246 |
| `trace_destination.removed` | `{name}` | #246 |
| `trace_destination.carried_over` | `{name, enabled, content}`, by the api's start | #246 |
| `workflow.tracing_changed` | `{content, destinations: [names] or null, sampleRatio}` | #259 |

Never a header's value. Spans taken, cut or swept are not audited: they are data, and
`trace.dropped` counts what a cap refused.

## Problems

| Code | Status | Where | Issue |
|---|---|---|---|
| `content_leaves_unacknowledged` | 422 | a destination's `content` | #246 |
| `trace_destination_taken` | 409 | another destination has the name | #246 |
| `trace_destinations_full` | 422 | an add past `MaxTraceDestinations` | #246 |
| `read_only` | 403 | the OTLP endpoint with a grant to read | #243 |
| `search_too_broad` | 422 | a span search past `SpanSearchTimeout` | #261 |

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
- A page for senders' own traces in the web app, and own traces as MCP tools.
- Span links, OTLP metrics and logs, and gRPC OTLP.
- Evaluations or scores on spans.
