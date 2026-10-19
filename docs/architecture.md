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
why it is built this way, and [`threat-model.md`](threat-model.md) what it
protects and from whom.

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
  confirmations are enforced outside the sandbox (#31, #32, #40). Who decides
  an approval, its form and its expiry come from the pinned spec, never from
  what the sandbox sends.

## Repository layout

| Path | What | Issue |
|---|---|---|
| `backend/` | Go control plane: `cmd/api`, `cmd/worker`, `cmd/migrate`, `cmd/openapi`, `cmd/tool-stub` | #3 |
| `runner/` | Python runner executed in the sandbox | #4 |
| `web/` | React SPA | #5 |
| `e2e/` | Playwright suite | #5 |
| `api/openapi.json` | Generated from the route table | #3 |
| `api/workflow.v1.schema.json` | The workflow spec's JSON Schema | #13, #14 |
| `proto/` | The runner protocol; `make proto` generates `backend/internal/runnerpb` and `runner/src/rotor_runner/pb` | #13 |
| `deploy/` | Compose stack, Dockerfiles, realm, OpenBao setup, the Helm chart (`deploy/charts/rotor`) | #1, #29 |
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
sessions. Keycloak's sign-out of a person from every session sends one token
naming one session, so after each token Rotor reads the person's sign-out time
from Keycloak's admin API, as the `rotor` client's service account with
`view-users`, and ends their sessions older than it (#53).
`rotor-administrators` maps to Rotor's administrator role.

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
control plane over gRPC (#18) and keeps its checkpoints there (#19). It
makes itself not dumpable before it reads its bootstrap token, so the code
nodes it starts as children cannot read its memory or environment (#89).

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

The worker destroys its stack's sandboxes that no live run keeps, on every
pass.

In a cluster the Kubernetes backend (#26, `internal/sandbox/kube`) starts each
sandbox as a pod in the sandbox namespace instead:
- under the gVisor RuntimeClass, refused at start-up without one unless
  `ROTOR_SANDBOX_ALLOW_RUNC` overrides it (never in production)
- non-root, read-only, without capabilities or privilege escalation, with
  the runtime's seccomp profile and a memory `/tmp`
- with no ServiceAccount token, its bootstrap token in a Secret the pod owns
- with `activeDeadlineSeconds` as a backstop to the worker

The namespace enforces the restricted Pod Security standard, and the worker
may manage pods and Secrets there and nothing else.

## Runs and the worker (#22)

A person starts a run of a workflow they may see with
`POST /workflows/{id}/runs`: the inputs are checked against the workflow's
inputs schema, each binding needs the person's connection with the scopes it
asks for, and an `Idempotency-Key` makes a retry answer the run it started.
The run is inserted as `queued`, through `rotor_app`, whose policy allows
nothing else.

`cmd/worker` carries runs from there. Any number of workers may run; each
leases the runs it acts on with `FOR UPDATE SKIP LOCKED`, so a worker that
dies delays a run by at most its lease. Every pass:
1. **Reconcile:** lists the stack's sandboxes once, destroys those no live run
   owns, and marks a live run whose sandbox exited or vanished as lost.
2. **Expire:** replaces a sandbox that never said Hello and one whose runner
   stopped answering (a replacement resumes from the last checkpoint, up to
   three sandboxes per run), ends a run past its deadline, destroys the
   sandbox of a run that waits and revokes its tokens (#28), expires a run
   whose approval nobody decided in time (#31), and one that waited a week.
3. **Start:** gives each queued run a sandbox with a fresh bootstrap token.
4. **Teardown:** cleans up once after each run that ended.

A trigger on `run` refuses any move the contract does not list. Cancelling
ends the run at once and queues a `Cancel`; Valkey tells the api replica
holding the run's stream to send it, and that stream also looks on every
heartbeat in case the signal was lost.

## Approvals (#31)

A run that reaches an `approval` node stops: the runner sends the interrupt
with LangGraph's id, the form's proposed values and a preview of the writes
the nodes after it may make, rendered from the state it alone holds. In the
transaction that marks the run `waiting`, the control plane opens an
`approval` row, taking the group that decides, the form and the expiry from
the pinned spec, and the worker then drops the run's sandbox and tokens.

`internal/approvals` lists and decides approvals. The starter decides one
whose node names no group; otherwise only the group's members do. A decision
moves the run in the same transaction: approved, it goes back to `starting`
and the next sandbox's `StartRun` carries the approved values with the
interrupt's id, so it answers that approval and no later one; rejected, it
ends `cancelled`. The worker expires a run whose approval passed its time.
The database shows an approval to the run's people and the group, and lets
nobody but the control plane write one.

The web client's Approvals inbox lists what waits for you, with a badge in
the sidebar; an approval's page shows the preview, the form and the decision.

## Triggers (#34, #35)

A trigger is a record a person makes on a workflow they may see. It starts
the workflow's current version as them, on a schedule (#34) or when a tool
delivers a matching event (#35). The worker runs three more steps each pass,
before queued runs start:
1. **Schedule:** one worker at a time, under an advisory lock, turns each
   due schedule into a `trigger_fire` row and moves its next time on. Times
   come from five cron fields read in the trigger's time zone, skipping a
   time summer time removes and firing a repeated one once.
2. **Fire:** every queued firing is checked again as the trigger's owner,
   with a principal built from their account and no session. The owner must
   be active and see the workflow; every binding needs their connection;
   every write must sit behind an approval until #32; the inputs must fit.
   Then `runs.Start` runs with the exact version it checked and a key per
   firing, so a firing never starts two runs. A failed check turns the
   trigger off with a reason the owner reads. A tool that does not answer
   only delays the firing.
3. **Sweep:** once a minute, one query turns off the triggers whose owner
   was switched off, lost a connection a binding needs, or whose workflow
   was archived. Consent ends in several places and two processes, so a
   sweep catches them all.

## The audit log (#2, #36)

`audit.Write` records each act in the transaction that does it, with secrets
scrubbed from its data; `rotor_app` and `rotor_admin` may only insert and
read, and only administrators read. `internal/audit/service.go` is the read
side: keyset windows over `(created_at, id)`, facets, and exports as CSV or
JSON that are recorded before they leave. The worker writes the entries of
sandboxes and `runnerctl` the end of every run, both without an actor.

## Traces and metrics (#37)

Each run is one trace. Starting it opens a `run.start` span, whose context
the run keeps as its `traceparent`; the worker's sandbox steps join it, and
`StartRun` hands it to the runner, which traces the run, its nodes, its model
calls and its MCP calls. The runner sends its spans on its stream; the api
keeps those of the run's own trace, redacts them again and exports them with
its own over OTLP to the collector, so a sandbox reaches no collector. While
an administrator allows it, the api also sends the runner's spans, with
prompts and answers, to LangSmith with a key from OpenBao. The worker and the
api serve Prometheus metrics; `make observe` adds a collector, Jaeger and
Prometheus to the development stack.

## A run's tokens (#23)

A run's sandbox never sees a token in clear on the wire. When its runner asks
for a binding's token, the control plane mints one as the run's person, keeps
it in OpenBao at `rotor/runs/{run}/tokens/{id}` and sends an OpenBao wrapping
token that opens once within two minutes; the runner unwraps it at OpenBao
itself. The provider key travels the same way. Tokens live in the runner's
memory alone and are cut from its events, which the control plane redacts
again. When the run ends the worker revokes each token at its tool and
forgets it; ending a connection or switching a person off does the same at
once.

## A run's data at rest (#41)

Every run has a data key of its own. When a run is started the control plane
asks OpenBao for one (`transit/datakey/plaintext/rotor-run-data`), seals the
run's inputs with it and stores it wrapped by the installation's transit key
beside the run (`run.data_key`); only OpenBao opens it again. The run's
inputs and output, its events, its checkpoints and their writes, and the
control messages kept for its runner are sealed with AES-256-GCM, each bound
to the run, its column and its row, so a value moved elsewhere does not open
(`backend/internal/envelope`). The api and the worker keep an unwrapped key in
memory for five minutes after its last use, never in Valkey or the database.
Deleting a run deletes its wrapped key with it.

Before anything is kept or said, credentials are cut from it: the patterns in
`api/secret-patterns.json` (known token shapes, bearer values, private keys,
passwords in URLs) and fields named like a secret, by the runner first, which
also cuts every secret it holds, and again by the control plane for events,
run output, spans, audit records and every log line (`backend/internal/redact`).
Approvals keep their message, preview and values redacted but not sealed.

## Following a run (#24)

Every event of a run is a row of `run_event`, numbered by the control plane.
Whoever stores one publishes `runevents:<run>` on Valkey once it is committed.
`GET /runs/{id}/events` streams them as server-sent events:
1. It checks that the caller may see the run.
2. It listens on the run's channel, then sends what is stored after `Last-Event-ID`.
3. It sends each new row as a signal or its own poll finds it, until the run has ended.

Any api replica can serve a stream, whichever replica the runner talks to.

The web client's runs list filters by workflow, state and the runs you
started. A run's page draws the pinned version's graph with React Flow, laid
out in layers from the entry and coloured by the events. Beside the graph, a
timeline gives the same events in words, with tool calls folded open on
request and links to what they created.

## The chart (#29)

`deploy/charts/rotor` installs the api, the worker and the web, runs the roles and migrate Jobs before every
upgrade, creates the sandbox namespace with its token-less ServiceAccount and the worker's Role, and can configure
OpenBao with Rotor's own init script. In a cluster the control plane signs in to OpenBao with Kubernetes auth and
sandboxes dial the api's Service by its cluster name. Sandbox egress is denied by default (#27): a NetworkPolicy
allows DNS, the runner port and OpenBao, and Cilium allows the tools and the provider by hostname.
Rotor's images are named by digest, signed with cosign and carry a signed SBOM (#42); with sigstore's
policy-controller installed, the chart can have the cluster refuse a sandbox pod whose image is not signed.
`docs/deployment.md` says how to install it.

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
