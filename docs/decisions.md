# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-19: Content a run reads, for M8 (#40)

- **Every quoted value is data, not only tool answers.** A template can
  quote a tool node's output, a code node's, another agent's or a hook's
  payload, and tracking where each came from would be one more thing to get
  wrong. So every value a prompt quotes is fenced, and only the author's own
  words instruct.
- **A random fence id the policy never names.** The id changes with each
  sandbox, so content cannot know it, and the policy describes fences
  without one, so a resumed run reads the same policy. Markers inside content
  are defanged as well, so even a guessed id closes nothing.
- **Links limited in the runner, to exact hosts.** The runner is where an
  agent's text and the arguments it sends a tool are made, so that is where a
  link to another host becomes `[link removed]`; the run's page links only to
  the same hosts. A subdomain is another host, since anyone may own one.
  Code nodes are the author's code and keep their links.
- **A warning, not a refusal.** A workflow that reads and then writes with
  nobody approving in between is often what its author means, so saving
  works and the editor says why it may be unwise; triggers keep refusing
  unattended writes (#34). `confirmWrites` (#32) will make the warning
  answerable without an approval node.
- **The corpus proves the guards, not the model.** A fake model cannot be
  persuaded, so `runner/tests/injection/` scripts one that obeys the planted
  text and checks what holds anyway: the policy leads, every quote is
  fenced, a binding's limits and the review rule hold, and no other host
  leaves.

## 2026-10-19: A run's data at rest for M8 (#41)

- **Envelope encryption, one data key per run.** OpenBao transit makes each
  run's key and wraps it with the installation's key; the control plane
  seals with it locally. A run's stream, its followers and its pages then
  cost one OpenBao call per run rather than one per value, and deleting a
  run deletes its key. A key per time window would have bound less and made
  deleting one run's data a matter of trust.
- **Keys stay in each process.** An unwrapped key is kept in memory for five
  minutes after its last use and never shared through Valkey, so a copy of
  Valkey or the database holds no key that opens anything.
- **Bound to the row.** The additional data names the run, the column and
  the row, so a ciphertext copied to another run or row does not open.
- **No conversion.** Nothing had been released, so the migration deletes the
  runs kept in plaintext instead of keeping a second read path; the audit
  log keeps their record.
- **One list of credentials for both languages.** `api/secret-patterns.json`
  holds the patterns, written in the syntax Go's RE2 and Python's re share,
  and a test on each side refuses a copy that differs, as for the workflow
  schema.
- **Retention per installation, shortened per workflow.** An installation
  answers for how long it keeps what its people's runs read, so a workflow
  may keep its runs for less, never for more. The value is read from the
  workflow's current version, so shortening it applies to runs already kept.
- **Deleting a run deletes everything it held.** Its events, checkpoints,
  control messages, approvals and usage go with it by cascade, which
  PostgreSQL runs as the tables' owner, so no runtime role needs the right to
  delete events alone; daily totals and the audit log stay.
- **Approvals are sealed too (#91).** Their message, preview and values are
  sealed with the run's key and bound to the interrupt they answer, which is
  unique within the run, so they need no id before they are stored. The
  answer a resumed run gets is built in Go from the opened values instead of
  in SQL, and a page of approvals across many runs unwraps their keys in one
  OpenBao call.

## 2026-10-19: OpenBao's audit log and Valkey's password (#92)

- **Refuse, rather than enable, an audit device.** OpenBao 2.7 takes audit
  devices only from its configuration file, never through its API, so
  Rotor's init script cannot turn one on. It refuses to configure an OpenBao
  that has none, with a switch for throwaway test servers such as the kind
  test's dev-mode one.
- **The development stack's audit log goes to the container's log**, where
  `docker compose logs openbao` shows it, hashed as OpenBao hashes secrets.
- **A Valkey password is required outside development, twice.** The chart
  refuses to render without one and the api refuses to start, so an
  installation that does not use the chart is held to it too.

## 2026-10-19: Code nodes and the runner's memory (#89)

- **The runner is not dumpable.** A code node runs as the runner's own user,
  and the runner holds the run's tool tokens and the provider key in memory.
  `prctl(PR_SET_DUMPABLE, 0)` before the bootstrap token is read keeps every
  process of that user without `CAP_SYS_PTRACE` from tracing the runner or
  opening its `/proc` files, its environment as it started included, which
  `os.environ.pop` does not clear. A second user for code nodes would have
  needed a setuid helper or a capability in an image that has neither.
- **The runner refuses to start without it**, and its self-check runs it, so
  an image that cannot is caught by the smoke test.

## 2026-10-19: Signed images and dependency audits for M8 (#42)

- **Signing is make targets, publishing waits for the release.** `make sbom`,
  `sign`, `attest` and `verify` run cosign and syft from the kind tools
  image, and the kind test proves them against a local registry; the release
  workflow (#44) calls the same targets, so nothing is published before
  releases are cut. Locally a key pair signs and nothing goes to a public
  transparency log, since a private registry's images have no place there;
  the release can sign keyless with GitHub's identity instead.
- **The SBOM is an attestation, not a file beside the image.** Signed with
  the same key and stored with the image, it travels to every registry the
  image is copied to, and admission can require it.
- **policy-controller for admission, in the sandbox namespace only.** It is
  sigstore's own, verifies cosign's signatures and attestations with a key
  and no transparency log, and acts only on namespaces labelled for it, so
  the control plane and everything else in the cluster are untouched. It
  stays optional and the operator's to install: a cluster may already run
  Kyverno or another admission controller.
- **Audits read what ships, nightly and when dependencies change.**
  govulncheck reports only vulnerable code the binaries reach, and pip-audit
  and npm audit read the runtime dependencies, not the test and build tools,
  which never meet untrusted input. A new advisory must not block unrelated
  pull requests, so the `deps` workflow is no required check; Renovate,
  waiting two weeks after each release as Rotor's dependencies always have,
  brings the fix. Its pull requests are the one kind of change that hangs off
  no issue.
- **Base images by digest.** Every Dockerfile names its bases by digest, so
  a build is the same tomorrow; Renovate moves the digests.

## 2026-10-19: Signing a person out everywhere (#53)

- **Rotor asks Keycloak when the person was signed out.** Keycloak 26.2
  answers an administrator's sign-out of a person with one logout token per
  client, naming one of their sessions, so the token alone cannot tell it
  from leaving one browser. Keycloak stamps the person's `notBefore` in the
  same request, so Rotor reads it from the admin API and ends every session
  made before it. Ending a single Keycloak session leaves the stamp alone and
  so still ends one Rotor session.
- **After the answer, not during it.** Keycloak saves the stamp only once its
  sign-out request ends, and that request waits for Rotor's answer to the
  token, so Rotor looks two and ten seconds later.
- **The `rotor` client's service account with `view-users`.** The api holds
  that client's secret in every installation, while `rotor-control-plane` is
  used only by the development OpenBao. The role lets the control plane read
  the realm's people, which it already learns as they sign in; an
  installation that withholds it keeps the old behaviour, with a warning.

## 2026-10-19: Usage, cost and rate for M7 (#38)

- **The control plane counts, the runner reports.** Only the control plane
  sees every sandbox of a run and every run of a person, so it totals usage
  per run and per UTC day as each `llm.usage` event is stored, in the same
  transaction, and stops the run there. The runner keeps its own token count
  as a quick stop within one sandbox. Usage is as honest as the runner that
  reports it; a sandbox runs the workflow's code, so the tools' and the
  providers' own records stay the check.
- **A run keeps the prices it began with.** Prices are copied into the run's
  usage when its stream first opens, so changing a price never rewrites what
  a run in flight costs. A model without prices counts tokens only, and a
  workflow cannot bound the cost of what nobody priced.
- **A waiting run holds no place.** Concurrency counts runs starting or
  running; a run waiting for an approval has no sandbox, and holding a place
  for it would let one slow approver block everyone else's runs.
- **Tool calls are limited in the runner.** The rate is the run's own, and
  the runner is where its calls happen; a call past it ends the run even
  inside an agent, whose tool node re-raises it rather than handing it to the
  model.

## 2026-10-19: Traces, metrics and LangSmith for M7 (#37)

- **The runner's spans go through the control plane.** A sandbox reaches
  the api, OpenBao's unwrap and the tools, nothing else; a route to a
  collector would be one more way out. The runner sends its spans on the
  stream it already holds, and the api checks, redacts and exports them,
  so a forged span can neither leave the run's trace nor carry a secret.
- **So does LangSmith.** LangSmith reads OpenTelemetry, so the api forwards
  the same spans there while an administrator allows it. The key stays in
  OpenBao and in the api's memory, never in a sandbox where workflow code
  runs, and the sandbox's egress rules, fixed when the chart is installed,
  need no LangSmith host that an administrator's switch could not open.
- **Prompts and answers go to LangSmith alone.** They are what LangSmith is
  for and what an operator's collector does not need, so model spans carry
  them only while LangSmith is on, and the collector never receives them.
- **Off until acknowledged.** Turning LangSmith on sends run data out of
  the installation; the administrator says so in the same request.
- **Metrics on client_golang, its names kept.** Histograms and the
  process's own metrics came with it; nothing a dashboard reads changed.

## 2026-10-19: Triggers for M6 (#33)

- **Triggers are records each person makes, not part of the workflow's
  YAML.** A trigger acts as someone, and that someone is whoever made it: a
  colleague can schedule a shared workflow as themselves, with their own
  connections, and their trigger ends with their consent, not the author's.
  Whether a trigger is on, and why Rotor turned it off, is state, which a
  versioned document should not hold. The spec keeps `triggers` reserved and
  empty.
- **Until writes are confirmed at the tool, a triggered run writes only
  after an approval.** Nobody watches a run a schedule or an event starts,
  and #32, which holds a write back at the tool, waits on token exchange
  (Cloudster1/Exciter#235). So a trigger needs every node that may write to
  sit behind an approval node, whatever its binding says, checked again at
  each firing against the version that will run.
- **A run starts only if its owner can read the event's subject.** The tools
  read a webhook's payload with the rights of the administrator who added
  the webhook, so the payload may show what the trigger's owner may not
  see. Rotor asks the tool, as the owner, for the requirement, issue or page
  first, and skips the firing otherwise.
- **Registration is manual for now.** Each tool makes the webhook's secret,
  and its administrator pastes it into Rotor, which keeps it in OpenBao.
  Armature now lets a connected app register its own webhook (Armature
  #281); using it comes later, with the other tools following.
- **A firings queue between deciding and starting.** The scheduler and the
  hook endpoint only write `trigger_fire` rows; the worker starts the runs.
  A delivery is answered without waiting for a sandbox, one scheduler at a
  time is enough under an advisory lock, and a firing has one key, so
  retries and duplicate deliveries never start a run twice.
- **Lost consent is swept, not signalled.** Consent ends in several places
  and two processes; one sweep in the worker that compares triggers with
  connections and accounts catches each of them, and every firing checks
  again.

## 2026-10-19: Approvals for M5 (#30)

- **Approvers are set per approval node, and a group decides alone.** Each
  step names who should judge it: a write to a shared tool may need a
  reviewing group, a personal summary only its starter. With a group named,
  the starter decides only as one of its members, so a person cannot wave
  their own run's write past the people asked to review it. Administrators
  and workflow owners see approvals but do not decide them.
- **An approver lends judgement, never access.** The run goes on with the
  tokens of the person who started it. A group approver sees the run they
  decide on, but cannot cancel it, since rejecting is how they stop it.
- **The runner computes the preview.** Only the runner holds the run's
  state; the control plane cannot read checkpoints it keeps sealed. The
  control plane still takes the approver group, the form and the expiry
  from the pinned spec, never from what the sandbox sends.
- **A run resumes by interrupt id.** A plain resume value answers whatever
  interrupt comes next, so a sandbox replaced after an approval would have
  answered the following approval with the earlier decision. Keyed by the
  interrupt's id, a decision answers only the step it was taken for.
- **Write confirmation waits for token exchange (#32).** Holding a write
  back from a sandbox needs a token that cannot write; until the tools
  issue narrower tokens without narrowing the person's grant
  (Cloudster1/Exciter#235), the policy is agreed and not built.

## 2026-10-18: Sandbox egress for M4 (#27)

- **Cilium for hostnames, NetworkPolicy beneath it.** The tools and the
  model providers are hostnames whose addresses change; Cilium's FQDN
  policies follow them from the lookups a sandbox makes, while a plain
  default-deny NetworkPolicy allows DNS, the runner port and OpenBao on any
  cluster. An egress proxy would have worked without Cilium but put another
  component on every call a run makes.
- **No hostname allow without Cilium.** The chart refuses hostnames on a
  cluster without it rather than silently allowing nothing or everything,
  and takes address ranges there instead.
- **The kind test runs Cilium.** The policies are proven where they are
  enforced, not only rendered.

## 2026-10-18: A waiting run keeps nothing alive (#28)

- **The idle time is zero.** A run that starts waiting loses its sandbox at
  once (since #22) and now its tool tokens too, revoked at the tools. A
  resume starts a fresh sandbox from the checkpoint, which is handed fresh
  tokens. This replaces "destroyed when idle" in the shape of Rotor: a warm
  sandbox kept for a quick approval would save a cold start but keep a
  sandbox and live tokens around for nothing in every other case, so there
  is no idle TTL to set, per installation or per workflow.
- **Leases, not an elected reaper.** The worker leases each run it acts on
  with `FOR UPDATE SKIP LOCKED`, so any number of workers reap without two
  acting on one run and without electing one by an advisory lock.
- **Metrics by hand until M7.** The worker serves `/metrics` in Prometheus's
  text format, written in a small package of its own: three families do not
  justify a client library before M7 chooses the observability stack.

## 2026-10-18: Sandboxes on Kubernetes for M4 (#26)

- **A sandbox holds no identity.** No ServiceAccount token is mounted in a
  sandbox pod and no OpenBao role exists for it: since #23 a sandbox only
  opens single-use wrapped tokens, which needs no login, so an identity
  would guard paths it never uses and give a hostile code node something to
  steal. This replaces the earlier plan of Kubernetes auth for sandboxes.
- **The control plane signs in to OpenBao with Kubernetes auth in a
  cluster.** The api and the worker present their own ServiceAccount
  tokens, so a cluster needs no Keycloak service account for OpenBao; JWT
  auth stays for the development stack.
- **client-go for the Kubernetes backend.** Unlike the Docker backend, which
  speaks a few calls of the Engine API by hand, pods, secrets, in-cluster
  credentials and their rotation are what client-go is for.
- **gVisor unless explicitly overridden.** Start-up refuses a Kubernetes
  sandbox without a RuntimeClass unless `ROTOR_SANDBOX_ALLOW_RUNC` says so,
  which production refuses; kind in CI runs runc behind it.
- **The sandbox namespace enforces the restricted Pod Security standard.**
  A pod that is not hardened is refused by the cluster as well as never
  built by Rotor, and the worker may manage pods and their bootstrap Secrets
  there and nothing else.
- **The Kubernetes tests run apart.** A kind cluster adds minutes, so they
  run nightly, on pull requests that change what a cluster runs and on
  those labelled kube, without being a required check.

## 2026-10-18: Workflows and the runner for M2 (#13)

- **gRPC with protobuf for the runner protocol, generated with buf.** One
  bidirectional stream carries run control, tokens, events and checkpoints
  between Go and Python with typed messages both sides generate from one
  file; buf lints it and refuses incompatible changes against main. Go code
  comes from buf with plugins built in the Go container, Python code from
  grpcio-tools in the uv container, so no generation needs a network
  service.
- **The runner dials out.** A sandbox needs no listening port and no
  inbound rule, and the control plane decides what it serves; a reconnect
  replays what the other side did not acknowledge.
- **The Docker sandbox backend comes in M2, not M3.** The runner protocol
  and the checkpoints are only proven by killing a real sandbox and
  resuming in a fresh one, so the tests start and destroy real containers.
- **Templates are paths, never code.** A tool node's argument may name a
  value of the state, `{{ nodes.draft.title }}`, and a router switches on a
  value; there is no expression language to sandbox or to keep compatible.
  Anything more is a code node, which runs in a subprocess with limits.
- **Checkpoints are encrypted with OpenBao transit.** They hold what a run
  read from the tools; the key never leaves OpenBao and a database dump
  shows ciphertext.
- **CodeMirror 6 for the workflow editor.** It is small, themable with the
  suite's tokens and testable in jsdom, and codemirror-json-schema gives
  completion and checks from the same schema the server uses.

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
  rotate; a sandbox holds no OpenBao credential at all (see #26 above).
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
