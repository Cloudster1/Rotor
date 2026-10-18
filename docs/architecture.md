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

## Development stack (`deploy/docker-compose.yml`, #1)

Postgres 18, Valkey 8, Keycloak 26 importing the suite's realm, and an
OpenBao dev server configured by `deploy/openbao/init.sh` on every start
(kv-v2 at `rotor/`, transit, JWT auth trusting the realm). `make up` derives
the compose project and its ports from the checkout's path, in 30000 to
32759, so worktrees and the siblings' stacks run side by side.
