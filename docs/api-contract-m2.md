# API contract for M2 and M3: workflows and runs

M2 makes workflows real and M3 runs them end to end:
- a workflow is versioned YAML checked by a JSON Schema and by its own sense
- people write, version and share workflows in the browser
- the runner compiles a workflow into a LangGraph graph, with MCP tools, Python
  code nodes and a language model from a provider an administrator set
- the runner in its sandbox speaks to the control plane over one gRPC stream,
  and the control plane keeps the run's checkpoints, so a run outlives its
  sandbox
- M3 starts runs, hands them tokens and streams their events to the browser

This file is #13. The route table in `backend/internal/httpapi/operations_m2.go`
is the source of truth for the HTTP operations, `proto/rotor/runner/v1/runner.proto`
for the runner protocol and `api/workflow.v1.schema.json` for the spec; this file
says what they mean. Operations carry `pending: true` and answer 501 until their
issue is built.

Conventions are M1's: paths under `/api/v1`; "signed in" and "admin" as there;
a workflow or run the caller may not see is 404, never 403; refusals are
sentences.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | Tables |
  |---|---|---|
  | 00200-00219 | #15 | `workflow`, `workflow_version` |
  | 00220-00229 | #18 | `run` (base), `runner_credential`, `run_event` (base) |
  | 00230-00239 | #19 | `run_checkpoint`, `run_checkpoint_write` |
  | 00240-00249 | #20 | `provider` |
  | 00300-00319 | #22 | the run state machine, the worker's lease and teardown columns on `run`, run visibility, `connection_login.next_path` |
  | 00320-00329 | #23 | `run_token`: which tokens a run holds, never the tokens |
  | 00330-00349 | #24 | event fan-out and retention |

  #14 and #21 need none.
