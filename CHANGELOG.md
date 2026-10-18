# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Rotor installs with one Helm chart (#29), adapted from Exciter's: the api,
  the worker and the web, the migrations, the sandbox namespace under the
  restricted Pod Security standard with the worker's narrow rights, a Job
  that configures OpenBao for the release, and Postgres and Valkey for a
  trial. The chart is linted and rendered against golden files on every
  change, and installed into a kind cluster, where a run executes in a
  sandbox pod, nightly and on pull requests that touch it.
  `docs/deployment.md` says how.
- A run waiting for an answer keeps nothing alive (#28): besides its
  sandbox, the tokens it was handed are now revoked at the tools as soon as
  it starts waiting, and a resume starts with fresh ones. The worker serves
  metrics for operators: the sandboxes alive, those it destroyed and why,
  and the runs by status.
- Runs can execute on Kubernetes (#26). Each sandbox is its own pod under
  gVisor, non-root, read-only, without capabilities or a ServiceAccount
  token, in a namespace under the restricted Pod Security standard; the
  worker may manage pods there and nothing else. In a cluster the control
  plane signs in to OpenBao with its own ServiceAccount, and sandboxes dial
  the api by a configurable name. A kind cluster tests it all, nightly and
  on pull requests that touch it.
- The first run is proven end to end against the real Exciter (#25). In the
  suite, alice runs `examples/draft-requirements.yaml` and the requirement
  appears in Exciter as hers, marked as written through Rotor; dave, who may
  only read the project, runs the same workflow and Exciter refuses the
  write, which his run reports. Afterwards both sandboxes are gone and
  Exciter refuses the tokens the runs held. The example ships in
  `examples/`, checked by both the control plane and the runner.
- People follow a run live (#24). Its page draws the workflow's graph and
  lights it up node by node. Beside it, a timeline says what happened: steps
  started and finished, each tool call with its arguments and answer, what
  the model wrote, and links to what the run created in the tools. A dropped
  connection picks up where it left off. The Runs page lists the runs you may
  see, filtered by workflow, state and the runs you started. The stream works
  through any api replica.
- A run's tokens reach only its sandbox and end with it (#23). Each token and
  the provider key arrive wrapped in a single-use OpenBao token that only the
  sandbox opens, within two minutes; the runner keeps them in memory and cuts
  them from everything it reports, and the control plane redacts events
  again. When the run ends, every token it was handed is revoked at its tool;
  ending a connection or switching a person off revokes them at once, and a
  switched-off person's run gets no new token.
- People start a run of a workflow and it runs as them (#22). Run on a
  workflow opens a dialog that asks for its inputs, says which tools the run
  acts in and offers connecting those you have not, coming back to the
  workflow. A new worker starts each run's sandbox, replaces one that dies
  so the run carries on from its last checkpoint, ends runs past their time
  and cleans up after them; it alone holds the Docker socket now. Runs can
  be cancelled by whoever started them, the workflow's owner or an
  administrator, and a retried start never starts a run twice.
- Administrators choose the language models workflows may use (#20). In
  Settings they add Anthropic, OpenAI, Azure OpenAI or any OpenAI-compatible
  server (such as Ollama), list its models, pick the installation's default
  and test the connection; keys go to OpenBao and nowhere else. A run's
  agents get the workflow's model, or the default, and the key reaches the
  run's sandbox over its channel, never in a file or the environment. The
  tool double gains a scripted, OpenAI-compatible model for tests.
- A run outlives its sandbox (#19). The runner keeps no checkpoint of its
  own: each goes to the control plane over the runner's channel, sealed
  with OpenBao's transit key before it reaches Postgres. A run waiting for
  an answer can lose its sandbox and pick up in a fresh one from the last
  checkpoint, without repeating what it already did. Checkpoints never hold
  a token, and one larger than 4 MiB fails the run, saying so.
- The control plane and a run's sandbox talk over one channel (#18). The
  runner in the sandbox dials the api's runner port over TLS, with a
  certificate from OpenBao's PKI, using a bootstrap token that works once
  and expires within minutes; the api answers with the run's pinned workflow
  and its tools, mints tokens as the run asks for them, and keeps every event
  the runner reports. A dropped link or a restart loses nothing: both sides
  number what they send and resend what the other did not acknowledge, and a
  runner that stops answering is recorded as lost.
- People write, version and share workflows in the browser (#15). The
  Workflows page lists your workflows and those shared with you; the editor
  completes and explains the YAML from the workflow schema and marks each
  problem the server finds on its line as you type, including what only your
  tools can tell, such as a binding that allows a writing tool but says it
  only reads. Every save is a new version that never changes; a save based on
  an older version is refused rather than overwriting a colleague. Versions
  are compared line by line, YAML is imported and exported, and a tool picker
  lists what your connection may call in each tool with its read-only and
  destructive hints. Sharing with your realm groups or everyone lets others
  read and run a workflow as themselves; it never shares your access.
- Runs get sandboxes (#21): in development the api starts each run's runner
  as a container on the host's Docker daemon, read-only with a small `/tmp`,
  without capabilities, as a non-root user, within memory, CPU and process
  limits, on an internal network that reaches the api and the tools and not
  the internet. At start the api destroys sandboxes left from before.
  `make up` builds the runner image.
- Workflows can run a small Python step (#17): a code node's
  `run(state, ctx)` runs in its own process with none of the run's tokens or
  keys in its environment, its own memory and time limits (256 MiB and 30
  seconds unless it says otherwise, at most 1 GiB and 5 minutes) and no file
  writes. It calls tools only through `ctx.call_tool`, which the runner
  checks against the node's bindings; it must return JSON matching its
  output schema, and what it prints becomes capped log events.
- People write, version and share workflows in the browser (#15). The
  Workflows page lists your workflows and those shared with you; the editor
  completes and explains the YAML from the workflow schema and marks each
  problem the server finds on its line as you type, including what only your
  tools can tell, such as a binding that allows a writing tool but says it
  only reads. Every save is a new version that never changes; a save based on
  an older version is refused rather than overwriting a colleague. Versions
  are compared line by line, YAML is imported and exported, and a tool picker
  lists what your connection may call in each tool with its read-only and
  destructive hints. Sharing with your realm groups or everyone lets others
  read and run a workflow as themselves; it never shares your access.
- The runner turns a workflow into a LangGraph graph (#16): agents built on
  the workflow's model with the tools their bindings allow (only tools that
  say they only read, unless a binding may write), single MCP calls with
  arguments filled from the run's state, routers, and approvals that wait.
  Every MCP call carries the run's token for its binding, which never enters
  the run's state. A run that passes its step, time or token limit ends and
  says which; the runner reports each node starting and finishing, the
  model's text and each tool call as events.

### Fixed

- Rotor reads a workflow's YAML as YAML 1.2, as the runner does (#67): a
  scope such as `mcp:read` inside an inline mapping is text, not a nested
  mapping, and a golden example in flow style holds both sides to it. The
  refusals for an unknown tool and a missing connection now say what to do.
- The tool test double answers `whoami` in the shape Exciter's tool list
  declares, which MCP clients check, and a contract test keeps it so.
- Workflows have a written form (#14): versioned YAML, `rotor.dev/v1`, with
  inputs, tool bindings that say what a run may call, a model, limits, and
  agent, tool, code, approval and router nodes. Rotor checks it against
  `api/workflow.v1.schema.json` and its own sense, and names each problem by
  line and column; the runner checks it the same way, and both are held to
  the same examples. `docs/workflows.md` explains it with three examples.
- The plan for workflows and runs is agreed (#13): `docs/api-contract-m2.md`
  settles the workflow and run operations, the run states, the live events,
  the sandbox interface and the runner protocol, which `proto/` defines and
  `make proto` generates for Go and Python; `api/workflow.v1.schema.json` is
  the workflow spec's first schema. The new endpoints answer 501 until they
  are built.
- The control plane mints a run's access token from a person's connection
  (#12). Each run gets its own, never stored or shared, within what the
  person consented to; asking for more is refused, naming what is missing.
  Only one refresh runs per connection at a time and the rotated refresh
  token is in OpenBao before the access token is used, so twenty runs at
  once never end the grant. A grant the tool ended marks the connection
  "connect again", and one run's token can be revoked alone.
- People connect their accounts in the tools (#11). The Connections page
  lists every tool; connecting sends the browser to the tool's own consent
  page and back, with PKCE, a state bound to the session and the tool's MCP
  endpoint named as the resource. The refresh token goes to OpenBao and the
  access token is dropped. The page shows what was allowed, when it was
  connected and last used, and disconnecting revokes the grant at the tool.
  Removing a tool or switching a person off ends their connections too.
- Administrators add the tools Rotor acts in (#9). Settings lists every
  tool with what it offers; an administrator adds one by the address it is
  opened at, and Rotor reads its MCP endpoint and authorization server and
  registers itself there as an OAuth client, keeping the client secret in
  OpenBao. A tool without OAuth is refused with a sentence saying what it
  lacks; one that forgot Rotor is registered again with a click; removing a
  tool forgets Rotor's client there.
- Rotor can be tested against tools without mocking them (#8). `make up`
  starts a test double that behaves as Exciter does as an OAuth authorization
  server and MCP endpoint, as many independent tools as tests need, kept
  honest by contract tests against Exciter's, Armature's and Stator's own API
  documents and a recording of Exciter's MCP tools. `make suite-up` runs the
  real Exciter beside the stack, signing in through the same realm, and
  `make test-suite` tests against it; CI does so nightly and on pull requests
  labelled `suite`.
- Rotor keeps its secrets in OpenBao (#10). The api signs in to OpenBao as
  the realm's service account `rotor-control-plane`, renews its token and
  signs in again when it ends; it may touch people's connections and the
  tools' clients and nothing else, and every write names the version it
  read. `/readyz` reports unavailable while OpenBao is sealed or
  unreachable. The development OpenBao now keeps its data across restarts.
- The plan for tool connections is agreed (#7): `docs/api-contract-m1.md`
  settles how tools are added, how people connect their accounts, where the
  secrets live and how a run gets its token. The new endpoints are in the
  API document and answer 501 until they are built; those for administrators
  alone already refuse everyone else.
- People sign in with their suite account (#6). Rotor sends the browser to
  the realm `dynamo` and keeps the session as an HttpOnly cookie whose
  SHA-256 alone is stored; `rotor-administrators` makes an administrator,
  everyone else in the realm is a member, and the role follows the realm at
  every sign-in. Ending a session in Keycloak ends it in Rotor through
  back-channel logout, and a switched-off person loses their sessions and
  cannot sign in again. The settings show who is signed in.
- Rotor has a face (#5): `make up` now serves the web client, a shell with
  Workflows, Runs, Approvals, Connections and Settings, each saying what will
  live there, in the suite's light and dark colours. Settings names the build
  the server runs. A browser suite checks every page with axe in both schemes.
- The workflow runner has a home (#4): `runner/` is a Python 3.13 project
  with LangGraph, langchain-core, langchain-mcp-adapters and grpcio pinned
  by `uv.lock`, checked by ruff, mypy and pytest, and built into a
  distroless, non-root image that proves itself with no network, no
  capabilities and a read-only root.
- The backend answers (#3): `make up` now migrates the database and starts
  the api, which serves `/healthz`, `/readyz`, `/api/v1/openapi.json` and
  `/api/v1/meta`. One route table drives the router and `api/openapi.json`;
  sign-in's routes are agreed and answer 501 until #6. The audit log is
  append-only and readable by administrators alone, which the database
  itself enforces.
- The architecture and the decisions behind it are written down (#2):
  a Go control plane and a Python LangGraph runner, Rotor as an OAuth
  client of each tool with refresh tokens kept in OpenBao, one sandbox per
  run that is destroyed when idle, and workflows as versioned YAML. See
  `docs/architecture.md` and `docs/decisions.md`.
- A development stack and a test gate (#1). `make up` starts Postgres,
  Valkey, Keycloak with the suite's realm `dynamo` and an OpenBao dev server
  configured for Rotor, on ports of the checkout's own; `./run-tests.sh` runs
  the same layers locally and in CI, and the realm gains Rotor's sign-in
  client, its control-plane service account and the `rotor-administrators`
  group.
