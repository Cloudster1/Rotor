# Architecture

Rotor is the agentic backplane of the suite. People define workflows that
read and write [Exciter](https://github.com/Cloudster1/Exciter),
[Armature](https://github.com/Cloudster1/Armature) and
[Stator](https://github.com/praetorianer777/Stator) through their MCP servers,
start them by hand, on a schedule or from an event, and watch them run. It
mirrors the siblings' stack and patterns, so someone who knows one of them
finds their way around Rotor.

This document describes the architecture as it is built, and marks what is
planned with the issue that brings it. [`decisions.md`](decisions.md) says
why it is built this way.

## Overview

```
                      Keycloak, realm "dynamo" (one subject per person across the suite)
                         ^ sign-in (OIDC)              ^ service-account token
                         |                             |
browser --> web (nginx, React SPA) --> api (Go) -------+------> OpenBao
                         ^  SSE          |   |                   (refresh tokens, wrapping,
                         |               |   +--> PostgreSQL      transit, JWT and k8s auth)
                         |               |   +--> Valkey                ^
                         |               |                              | unwrap once,
                         |               v                              | own run's paths only
                         |         worker (Go) --spawn/destroy--> sandbox (one per run)
                         |               ^                        +--------------------------+
                         +---------------+---- gRPC, dialled out --| runner (Python, LangGraph) |
                                                                   +--------------------------+
                                                                      |  MCP over HTTPS, as the person
                                                                      v
                                                     Exciter /mcp   Armature /mcp   Stator /mcp
                                                     (each an OAuth 2.1 authorization server)
                                                                      |
                                                                      v  model calls
                                                               LLM provider
```

## Trust boundaries

```
+-- trusted: Rotor's control plane -------------------------------------------+
|  api, worker, PostgreSQL, Valkey, OpenBao                                   |
|  holds: sessions, workflow definitions, checkpoints, audit,                 |
|         refresh tokens (OpenBao only), model provider keys (OpenBao only)  |
+-----------------------------------------------------------------------------+
        ^ one gRPC stream per run, opened by the runner with a single-use credential
        |
+-- untrusted: a sandbox, one per run ----------------------------------------+
|  runs the workflow author's code nodes and reads content attackers may      |
|  have written; holds only its run's short-lived, narrow access tokens       |
|  egress: Rotor's runner endpoint, OpenBao, the tools, the provider (#27)    |
+-----------------------------------------------------------------------------+
        | MCP calls as the person; the tool enforces that person's permissions
        v
+-- the tools: Exciter, Armature, Stator (each judges every call itself) ----+
```

- A person's permissions are enforced by the tool, not by Rotor: a token
  minted for a person carries that person's rights and no more, and the
  workflow's declared scopes narrow it further (#12).
- Content read from a tool is data, never instructions; approvals and write
  confirmations are enforced outside the sandbox (#31, #32, #40).

## Repository layout

| Path | What | Issue |
|---|---|---|
| `backend/` | Go control plane: `cmd/api`, `cmd/migrate`, `cmd/openapi`, later `cmd/worker` | #3 |
| `runner/` | Python runner executed in the sandbox | #4 |
| `web/` | React SPA | #5 |
| `e2e/` | Playwright suite | #5 |
| `api/openapi.json` | Generated from the route table | #3 |
| `api/workflow.v1.schema.json` | The workflow spec's JSON Schema | #13, #14 |
| `proto/` | The runner protocol; `make proto` generates `backend/internal/runnerpb` and `runner/src/rotor_runner/pb` | #13 |
| `deploy/` | Compose stack, Dockerfiles, realm, OpenBao setup; later the Helm chart | #1, #29 |
| `mk/`, `run-tests.sh`, `tests/` | Toolchains in containers, the gate and its own tests | #1 |

## Backend (`backend/`, #3)

- Module `github.com/cloudster1/rotor/backend`, Go 1.26, chi v5, pgx v5 and
  hand-written SQL, goose migrations embedded in `cmd/migrate`.
- One route table drives the router, `api/openapi.json` and the tests that
  keep them in step. An operation agreed before it is built carries
  `pending: true` and answers 501.
- Configuration: environment variables only, prefix `ROTOR_`.
- Two database roles: `rotor_app`, bound by row-level security, and
  `rotor_admin`, exempt by policy for sign-in and background jobs. Neither is
  a superuser, and the api refuses to start as one outside development.
- The audit log is append-only: neither role may update or delete it.

## Sign-in (#6)

OIDC authorization code flow with PKCE against the realm `dynamo`, client
`rotor`. Sessions are rows in Postgres holding the SHA-256 of an opaque token
sent as the HttpOnly cookie `rotor_session`. Keycloak's back-channel logout
reaches `POST /api/v1/auth/oidc/backchannel-logout` and ends the matching
sessions. `rotor-administrators` maps to Rotor's administrator role.

## Tool connections (M1, `docs/api-contract-m1.md`)

An administrator adds a tool by its address; Rotor discovers its MCP
endpoint and authorization server (RFC 9728, RFC 8414) and registers as its
OAuth client (RFC 7591). A person connects on the tool's consent page; the
refresh token goes to OpenBao, at `rotor/connections/{user}/{tool}`, and
never to Postgres. When a run starts, the control plane mints a fresh access
token for it, one refresh per connection at a time, so the tool's refresh
token rotation never ends a grant. The api reaches OpenBao with JWT auth as
the realm client `rotor-control-plane`.

## Workflows (#14, #15)

A workflow is YAML checked by `internal/workflow/spec`; `internal/workflows`
keeps it as `workflow` and immutable, gap-free `workflow_version` rows, shared
by realm group through row-level security. Saving checks a spec against its
tools as the saver sees them: `internal/catalog` mints the saver a token,
reads the tool's MCP `tools/list` and caches it in Valkey per tool build and
person (`internal/cache`), which is Valkey's first use; losing the cache costs
a fetch. The web editor is CodeMirror 6, loaded only on the workflow pages.

## Runner (`runner/`, #4)

Python 3.13 with uv, LangGraph, langchain-core and langchain-mcp-adapters,
pinned by `uv.lock`. Its image is distroless, runs as non-root with a
read-only root filesystem, and is tested with no network and no
capabilities. From M2 it compiles a workflow into a graph (#16), talks to the
control plane over gRPC (#18) and keeps its checkpoints there (#19).

## Web (`web/`, #5)

React 19, TanStack Router and Query, Tailwind with the suite's design tokens,
a client typed from `api/openapi.json`, and an accessibility check on every
page in the browser suite.

## Runner protocol (#18)

The api serves `RunnerControlService` on `:9443` with TLS from OpenBao's PKI,
reachable from the sandbox network alone. A sandbox's runner dials it with a
single-use bootstrap token. From then on:
- The api answers with the run's pinned workflow, its bindings resolved to
  the tools' MCP addresses, and tokens minted for the run's person as the
  runner asks for them.
- Every event is stored before it is acknowledged. Both sides resend what was
  not acknowledged after a reconnect.
- A runner silent for three heartbeats is recorded as lost.

## Model providers (#20)

Administrators keep the providers in the `provider` table and their keys in
OpenBao at `rotor/providers/{id}/key`. When a run starts, the control plane
resolves the workflow's model (or the installation's default) and puts it in
`StartRun`, and sends the key in a `SecretGrant`. The runner builds the model
with `init_chat_model`, and the key stays in its memory.

## Sandboxes (#21)

`internal/sandbox` starts and ends the containers runs execute in. In
development the Docker backend drives the host's daemon through its socket.
Each sandbox:
- runs read-only with a small `/tmp`
- has no capabilities and runs as a non-root user
- has memory, CPU and process limits
- is labelled with its stack and run
- joins only the internal network `sandbox`, which reaches the api and the
  tool double and nothing else

At start the api destroys its stack's sandboxes that no run keeps.

## Development stack (`deploy/docker-compose.yml`, #1)

Postgres 18, Valkey 8, Keycloak 26 importing the suite's realm, and OpenBao
with integrated storage on a volume, initialized and unsealed by its
entrypoint and configured by `deploy/openbao/init.sh` on every start: kv-v2
at `rotor/` with check-and-set required, transit, the policies in
`deploy/openbao/policies/`, and JWT auth trusting the realm for the role
`rotor-control-plane`. A second OpenBao, `openbao-sealed`, is initialized
and never unsealed, so readiness is tested against a sealed server. `make up` derives
the compose project and its ports from the checkout's path, in 30000 to
32759, so worktrees and the siblings' stacks run side by side.

The `tool-stub` (#8) stands in for the tools: every `/t/<name>` on its port is
an OAuth authorization server and MCP endpoint that behaves as Exciter's does,
held to the siblings' vendored documents in `api/tools/` and to a recording of
Exciter's `tools/list` by a contract test. Its control API, on a port the
stack never publishes, lets tests shape a tenant, count what Rotor did there,
and end grants or forget clients as a tool would. `make suite-up` runs the real
Exciter, from the commit in `deploy/suite/EXCITER_REF`, beside the stack: it
signs people in through Rotor's Keycloak, and the two meet on a network of
their own under `rotor-*` and `suite-exciter-*` names.
