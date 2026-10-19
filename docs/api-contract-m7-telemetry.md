# API contract for M7: traces, metrics and LangSmith

#37 makes each run one trace across the api, the worker and the runner, counts what operators watch, and lets an
administrator send runs' traces to LangSmith:
- the api and the worker trace requests (otelhttp), database statements (pgx) and a run's sandbox steps, and send
  them over OTLP/HTTP to a collector
- the runner traces the run, each node, each model call and each MCP call under the run's trace, and sends its spans
  to the control plane over the runner protocol, which exports them for it, so a sandbox reaches no collector
- the worker and the api serve Prometheus metrics
- LangSmith is off by default; an administrator turns it on with a key kept in OpenBao

The route table in `backend/internal/httpapi/operations_m7_telemetry.go` is the source of truth for the HTTP
operations and `proto/rotor/runner/v1/runner.proto` for the runner protocol; this file says what they mean.

## Migrations, packages and settings

- Migrations:

  | File | What |
  |---|---|
  | `00720_run_trace.sql` | `run.traceparent`, the run's W3C trace context, set when the run is started |
  | `00721_langsmith.sql` | `langsmith_setting`, one row: enabled, project, when the key was set, who changed it |

- Packages: `internal/observability` (tracing, the pgx tracer, the runner's spans), `internal/metrics` (on
  prometheus/client_golang), `internal/langsmith`; in the runner, `telemetry.py`.
- Settings, all `ROTOR_` variables:
  - `ROTOR_OTLP_ENDPOINT`: the collector's OTLP/HTTP traces URL, `/v1/traces` included; empty keeps no traces.
  - `ROTOR_TRACE_SAMPLE_RATIO`: the share of new traces kept, 1 by default; a trace a caller began keeps its own
    decision.
  - `ROTOR_API_METRICS_ADDR`: where the api serves `/metrics`, `:9465` by default; `ROTOR_WORKER_METRICS_ADDR` stays
    the worker's.
  - `ROTOR_LANGSMITH_ENDPOINT`: LangSmith's OpenTelemetry endpoint, `https://api.smith.langchain.com/otel/v1/traces`
    by default.
- Limits, in `internal/config`:
  - `MaxRunnerSpansPerBatch` = 256, `MaxRunnerSpanAttributes` = 32, `MaxSpanAttrChars` = 512, `MaxSpanNameChars` =
    128: what one runner message may carry; more is dropped or cut.
  - `LangSmithSettingTTL` = 30 seconds: how long an api replica trusts what it read of the setting.

## One trace per run

- **Where it begins:** `POST /workflows/{workflowID}/runs` continues a `traceparent` the caller sends, or begins a
  trace. Starting the run opens a `run.start` span and keeps its context in `run.traceparent`; a trigger's firing
  begins its own.
- **The worker:** `run.start_sandbox` and `run.teardown` join the run's trace.
- **The runner:** `StartRun.traceparent` (9) hands the trace to the sandbox. Without a sampled traceparent the runner
  makes no spans at all. With one it opens:
  - `run`, a child of the traceparent
  - `node <id>`, with `rotor.node` and `rotor.node.type`; an interrupt for an approval is marked `rotor.interrupted`,
    not an error
  - `llm <model>`, with `gen_ai.request.model`, `gen_ai.usage.input_tokens` and `gen_ai.usage.output_tokens`
  - `mcp <call>`, with `rotor.binding` and `mcp.tool`
- **Spans over the runner protocol:** `ConnectRequest.spans` (19) is a `SpanBatch` of `RunnerSpan`s, sent with seq 0:
  never stored or acknowledged, and lost with a stream that drops, which costs a trace a few spans and never a run
  anything. The runner flushes them before `Done`.
- **What the control plane checks:** it keeps only spans of the run's own trace, at most `MaxRunnerSpansPerBatch` per
  message, cuts names and attributes, and redacts every value again with the redactor events pass, since the sandbox
  runs code the workflow's author wrote. It exports them as the service `rotor-runner`.
- **Prompts and answers:** `StartRun.trace_content` (10) is set while LangSmith is on. Only then do model spans carry
  `gen_ai.prompt.<n>.role`, `gen_ai.prompt.<n>.content` (the last four messages) and `gen_ai.completion.0.content`,
  each cut to 512 characters and redacted by the runner and again by the control plane. Those attributes go to
  LangSmith alone; the collector never receives them.

## Metrics

| Metric | Kind | Served by | What |
|---|---|---|---|
| `rotor_runs{status}` | gauge | worker | runs by state, read at each scrape |
| `rotor_sandboxes_live` | gauge | worker | sandboxes alive at the last pass |
| `rotor_sandboxes_reaped_total{reason}` | counter | worker | sandboxes destroyed, by why |
| `rotor_run_duration_seconds{status}` | histogram | worker | from a run's start to its end, counted at teardown |
| `rotor_trigger_fires_total{outcome}` | counter | worker | triggers' firings that ended |
| `rotor_token_mint_failures_total{reason}` | counter | api | tokens a run asked for and did not get |

The `reason` of a mint failure is `not_connected`, `reconnect`, `needs_registration`, `scope_not_consented`,
`unreachable`, `run_not_live`, `switched_off` or `other`. Both processes also serve Go's and the process's own metrics.

## LangSmith

| Operation | Needs | Answers |
|---|---|---|
| `GET /settings/langsmith` | admin | `{enabled, project, keySet, keySetAt, changedBy, updatedAt, endpoint}`; never the key |
| `PUT /settings/langsmith` | admin | `{enabled, project, key?, acknowledgeDataLeaves}`; the setting |
| `DELETE /settings/langsmith/key` | admin | the setting, turned off |

- **Turning it on** needs a key, given now or held already, and `acknowledgeDataLeaves: true`. Without the
  acknowledgement the answer is 422 `data_leaves_unacknowledged`; without a key, 422 `langsmith_key_missing`. A key
  of another shape, or a project over 128 characters, answers 422 with `fields`.
- **The key** goes to OpenBao at `rotor/settings/langsmith/key` before the setting changes, and is never answered,
  logged or put in an event. Leaving `key` out keeps the one held; forgetting it turns LangSmith off.
- **What is sent:** while it is on, the api sends each run's runner spans, with their prompts and answers, to
  `ROTOR_LANGSMITH_ENDPOINT` over OTLP/HTTP, with the key as `x-api-key` and the project as `Langsmith-Project`. The
  sandbox never holds the key and reaches no LangSmith host; the api's own spans stay with the collector.
- **Audit:** `langsmith.changed` with `{enabled, project, key: set|kept|removed, acknowledged}`, never the key.
- **In development** `ROTOR_LANGSMITH_ENDPOINT` points at the tool double's stand-in, which keeps what reaches it for
  the tests (`GET /control/langsmith` on its control port).

## The observe profile

`make observe` starts the stack with an OpenTelemetry collector (`otel/opentelemetry-collector-contrib`), Jaeger behind
it and Prometheus scraping the api and the worker, on `JAEGER_PORT` and `PROMETHEUS_PORT`. The gate's stack always
starts it, and an integration test follows one run from the api through the worker to the runner's node, model call
and MCP call in Jaeger.

## What is left for later

- Metrics of the runner itself, and OpenTelemetry metrics and logs beside the traces.
- Sampling decided per workflow.