- Packages:
  - Go: `internal/workflow/spec` (#14), `internal/workflows` (#15), `internal/sandbox` with `docker` (#21), `internal/runnerctl` and the generated `internal/runnerpb` (#18), `internal/runs` (#19, #22), `internal/providers` (#20), `internal/cache` on Valkey (#15).
  - Python, in `runner/src/rotor_runner`: `spec.py` (#14), `compile.py` and `mcp.py` (#16), `codenode.py` and `codehost.py` (#17), `control.py` (#18), `checkpoint.py` (#19), and the generated `pb/`.
- Settings, all `ROTOR_` variables:
  - `ROTOR_SANDBOX_BACKEND` (`docker`, development only, #21), `ROTOR_SANDBOX_IMAGE`, `ROTOR_SANDBOX_NETWORK`
  - `ROTOR_RUNNER_ADDR`: where the control plane serves the runner protocol, `:9443` by default (#18)
  - `ROTOR_VALKEY_URL` (#15)
- Limits, in `internal/config`:
  - `MaxSpecBytes` = 256 KiB: one workflow's YAML.
  - `CatalogTTL` = 1 hour: a tool's MCP tool list, per tool build and person.
  - `BootstrapTTL` = 5 minutes, `SessionTTL` = the run's timeout plus 1 hour (#18).
  - `HeartbeatInterval` = 10 seconds, `MissedHeartbeats` = 3 (#18).
  - `MaxEventBytes` = 64 KiB, `MaxCheckpointBytes` = 4 MiB (#18, #19).
  - Code nodes: 30 seconds and 256 MiB unless the node says otherwise, at most 5 minutes and 1 GiB; 64 KiB of output (#17).
- **Decided:**
  - gRPC with protobuf, generated with buf; the Docker sandbox backend comes in M2.
  - Templates are paths into the state, never code, and a router switches on a value.
  - Checkpoints are encrypted with OpenBao transit; since #41 with the run's own data key, as `docs/api-contract-m8.md` says.

  `docs/decisions.md`, 2026-10-18, has the reasons.

## #14 The workflow spec

`api/workflow.v1.schema.json` (JSON Schema 2020-12) is the shape, and
`docs/workflows.md` explains it with examples.

**Top level:**
- `apiVersion: rotor.dev/v1`, `kind: Workflow`, `metadata.name` (lower case and dashes)
- `inputs`: a JSON Schema for what the person gives at the start
- `tools`: bindings by name; `model`; `limits`
- `triggers`: empty; since M6 triggers are records each person makes (`docs/api-contract-m6.md`)
- `entry`, `nodes` and `edges`

**Binding.** `{tool, scopes, allowTools, access, approval}`:
- `tool` names a tool in Rotor by its product name, such as `exciter`.
- `allowTools` lists the MCP tools a run may call through the binding; nothing else is offered.
- `access: read` (the default) offers only tools whose `readOnlyHint` is true.
- `approval: beforeWrite` requires an approval node on every path to a node that writes through the binding.

**Nodes:**

| Type | Fields | Becomes |
|---|---|---|
| `agent` | `prompt`, `tools` (binding names), `output.schema` | a tool-calling agent on the workflow's model |
| `tool` | `tool` (binding), `call` (MCP tool), `arguments` | one MCP call; a string argument may hold `{{ inputs.x }}` or `{{ nodes.<id>.x }}` |
| `code` | `source`, `output.schema`, `tools`, `timeout`, `memory` | `def run(state, ctx) -> dict` in a subprocess |
| `approval` | `message` | an interrupt the run waits at |
| `router` | `on` (a state path), `cases` (value to node), `default` | conditional edges |

**State:**
- The state is `{inputs, nodes: {<id>: <output>}}`, plus the agent's messages.
- A node's output goes under its id.
- A router's next nodes come from its cases, never from `edges`.
- A node without an outgoing edge ends the run.

**Validation**, by Go when saving and by the runner when starting:
1. YAML that parses, at most `MaxSpecBytes`.
2. The JSON Schema.
3. The spec's own sense:
   - node ids unique; edges and cases name known nodes
   - every node reachable from `entry`
   - routers have no `edges` of their own
   - tool nodes and agents name declared bindings, and `call` is in the binding's `allowTools`
4. When saving, against the tools as the saver sees them:
   - the bound tool exists and offers the scopes
   - an `access: read` binding allows only read-only tools
   - `approval: beforeWrite` holds

**Problems** carry a JSON pointer, line and column (from 1), a code and a sentence.

**Golden fixtures** in `api/workflow-fixtures/` are validated by both Go and Python, which must agree.

## #15 Workflows

| Operation | Needs | Answers |
|---|---|---|
| `GET /workflows` | signed in | `{workflows}` the caller may see; `?archived=true` for archived ones |
| `POST /workflows` | signed in | `{specYAML, comment?}`; 201 `{workflow, version}`; 422 `{problems}` in the envelope's fields |
| `GET /workflows/{workflowID}` | may see | `{workflow, version}` |
| `PUT /workflows/{workflowID}` | owner | `{specYAML, comment, baseVersion}`; 409 when `baseVersion` is not current |
| `DELETE /workflows/{workflowID}` | owner or admin | 204; archives |
| `GET /workflows/{workflowID}/versions[/{versionNumber}]` | may see | the versions, or one with its YAML |
| `GET /workflows/{workflowID}/diff?from=&to=` | may see | hunks of lines with `equal`, `insert` and `delete` |
| `PUT /workflows/{workflowID}/sharing` | owner | `{sharing: private\|groups\|everyone, groups}` |
| `POST /workflows/validate` | signed in | `{valid, problems}` without saving |
| `GET /schemas/workflow.v1.json` | signed in | the schema, for the editor |
| `GET /tools/{toolID}/catalog` | signed in | `{tool, build, tools}`; 409 when the caller is not connected |

- **Who may see a workflow:**
  - its owner
  - everyone when `sharing` is `everyone`
  - a person in one of `sharedGroups` (their realm groups, as of their last sign-in) when it is `groups`
  - administrators

  Only the owner saves and shares; the database refuses anyone else.
- **Versions** are numbered from 1 without gaps and never change. A run pins one.
- **Sharing** shares the workflow, never the owner's tokens: a run acts as whoever starts it.
- **The catalog:**
  - It is the tool's `tools/list`, called with an access token minted for the caller.
  - It is cached per tool, tool build (from the tool's `/meta`) and person for `CatalogTTL`.
  - It gives each tool's name, description, input schema and the `readOnlyHint` and `destructiveHint` it declares.
- **Audit:** `workflow.created`, `workflow.changed` (version), `workflow.shared` (sharing, groups), `workflow.archived`.
- **As built (#15):**
  - A 422 lists each problem's message in the envelope's `fields`, by JSON pointer. The editor places problems by line and column through `POST /workflows/validate`.
  - The owner's and each version author's names are kept on the rows, because a person sees only their own row of `app_user`.
  - The catalog mints a token from the caller's whole consent and revokes it after the read.
  - The cache key is the tool, its build (`version+commit` from its `/meta`) and the person.
  - Archived workflows refuse saves with 409.

## #16 and #17 The runner

- **Compiling.** The spec becomes a LangGraph `StateGraph`:
  - **agent:** `langchain.agents.create_agent` on `init_chat_model`.
  - **tool:** one call through langchain-mcp-adapters over streamable HTTP, with `Authorization: Bearer` set per call from the run's token for that binding. Tokens never enter the state.
  - **router:** `add_conditional_edges`.
  - **approval:** `interrupt({message})`.
- **Limits:**
  - `maxSteps` is the recursion limit, `timeout` bounds the whole run, and `maxTokens` counts the model's usage.
  - Exceeding one ends the run with reason `max_steps`, `timeout` or `max_tokens`.
- **Code nodes:**
  - `def run(state, ctx) -> dict` runs in a child process with an empty environment, the node's memory limit, a CPU limit and its timeout.
  - It may use only what the runner image has installed.
  - `ctx.call_tool(binding, name, arguments)` goes back through the runner, which enforces the binding.
  - The result must be JSON matching `output.schema`.
  - Its stdout and stderr become `log` events, capped.

## #18 The runner protocol

`RunnerControlService.Connect` is one bidirectional stream per run. The
control plane serves it on `ROTOR_RUNNER_ADDR` over TLS, with a certificate from
OpenBao's PKI. The sandbox is started with the control plane's address, the CA
and a bootstrap token, and dials out.

- **Hello:**
  1. The first message is `Hello` with the bootstrap token: single use, expiring after `BootstrapTTL`, bound to one run.
  2. The answer is `HelloAck` with the run id and a session token.
  3. A dropped stream reopens with `Hello` carrying the session token, which the next `HelloAck` replaces.

  A used, expired or unknown credential ends the stream with `UNAUTHENTICATED`.
- **Delivery:**
  - Each side numbers the messages that must arrive (`seq` from 1) and acknowledges what it has handled (`ack`).
  - The control plane stores an event before acknowledging it. It ignores a sequence number it has, and ends the stream on a gap.
  - After a reconnect each side resends what the other did not acknowledge.
- **Heartbeats** every `HeartbeatInterval`. After `MissedHeartbeats` missed, the sandbox is lost: the run records `runner.lost`, and #22 decides what follows.
- **Runner to control plane:**
  - `Event` (a type below, a redacted JSON payload)
  - `TokenRequest` (a fresh token for a binding)
  - `Heartbeat`
  - `Done` (`succeeded`, `failed` with a reason, `cancelled`, or `interrupted` when it waits, checkpointed)
  - the checkpoint requests of #19
- **Control plane to runner:**
  - `StartRun` (the pinned spec, the inputs, the resolved bindings, the model, and `resume` with its answer for a run that has checkpoints)
  - `Resume`, `Cancel`, `TokenGrant`, `SecretGrant`
  - the checkpoint answers

  In M2 a grant carries the token itself; M3 sends an OpenBao wrapping token instead (#23).

**As built** (#18):
- The service is `RunnerControlService` (buf's naming rules), its stream `Connect(ConnectRequest) returns ConnectResponse`.
- **Numbering:**
  - `run_event.seq` is numbered by the control plane in the order stored, so its own events (`runner.lost`) share the sequence.
  - `run.runner_seq` keeps the runner's own numbering, for ignoring resends and refusing gaps.
- **What carries a number:** events and `Done` from the runner, and `StartRun`, `Resume` and `Cancel` from the control plane, which `run_control` keeps until acknowledged.
  - Grants and checkpoint answers carry none: a token is asked for again, never stored, and a checkpoint request is sent again after a reconnect.
- `TokenGrant.error` says why there is no token, so a run is never left waiting.
- `StartRun` gives each binding's MCP address as a sandbox reaches it (the `ROTOR_TOOLS_BACKCHANNEL` rewrite).
- A runner whose credential is refused exits with status 2.

## #19 Checkpoints

- The runner's checkpointer stores and loads over the stream: `CheckpointGet`, `CheckpointList`, `CheckpointPut`, `CheckpointWrites`. Values are LangGraph's serialized `(type, bytes)`, loaded with strict msgpack.
- **Storage:**
  - The control plane keeps checkpoints in `run_checkpoint` and their pending writes in `run_checkpoint_write`, under the run the stream is bound to.
  - The sandbox has no database access.
  - Each value is sealed with the run's data key, which the OpenBao transit key `rotor-run-data` wraps (#41; until then the transit key `rotor-checkpoints` encrypted each value). A checkpoint above `MaxCheckpointBytes` is refused and the run fails, saying so.
- **No tokens in checkpoints:** tokens and keys live in the runner's memory and are never in the state, so a checkpoint cannot hold one; a test proves it.

**As built** (#19):
- `run_checkpoint` keeps `checkpoint_type` and `metadata_type` in clear (LangGraph's tags) beside the sealed values. `run_checkpoint_write` does the same for each write.
- Putting a checkpoint again changes nothing. Ordinary writes are kept once, special ones (negative `idx`) replace the last.
- `runnerctl.Resume(run, answer)` stores the answer and moves a waiting run to `starting`. The next sandbox's Hello then gets a fresh `StartRun` with `resume`. Since #31 a decision on an approval does this, and `StartRun` names the interrupt it answers (`docs/api-contract-m5.md`).
- A sandbox numbers its messages on from the `ack` of its `HelloAck`, so a resumed run's numbering continues.
- The runner's checkpointer loads with the serializer's safe types alone (`allowed_msgpack_modules=None`).

## #20 Model providers

| Operation | Needs | Answers |
|---|---|---|
| `GET /providers` | admin | `{providers}`, with `hasKey` and never the key |
| `POST /providers` | admin | `{name, kind, baseURL?, apiVersion?, models, defaultModel?, enabled, apiKey?}` |
| `PUT /providers/{providerID}` | admin | as POST; an empty `apiKey` keeps the one held |
| `DELETE /providers/{providerID}` | admin | 204 |
| `POST /providers/{providerID}/test` | admin | `{ok, message, models}` from the provider's model list |
| `GET /models` | signed in | the models a workflow may name, and the default |

- **Kinds:** `anthropic`, `openai`, `azure_openai`, `openai_compatible` (any server speaking OpenAI's chat API, such as Ollama). The runner builds each with `init_chat_model`.
- **Keys:** they live in OpenBao at `rotor/providers/{id}/key` and reach the sandbox in a `SecretGrant`, never in a spec or an environment file.
- **Choosing a model:** a workflow's `model` must be in the allowed list; without one it gets the default.
- **Audit:** `provider.changed` (added, changed, removed), never the key.

**As built** (#20):
- **Default model:** providers carry `isDefault`; one provider holds the installation's default model.
- **Model resolution:** at a run's first Hello, `runnerctl` resolves the workflow's model and puts it in `StartRun.model`, with the base URL as a sandbox reaches it. A workflow whose model is not allowed starts without one, and its agents fail with `model_missing`.
- **Key delivery:** the key comes in a `SecretGrant` after every Hello, since a grant is never stored to be resent. The runner waits for it before building `init_chat_model`.
- **Egress in development:** development sandboxes reach no internet, so their agents reach the tool double's stub model and models on the stack's network, not Anthropic or OpenAI themselves. In a cluster the chart's egress rules (#27) allow a provider by hostname.
- **Stub model:** the tool double serves `/t/{tenant}/llm/v1/models` and `/llm/v1/chat/completions` in OpenAI's shapes, streamed or not, answering from a script the tenant sets (`llmScript`) behind an optional key (`llmKey`).

## #21 Sandboxes

```go
type Backend interface {
	Spawn(ctx context.Context, spec Spec) (ID, error)
	Destroy(ctx context.Context, id ID) error
	Status(ctx context.Context, id ID) (Status, error)
	List(ctx context.Context, labels map[string]string) ([]Sandbox, error)
}

type Spec struct {
	RunID  uuid.UUID
	Image  string
	Env    map[string]string // the control plane's address, its CA and the bootstrap token
	Limits Limits            // memory, CPUs, processes
	Labels map[string]string
}
```

- **The Docker backend:**
  - It runs only in development, through the Docker socket.
  - Its containers are read-only with a tmpfs `/tmp`, have no capabilities, run as a non-root user, and are labelled `rotor.dev/sandbox`, `rotor.dev/stack` and `rotor.dev/run`.
  - They join one internal network, which reaches the api and the tools and nothing else.
  - At start-up the api destroys sandboxes of its stack that no run owns.
- **Kubernetes and gVisor** come in M4.
- **As built (#26):**
  - **Settings:**
    - `ROTOR_SANDBOX_BACKEND=kubernetes` with `ROTOR_SANDBOX_NAMESPACE` (`rotor-sandboxes`), `ROTOR_SANDBOX_RUNTIME_CLASS` (`gvisor` when unset; set empty for none, which needs `ROTOR_SANDBOX_ALLOW_RUNC=true`, refused in production), `ROTOR_SANDBOX_SERVICE_ACCOUNT` (`rotor-sandbox`, token-less) and `ROTOR_SANDBOX_KUBECONFIG` for tests outside a cluster.
    - `ROTOR_RUNNER_HOST` names the host the runner certificate carries and sandboxes dial.
    - `ROTOR_OPENBAO_AUTH=kubernetes` signs the control plane in with its pod's ServiceAccount token.
  - **`Spec` gains two fields:**
    - `SecretEnv`, the bootstrap token, which the Kubernetes backend keeps in a Secret the pod owns.
    - `Deadline`, the run's timeout plus `SandboxDeadlineGrace`, as the pod's `activeDeadlineSeconds`.
  - **The worker's rights:** create, get, list, watch, update and delete on pods and secrets in the sandbox namespace (`deploy/kube/sandboxes.yaml`; the chart renders the same).

## #22 to #24 Runs (M3)

| Operation | Needs | Answers |
|---|---|---|
| `POST /workflows/{workflowID}/runs` | may see | `{inputs, version?}` with `Idempotency-Key`; 201 `{run}`, 200 for a repeated key, 409 naming the tools to connect, 422 inputs against the schema |
| `GET /runs` | signed in | runs the caller started, runs of workflows they own; admins all; filtered by `workflow`, `status`, `mine` |
| `GET /runs/{runID}` | may see | `{run}` |
| `POST /runs/{runID}/cancel` | the runner's person, the workflow's owner or an admin | `{run}`; 409 when it has ended |
| `GET /runs/{runID}/events` | may see | `text/event-stream` |

- **States and their moves:**
  - queued to starting or cancelled
  - starting to running, failed or cancelled
  - running to waiting, succeeded, failed or cancelled
  - waiting to starting (resumed), cancelled or expired

  A trigger refuses any other move. A worker claims queued runs with `FOR UPDATE SKIP LOCKED` and takes up runs again after a restart.
- **Events:**
  - Each event is a row of `run_event` with its sequence number as the SSE id. `Last-Event-ID` replays the rest. Valkey fans events out across api replicas.
  - Payloads are capped at `MaxEventBytes` and redacted.

  | Type | Payload |
  |---|---|
  | `node.started` | `{node, type}` |
  | `node.finished` | `{node, ok, error?, outputSummary}` |
  | `llm.output` | `{node, text}`, in chunks |
  | `tool.called` | `{node, binding, tool, arguments, resultSummary, ok}` |
  | `log` | `{node, stream, text}` |
  | `interrupt` | `{node, message}` |
  | `run.finished` | `{status, reason, output}` |
  | `runner.lost` | `{since}` |

- **As built (#22):**
  - **The extra move:** besides the moves above, a running run whose sandbox was lost goes back to `starting`, and a fresh sandbox resumes it from the last checkpoint. After `MaxRunAttempts` (3) sandboxes it fails with `runner_lost`.
  - **Worker reasons:** `sandbox_start` (no sandbox said Hello within `StartTimeout`), `runner_lost`, `timeout` (past the run's own timeout plus `RunDeadlineGrace`), `waited_too_long` (expired after `WaitingTTL`, 7 days). A `run.finished` event says each with a `message`.
  - **Who sees a run:** whoever started it, the workflow's owner and administrators, by the database's policy. The same people may cancel.
  - **Refusals:**
    - A missing connection answers 409 `connections_needed`. Its `fields` map each tool's name to its id, empty for a tool the installation does not have.
    - Inputs the schema refuses answer 422 `inputs_invalid`, with `fields` keyed `inputs/<pointer>`.
    - A key reused for another workflow answers 422.
  - **Replies:**
    - `runResponse` carries `inputs`, `output`, `startedAt` and `ended`.
    - `versionResponse` carries the version's `inputs` schema and its `bindings`, for the run dialog.
  - **The worker** is `cmd/worker`, its own compose service; it alone holds the Docker socket. A cancel reaches the runner through `run_control`, with Valkey pub/sub on `runctl:<run>` waking the api that holds the stream.
  - **Returning after connecting:** `POST /tools/{toolID}/connection?next=<path>` returns the browser to that path of Rotor's once connected, with `connected=<toolID>`.
- **As built (#24):**
  - **The stream:** `GET /runs/{runID}/events` answers 404 before any stream for a run the caller may not see. Otherwise it is `text/event-stream`:
    - `retry: 2000` first, then each event as `id: <seq>`, `event: <type>` and `data: {seq, type, node, payload, at}`.
    - A comment every `EventHeartbeat` (15 seconds) while nothing happens.
    - It ends once the run has ended and every event is sent. A waiting run's stream stays open.
  - **Resuming:** `Last-Event-ID` (or `?after=`) sends only the events after it; a browser's EventSource sends the header itself when it reconnects.
  - **Fan-out:** whoever stores an event (the api holding the runner's stream, a cancel, the worker) publishes on Valkey `runevents:<run>` after commit. Every replica's streams listen there, and also read every `EventPoll` (2 seconds) in case a signal was lost.
  - **Lasting connections:** the stream is exempt from the request timeout and lifts the server's write deadline for itself.
  - **The graph:** `GET /runs/{runID}` carries `graph {entry, nodes [{id, type}], edges [{from, to, label?}]}` from the pinned version. A router's cases are labelled edges, and its default is labelled `default`. Lists leave it out.
  - **Who sees a run:** #22's policy decides who may see a run, for the list and the stream alike.
- **Tokens (#23):** at `StartRun` and on `TokenRequest` the control plane mints per binding with `connections.Mint` and wraps the result with OpenBao response wrapping (single use, at most 2 minutes). The runner unwraps it. At the end every access token is revoked at its tool.

- **As built (#23):**
  - **Grants:** `TokenGrant` and `SecretGrant` carry only `wrapping_token`; `access_token` and `api_key` are deprecated and never sent.
    - A token's wrapping holds `access_token` and `expires_at`, a key's holds `api_key`.
    - Both are wrapped for `WrapTTL` (2 minutes) and open once.
  - **Unwrapping:** the runner unwraps at `ROTOR_OPENBAO_ADDR`, which the worker hands every sandbox. OpenBao joins the internal `sandbox` network for that; a sandbox holds no OpenBao credential but each wrapping token.
  - **Delivery** (`connections.Deliver`) refuses a run that is not `starting` or `running`, and a person whose account was switched off. The runner's `TokenRequest` then gets the reason in `TokenGrant.error`, and the run fails with `tool_error`, saying so.
  - **Keeping tokens until revoked:** each delivered token is kept in OpenBao at `rotor/runs/{run}/tokens/{id}` until revoked. `run_token` records only which tool and when it expires.
  - **Revoking:**
    - The worker's teardown revokes every token of an ended run at its tool (RFC 7009), forgets it and records `token.revoked`. A tool that does not answer is tried again on the next pass, until the token would have expired anyway.
    - Ending a connection, removing a tool or switching a person off also revokes the tokens their runs hold at that tool.
  - **Redaction:** the runner cuts every token and key it holds from its events. The control plane then redacts bearer values, known token prefixes and fields named like `token`, `secret`, `password`, `authorization` or `api_key` before storing an event.

- **As built (#25):**
  - **The proof:** `backend/test/suite_run_test.go` runs `examples/draft-requirements.yaml` against the real Exciter in the suite job, with the tool double's model; sandboxes reach no provider on the internet yet.
  - **The suite's networks:** the worker joins the suite's network so it can revoke tokens at Exciter. Exciter's web joins the sandboxes' internal network as `suite-exciter-web`, the address the api rewrites Exciter's MCP endpoint to.
  - **The tool double:**
    - A scripted model turn may carry `delayMs` to hold its answer back.
    - A tenant made on first use as `/t/<product>--<rest>` answers as `<product>`.
- **As built (#28, M4):**
  - **Waiting runs keep no token:** when a run starts waiting, the worker drops its sandbox and then revokes every token the run was handed, at the tools. A revocation that fails is tried again on the next pass. A resume starts a sandbox from the checkpoint, and that sandbox is handed fresh tokens.
  - **Worker metrics:** the worker serves `/metrics` on `ROTOR_WORKER_METRICS_ADDR` (`:9464`, never published) in Prometheus's text format:

    | Metric | Type | Labels | What it counts |
    |---|---|---|---|
    | `rotor_sandboxes_live` | gauge | none | sandboxes alive at the last pass |
    | `rotor_sandboxes_reaped_total` | counter | `reason` | sandboxes the worker destroyed |
    | `rotor_runs` | gauge | `status` | runs, read at each scrape |

    The `reason` label is one of `waiting`, `ended`, `orphan`, `unstarted`, `lost` or `deadline`.

- **As built (#29):**
  - **The chart:** `deploy/charts/rotor` renders the settings above for a cluster.
    - `ROTOR_SANDBOX_BACKEND=kubernetes`, the namespace `<release namespace>-sandboxes` and `ROTOR_RUNNER_HOST` as the api's Service.
    - `ROTOR_OPENBAO_AUTH=kubernetes`, with the role bound to the api's and the worker's accounts by the chart's OpenBao Job.
  - **Tests:** `tests/test-helm.sh` checks the chart in the gate, and `make test-chart-install` installs it on kind.

- **As built (#27):**
  - **Egress in a cluster:** the chart's `sandbox.egress` renders a default-deny NetworkPolicy for pods labelled `rotor.dev/sandbox: "1"`. It allows DNS, the api's runner port and OpenBao, plus a CiliumNetworkPolicy with `toFQDNs` for `sandbox.egress.hosts` (or address ranges without Cilium).
  - **Egress in development:** the compose stack keeps its internal network, which reaches the api, OpenBao and the tool double.

## What is left for later

- Approvals people answer in the browser: `docs/api-contract-m5.md` (#30). Triggers: `docs/api-contract-m6.md` (#33); the spec keeps `triggers` reserved and empty.
- Narrowing tokens at the tool once tools offer token exchange (Cloudster1/Exciter#235).
