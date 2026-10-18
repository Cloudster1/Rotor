# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- The runner turns a workflow into a LangGraph graph (#16): agents built on
  the workflow's model with the tools their bindings allow (only tools that
  say they only read, unless a binding may write), single MCP calls with
  arguments filled from the run's state, routers, and approvals that wait.
  Every MCP call carries the run's token for its binding, which never enters
  the run's state. A run that passes its step, time or token limit ends and
  says which; the runner reports each node starting and finishing, the
  model's text and each tool call as events.

### Fixed

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
