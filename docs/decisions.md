# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-18: Tool connections for M1 (#7)

- **One refresh per connection at a time.** The tools rotate refresh tokens
  at every use and end the whole grant when a replaced one comes back
  (Exciter does today; Armature and Stator are asked to). Two runs
  refreshing one connection at once would sign the person out of the tool.
  So Rotor takes a Postgres advisory lock on the connection, refreshes,
  writes the rotated token to OpenBao with check-and-set, and only then
  hands out the access token. A lock in Postgres rather than Valkey, because
  the database is already the one thing every api replica agrees on.
- **A refresh never asks for a narrower scope.** At Exciter that narrows the
  grant for good. A run that needs less gets the consented scope and the
  runner enforces the narrower one, until the tools offer token exchange
  (Cloudster1/Exciter#235).
- **Every run gets its own access token.** Minting refreshes each time, so
  revoking one run's token ends no other run, at the cost of a refresh per
  run, which the tools allow.
- **Rotor is a confidential client registered by RFC 7591.** An
  administrator adds a tool by its address and copies no secret; the client
  secret goes straight to OpenBao. A tool that forgets the client (Exciter
  sweeps unused ones) is registered again on demand.
- **The callback checks `iss`.** Every connection shares one redirect URI,
  so a response is matched to the tool it was started for by its state and
  its issuer (RFC 9207), against mix-up attacks.
- **Exciter is the only tool that can be connected in M1.** Armature and
  Stator have MCP servers but no authorization server yet. The gate tests
  against a test double that behaves as Exciter does, and the real Exciter
  runs, from a pinned commit, in a suite job outside the required checks,
  because building it adds minutes to every pull request.
- **Dev OpenBao keeps its data.** It runs as a server with file storage,
  initialized and unsealed on start, so a restart keeps people's
  connections as production would.

## 2026-10-18: Sign-in for M0 (#6)

- **Sessions live in Postgres, not Valkey.** A session is a row holding the
  SHA-256 of an opaque token, as in Exciter, so sessions are covered by the
  same grants and audit as everything else, survive a Valkey flush and can
  be ended by a query (back-channel logout, switching someone off). Valkey
  stays in the stack for caches and fan-out later; it holds nothing that
  must survive.
- **OIDC only, configured from the environment.** Rotor never acts on its
  own: every run acts as a person whose identity in the suite comes from the
  shared realm, so a local password account would be a person no tool knows.
  The issuer, client and secret are `ROTOR_OIDC_*` variables rather than a
  table an administrator edits, because an installation of Rotor belongs to
  one suite and one realm. Reconsider if Rotor is ever run without the suite.
- **Back-channel logout is supported from the start.** Ending a session in
  the realm must end it in Rotor too, because a signed-in session can start
  runs that act in three tools. Keycloak sends a logout token to the api,
  which ends the sessions of that realm session (`sid`), or of the person
  when only `sub` is given.
- **Every person of the realm may sign in, as a member.** What a person may
  do in Exciter, Armature and Stator is decided by those tools when a run
  acts as them, so Rotor needs no gate of its own beyond the realm.
  `rotor-administrators` makes an administrator of Rotor itself.
- **A logout token that names a session ends that session alone.** Leaving
  one browser keeps a person's others, as the specification asks; a token
  naming only the person ends all of theirs. Keycloak answers its admin
  sign-out of a person with one token naming one session, so their other
  Rotor sessions last until they expire; #53 follows that up.

## 2026-10-18: The shape of Rotor (#2)

- **A Go control plane and a Python runner.** The api, the worker and every
  piece of state are Go, like Exciter, Armature and Stator, so the suite's
  patterns, tests and operators carry over: one route table for the router,
  the OpenAPI document and later the MCP tools; pgx and hand-written SQL;
  row-level security proven by tests. LangGraph and LangChain are
  Python-first, and their JavaScript ports trail; so the part that runs a
  workflow is a Python program inside the sandbox and nothing else is.
  The two speak one protocol (below), so either side can be replaced.
- **Rotor is an OAuth 2.1 client of each tool.** A person connects their
  account in Exciter, Armature and Stator once, on each tool's own consent
  page. The tools' MCP servers already act as the holder of a token through
  their full permission checks, so a token minted for a person can never do
  more than that person can. Keycloak token exchange was the alternative,
  but it needs every tool to accept the realm's tokens at its MCP endpoint,
  which Exciter deliberately does not (its decision #182).
- **Refresh tokens live only in OpenBao and never enter a sandbox.** A
  sandbox runs code a workflow author wrote and reads text an attacker may
  have written; what it holds must be worth little when it leaks. Each run
  gets its own access tokens, as short-lived as the tool allows, limited to
  the scopes its workflow declares intersected with what the person
  consented to, and revoked when the run ends. The tools rotate refresh
  tokens and end a grant whose refresh token is used twice, so Rotor never
  refreshes one connection twice at once.
- **Who a run acts as.** A run started by hand acts as the person who
  started it. A scheduled run, or one started by an event in a tool, acts as
  the owner of its trigger, and stops being able to start the moment that
  person's consent or account ends. A shared workflow never shares its
  author's tokens.
- **OpenBao, not Vault.** OpenBao is the open-source fork under the MPL with
  the same API, dynamic secrets, leases, response wrapping and transit
  encryption. The control plane signs in to it with a Keycloak
  service-account token (JWT auth), so there is no static OpenBao token to
  rotate; a sandbox signs in with its Kubernetes service account
  (Kubernetes auth) and reaches only its own run's paths.
- **Tokens reach a sandbox wrapped.** The control plane mints the run's
  tokens and hands them over as an OpenBao response-wrapping token that can
  be unwrapped once, within minutes. A wrapping token seen twice means
  someone else opened it, and the run is stopped.
- **One sandbox per run, destroyed when idle.** A sandbox lives while its run
  is working or waiting, until a time without interaction (events,
  approvals, resumes) passes; then it is destroyed and its tokens revoked.
  The run keeps its place in a checkpoint and a new sandbox picks it up
  when a person answers. A run waiting days on an approval costs nothing in
  between.
- **gVisor in production, Docker for development.** A sandbox runs
  untrusted code, so in Kubernetes it is a pod under the gVisor runtime
  class, non-root, read-only, without capabilities, whose egress reaches
  only Rotor, OpenBao, the tools and the model provider. The Docker backend
  exists so the whole flow runs under `make up`; it is never offered in a
  production profile.
- **The runner dials out.** A sandbox accepts no connection. The runner
  starts with a single-use bootstrap credential, dials the control plane
  over gRPC, and receives its work, tokens and resumes on that one stream.
  Nothing on the cluster network can talk to a sandbox.
- **Checkpoints belong to the control plane.** LangGraph's checkpoints are
  stored and loaded through the runner channel into Rotor's own tables. A
  sandbox never holds database credentials, a checkpoint outlives its
  sandbox, and checkpoints get the same encryption, retention and audit as
  the rest of Rotor's data.
- **Workflows are data.** A workflow is a versioned YAML document
  (`rotor.dev/v1`) of nodes, edges, tool bindings with their scopes,
  limits, triggers and approval steps, compiled by the runner into a
  LangGraph graph. It can be reviewed, diffed and validated before it runs,
  and a visual editor can be drawn on top of it later. A Python code node
  is the escape hatch for a step the declarative nodes do not cover; it
  runs in a subprocess of the sandbox without the run's tokens.
- **Run events reach the browser over Server-Sent Events.** Events flow one
  way, the browser reconnects with `Last-Event-ID` and gets what it missed,
  and SSE passes every proxy that passes HTTP; a WebSocket would add a
  second protocol for no gain.

### Alternatives considered

- **An MCP gateway in the control plane** would keep every token out of the
  sandbox: the runner would ask the control plane to call a tool. It moves
  every tool call, stream and retry through Rotor's api and makes the api
  the bottleneck of every run; short-lived, narrow, revocable tokens in a
  sandbox that can reach nothing else give most of the safety. Reconsider
  if the tools cannot issue narrow tokens (Cloudster1/Exciter#235).
- **LangGraph's PostgresSaver** would give the sandbox a database
  connection, which a sandbox running untrusted code must not have.
- **HashiCorp Vault** has the same API, but its licence is the BSL.
- **Kata Containers or Firecracker** isolate more strongly than gVisor but
  need nested virtualisation or bare metal, which most clusters do not
  offer. The sandbox backend is an interface; another can be added.
