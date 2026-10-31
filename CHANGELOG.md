# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- The integration document says what Dynamo needs to run Rotor (#170): who may
  sign in, the images and memory of the stack and the chart, what an umbrella
  chart sets, and that a compose server runs without sandboxes. It follows
  Dynamo's compatibility rules and the shared promise of `mcp@1`.
- Back-channel logout reaches Rotor under the name the suite gives it (#172): the
  api answers to `rotor-api` on the stack's network, the realm's `rotor` client
  sends logouts to `http://rotor-api:8080/...` as Dynamo's realm does, and
  `make realm-check` now compares the whole realm with Dynamo's (#109).
- A service account of the realm can add tools and hand Rotor their webhook
  secrets (#168), so the suite's `dynamo-connect` wires Rotor to Stator,
  Armature and Exciter unattended. `GET` and `POST /api/v1/tools` and `GET`
  and `PUT /api/v1/tools/{toolID}/hook` take an access token of the realm whose
  audience holds `rotor` and whose groups hold `rotor-administrators`; a token
  missing any of that is refused in a sentence that says what, every other
  route refuses it, and what it changes is audited as the service account.
  Each tool now shows its `hookURL`, and the development realm has the client
  `dynamo-connect`.
- Rotor's chart installs as a dependency of a parent chart, as Dynamo's
  umbrella chart will install it (#169). Every name comes from
  `fullnameOverride`, every secret can come from a Secret made beforehand,
  `openbao.configure.afterInstall` configures an OpenBao the parent brings
  once it is up, `openbao.addr` and `sandbox.egress.openbao.namespace` may
  name the release's namespace as `{{ .Release.Namespace }}`, and a refusal
  names each value as the parent sets it, such as `rotor.oidc.issuer`. The
  chart tests render it under a small parent, and the kube layer installs it
  that way beside a second release.
- A feature specified in Exciter can be implemented through reviewed, tested
  code (#140): `examples/implement-feature.yaml`, started by a person, reads
  the feature's requirements and which of them changed, lets a test designer
  and a test reviewer write its test cases and files them in Exciter, linked
  `verifies`, then lets a coder work on the Armature issue's branch until its
  tests pass and code, security, test run and requirements reviewers agree,
  each round's findings going back to the coder. It opens a pull request with
  the reviewers' summary, records each test case's result in a test execution
  and comments on the issue; nothing merges by itself. The tool-stub places a
  requirement under a section, answers `list_links` with its suspect ends,
  refuses a test case's text sent both as a document and as Markdown, as
  Exciter does, and starts a repository with a fixture's files.
- Each agent can be its own specialist (#133): an agent node may name its
  own `model`, among those an administrator allows, a `role` put under
  Rotor's fixed rules, and its own `maxSteps`, at most the workflow's. Each
  model call counts at its own model's prices toward `maxCost`, the canvas
  edits an agent's role and model, and a run's page shows which model each
  agent ran on and what it used.
- Rotor knows Exciter's test plans and test executions (#139). The suite runs
  a current Exciter (`deploy/suite/EXCITER_REF`), whose tools a workflow
  calls to start an execution of a test plan, record each case's result and
  close it; the catalogue knows these write and ask for an approval, and that
  reading a case's results does not. The tool-stub answers them too, as well
  as `list_types`, `get_tree`, `link_requirements` and `list_test_plans`,
  files test cases with their steps, and the control API reads back the links
  and executions a run made, so the coming feature workflow is tested in
  every pull request. `make tool-snapshots` records Exciter's `tools/list`
  from a throwaway installation, as it does Armature's, without the suite.
  Adding this Exciter works: it lists a token exchange for its partner apps,
  which Rotor now leaves alone instead of registering for it, so Exciter no
  longer refuses Rotor.
- Reviewers run at once and the next step waits for all of them (#135):
  several edges out of a node run their targets in parallel, and `join: all`
  runs a node once, after every branch into it has ended. A node several
  edges lead into with no `join` is saved with the warning `join_missing`.
  Approvals that parallel branches reach together are all in the inbox, the
  run page links to each, and the run goes on once each is decided; one
  rejection ends it. `maxSteps` counts the nodes of every branch, and the run
  page draws the branches side by side above their join.
- Loops end where you say (#134): `maxVisits` on a node bounds how often it
  runs, and the visit past it goes to its `onExhausted`, such as an approval
  where a person takes over, or ends the run as `loop_exhausted`. Templates
  and routers read the count as `visits.<id>`, a loop with no `maxVisits` is
  saved with the warning `unbounded_loop`, the run's timeline numbers each
  visit, and the canvas and the run page draw an edge back as a loop.
- A workflow can run the project's tests as a step and report each one
  (#138): `type: test` with a read-only `workspace`, a `command` the test
  tool writes its JUnit report with (`gotestsum --junitfile`, `pytest
  --junitxml`, vitest's or node's junit reporter), the `reports` to read and
  a `timeout`, 15 minutes unless set. Its output counts passed, failed and
  skipped tests, names the failures and each case with the test case keys its
  `[verifies KEY]` tags name, and keeps the end of the log; failing tests are
  a result, while a command that cannot run, runs out of time or writes no
  report fails the step and says why. Reports are read without following a
  link, at most 4 MiB, and one declaring a DTD is refused. The canvas adds
  test nodes, and the run's page says how the tests went.

- A workflow step can work on a checkout of the run's branch (#136):
  `workspace: {repository, branch, image, access}` on an agent or code node.
  The runner fetches the branch at depth 1, an agent reads, searches, edits
  and runs commands in a toolchain image an administrator allows, and with
  `access: write` what it changed is committed and pushed as the person at
  the step's end; a failed step pushes nothing. Commands run as argument
  lists, never shell lines, with a timeout and capped output; no tool follows
  a symbolic link, and what files and commands say is fenced as data. Code
  nodes get the same through `ctx.workspace`. Administrators allow the
  images, name the one package mirror builds reach, and bound a step's time
  and its work tree in the settings (`/settings/workspaces`). Each image runs
  in a toolchain container beside the runner in the run's gVisor pod, started
  by Rotor's new exec agent image, `rotor-agent`; the chart sizes them and
  lets workspace pods reach the mirror.

- A workflow can bind a repository Armature connects to a project (#137):
  `repository: {project, name}` on an Armature binding, fixed or named by
  the run's inputs, checked with `list_repositories` as the person when the
  workflow is saved or the run starts. A run gets the repository's address
  at Armature's git proxy and its own token for it, with `git.write` when the
  binding writes, so no host token reaches Rotor; `create_issue_branch` and
  `open_pull_request` make the issue's branch and its pull request as the
  person. People connected to Armature before connect again to allow pushes.
  Workspaces (#136) clone and push with it.
- How a workflow writes code is agreed (#131): `docs/api-contract-m13.md`
  says how an agent gets its own model, role and step budget, how a loop is
  bounded, how parallel branches join, how a step checks out the run's
  branch of an Armature repository, and how a test step reports. The schema
  carries the fields, and a workflow using one is refused as `not_built`
  until its issue lands; `/settings/workspaces` answers 501 until then.
- The first workflow that holds a conversation on a ticket (#130):
  `examples/story-to-requirements.yaml` asks on a new Armature user story
  what is unclear, starts again when someone answers, then writes or updates
  the requirements in Exciter, links them to the story, asks for their
  review, and says so on the ticket. Its own comments never start it again.
  `docs/triggers.md` says how to set its trigger up against Armature.
- Rotor is tested against the real Armature (#45, #130): `make suite-up`
  starts Armature beside Exciter, signing in through Rotor's Keycloak, with a
  project `CP` and Exciter connected to it. The suite shows Rotor held until
  an Armature administrator allows it, alice's run commenting as alice with a
  token revoked at the run's end while her grant lives on, Armature refusing
  dave's run, and a story filed in Armature turned into reviewed Exciter
  requirements by Armature's own webhook.
- A trigger condition can say what an event must not be (#126): `does not
  equal`, `is none of` and `is missing` beside `equals`, `is one of` and
  `exists`, so a trigger can leave out, for example, internal comments or a
  bot's events. A path the event does not have equals nothing.
- A workflow no longer starts itself on what its own runs wrote (#127). The
  runner reports the comment, issue or requirement revision each write
  answered with, Rotor keeps it with the run, and an event about it within ten
  minutes skips the same workflow's triggers, saying which run wrote it. A
  skipped firing shows its reason on the workflow page.
- A trigger's owner can let named calls write without an approval (#128).
  A binding lists them under `unattended`, the trigger form asks the owner to
  agree to each, and a later version that names more turns the trigger off
  until they agree again. Each such write is in the audit log. An
  administrator forbids them for everyone under Settings, which turns those
  triggers off at once.
- The tool-stub answers as Armature does, so workflows that read a story and
  comment on it are tested in every pull request (#129). An Armature tenant
  lists Armature's own `tools/list`, recorded at `deploy/suite/ARMATURE_REF`
  by `make tool-snapshots TOOL=armature`, and answers `whoami`, `get_issue`,
  `list_comments`, `add_comment`, `list_remote_links` and `add_remote_link`
  from the issues a test gives it. Exciter tenants also search, create,
  revise, link and send requirements for review, and the control API reads
  back what a run wrote.
- Workflows read and write Armature as the person who started them (#45).
  Rotor finds Armature's OAuth metadata where its MCP endpoint points, waits
  until an Armature administrator allows it, which connecting does, and gives
  every run a token of its own exchanged from the person's connection, so
  ending a run never disconnects anybody. Bindings use Armature's scopes:
  `read`, `issue.write`, `issue.transition` and `comment.write`.
- How a triggered workflow talks on a ticket is agreed (#124):
  `docs/api-contract-m12.md` says how a trigger condition leaves events out,
  how a workflow skips what its own runs wrote, and how a trigger's owner
  lets named writes happen without an approval, which an administrator can
  forbid under `/settings/unattended-writes`. The switch answers 501 until it
  is built.
- The README shows what Rotor does, the way Armature's does (#116): how to
  run it, what it does with pictures of it, how it is put together,
  deployed, tested and operated, every environment variable with its
  default, and where everything lives. `make screenshots` takes the pictures
  in `docs/screenshots.md` from a fresh development stack, as a person would
  use it.
- An assistant can connect by the address of its client ID metadata document
  instead of registering (#114). Rotor fetches the document from the public
  internet alone, through a guard that never reaches Rotor's own network
  unless `ROTOR_OAUTH_FETCH_ALLOW` names a host or range, and the consent page
  says how Rotor knows the assistant.
- Workflows can be drawn on a canvas (#46). The editor has a Canvas tab beside
  the YAML: add, remove and rename nodes, draw the ways between them, set a
  router's cases and the node a workflow starts at, and write each node's
  prompt, call, source or message, all with the keyboard too. Every change is
  written into the YAML, keeping your comments and style, so the YAML tab
  shows what you could have written yourself. Nodes the checks found
  something wrong with are marked, and each problem opens the YAML at its
  line.
- Assistants work with Rotor over MCP as the person who connected them (#47).
  `POST /api/v1/mcp` offers workflows and runs, read, checked, started and
  cancelled, and approvals to read, never to decide. An assistant connects
  with Rotor's address alone: it registers, its person allows it on Rotor's
  consent page, to act or only to read, and Settings lists the assistants
  each person connected, to disconnect any. Administrators see everybody's
  and can switch assistants off. The audit log names the assistant beside
  the person, its CSV export in a last column, `via`, and `GET /runs` takes a
  `limit`. `/meta` lists `mcp@1`.
- Rotor joins the suite's capability names (#43). `GET /api/v1/meta` lists
  `workflows@1`, `runs@1` and `hooks@1`, each held by a test to what the build
  serves, and `docs/integration.md` says what each promises. Rotor reads what
  each tool offers: a tool that does not offer `mcp@1` shows on the
  Connections page as needing an update, and one without `webhooks@1` takes
  no webhook secret. Administrators ask Rotor to read an updated tool again
  with Check again under Settings, Tools, which keeps everyone's connection.
  The nightly suite also runs against the siblings' main.
- Rotor can be released (#44). `./release.sh` works out the next version,
  writes it into every file that carries it, turns this section into the
  release's notes and opens the release as a pull request; merging it tags
  the merge commit, and the tag builds the backend, web and runner images
  and the chart, signs each keyless with the release workflow's identity
  and its SBOM as a signed attestation, publishes them to ghcr.io with the
  GitHub release and its digests, and tells Dynamo. The chart's sandbox
  admission trusts that identity when no key is given, so a cluster can
  refuse any runner image a release did not sign. `docs/releasing.md` says
  how to cut, install and check a release.
- An approval's message, preview and values are ciphertext at rest too
  (#91), sealed with their run's data key; the inbox and the approval page
  read as before. Upgrading deletes the runs kept so far once more.
- Text a run reads cannot quietly steer it (#40). Every agent works under a
  fixed policy, and every value its prompt quotes and every answer a tool
  gives it is fenced as data that nothing inside can close. A link an agent
  writes, in its answer or in what it sends a tool, is removed unless it
  leads to one of the run's tools, and the run's timeline says so and links
  only to those tools. The editor warns, without refusing the save, about a
  workflow that writes after reading with no approval in between. A corpus of
  injection attempts runs in CI against a model scripted to obey them.
- What a run read and wrote is ciphertext at rest (#41). Each run has a
  data key of its own, which OpenBao makes and wraps with the
  installation's key; its inputs, output, events, checkpoints and the
  messages for its runner are sealed with it, so a copy of the database
  shows nothing a tool returned. Credentials of known shapes, from GitHub,
  GitLab, Slack, AWS, Google, Stripe and the model providers to JWTs and
  private keys, are cut from every event, run output, span, audit record
  and log line, by the control plane and the runner alike. Upgrading
  deletes the runs kept so far; the audit log keeps their record.
- Finished runs are deleted once kept long enough (#41): 90 days unless an
  administrator sets another number under Settings, or fewer when a
  workflow says so with `retention: 30d`. The worker deletes a run with its
  events, checkpoints, approvals and usage, and records in the audit log how
  many runs of which workflow went; the audit log itself is never deleted.
- Operators can tell what runs in a sandbox and that it is the image that
  was built (#42). `make sbom`, `sign`, `attest` and `verify` list an
  image's contents as an SPDX SBOM and sign the image and its SBOM with
  cosign; the chart names every Rotor image by digest and, with sigstore's
  policy-controller installed, can refuse any sandbox pod whose image is not
  signed (`sandbox.admission`). The kind test signs the images it installs
  and proves an unsigned one is refused. `make audit-deps` runs govulncheck,
  pip-audit and npm audit, nightly and on pull requests that change a
  dependency, and Renovate proposes updates once a release is two weeks old.
- The threat model (#39): `docs/threat-model.md` names what Rotor protects
  and from whom, goes through each component by STRIDE across its trust
  boundaries, works through untrusted content reaching private data, code
  nodes escaping, token replay and spoofed webhooks, and tracks every open
  finding as an issue. `SECURITY.md` says what is in scope for a report.
- Administrators read and export the audit log (#36): a page under
  Settings filters it by action, person, target and dates, walks it window
  by window, and exports what it shows as CSV or JSON, refusing rather than
  cutting an export too large. The log now also records sandboxes started
  and removed, every run's end and each approval asked for, and each export
  of itself; nobody may change or empty it.
- Runaway runs stop before they cost too much or flood a tool (#38). The
  runner reports each model call's tokens, and Rotor totals them per run,
  person and day, at prices administrators set per model. A workflow bounds
  its tokens (now across every sandbox of a run), its cost, how many of its
  runs go at once and its tool calls per minute; administrators bound each
  person's day and concurrent runs. A run over a limit stops and says why,
  one over a concurrency limit waits queued, and a usage page shows what you
  used.
- Each run is one trace (#37): the api and the worker trace requests,
  statements and the run's sandbox, and the runner's nodes, model calls and
  MCP calls join the same trace through the control plane, so a sandbox
  reaches no collector. `make observe` shows them in Jaeger, with Prometheus
  beside it; the gate runs with them and proves one trace per run. Run
  durations and token mint failures are counted, and the api serves its own
  /metrics. An administrator can send runs' traces to LangSmith, after
  confirming that run data leaves the installation; the key stays in
  OpenBao and never reaches a sandbox.
- Armature, Exciter and Stator can deliver their events to Rotor (#35). An
  administrator adds Rotor's webhook address in the tool and pastes the
  secret it shows into Rotor, which keeps it in OpenBao. Rotor checks each
  delivery's timestamped signature, refuses replays older than five minutes,
  reads envelope v1 and Exciter's CloudEvents, ignores an event it already
  received, and starts every event trigger whose topics, projects and
  conditions match, as the trigger's owner with the event as the input
  `event`. A run starts only if the tool shows the owner the event's
  requirement, issue or page, since a payload is read with the rights of
  whoever added the webhook. On a workflow's page, a person adds such a
  trigger for any tool they are connected to; `docs/triggers.md` walks each
  tool through.
- A workflow runs on a schedule as the person who set it (#34). On a
  workflow's page, add a trigger with cron fields and a time zone, see its
  next runs, choose whether a time is skipped while the last run goes on,
  and give the inputs each run starts with. Rotor checks each firing as you
  and turns the trigger off, saying why on the trigger and the Workflows
  page, once your account, a connection it needs or the workflow is gone.
  Until writes are confirmed at the tool, a triggered workflow must ask for
  an approval before every write.
- The triggers API is agreed (#33): `docs/api-contract-m6.md` says how a
  person schedules a workflow or starts it from Armature, Exciter and Stator
  events as themselves, how Rotor checks the tools' signed deliveries, and
  when it turns a trigger off. Its operations answer 501 until they are
  built.
- A person decides before a run goes on (#31). A run that reaches an
  approval waits, keeping no sandbox and no token, and shows what it is
  about to write. Its starter, or the group the step names, approves it,
  changing the fields the step offers, or rejects it, with a comment; the
  run goes on in a fresh sandbox or ends. An approval nobody decides in
  time ends its run. The Approvals inbox lists what waits for you, with a
  badge in the sidebar and a link to each approval. Decisions are in the
  audit log, and no workflow may call a tool that decides a review.
- The approvals API is agreed (#30): `docs/api-contract-m5.md` says who sees
  and decides an approval, what it shows of the writes to come, how a
  decision resumes a run, and how write confirmation will be held at the
  tool. Its operations answer 501 until they are built.
- A sandbox reaches only what it needs (#27). The chart denies sandbox
  egress by default and allows cluster DNS, the api's runner port, OpenBao,
  and the tools and the model provider by hostname through Cilium (or by
  address range without it). Cloud metadata, the rest of the cluster and
  every other host are refused, which the kind test, now running Cilium,
  proves from a pod with the sandbox's label.
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

### Changed

- The sandbox namespace is named from the release by default,
  `<release namespace>-<fullname>-sandboxes`, or `<fullname>-sandboxes` when
  the two are the same, so two installs in one cluster never share one
  (#169). An install that relied on `<release namespace>-sandboxes` sets
  `sandbox.namespace` to keep it. The `ClusterImagePolicy` objects that
  admit signed sandbox images are named after that namespace too.

### Fixed

- Signing in to Rotor accepts return addresses only on localhost and 127.0.0.1
  themselves; `http://localhost*` also let in hosts that merely start with
  them, such as `localhost.example`. The realm matches Dynamo's again, so
  `make realm-check` passes (#166).
- A workflow with a `maxTokens` of a million or more is saved; such a number
  was written with an exponent on its way into the spec, and refused.
- The run page draws every node of its graph when the run's events arrive in
  a burst, as they do on opening a finished run; a node redrawn before it was
  measured stayed hidden, with its edges.
- A trigger on Armature's events matches its projects and starts its run
  (#125). Rotor reads the issue key where Armature sends it, at the top of
  the payload, and checks the person can read that issue before the run
  starts.
- The development realm carries the token-exchange audience mappers Dynamo's
  canonical realm added to Armature, Exciter and Stator, and `make
  realm-check` compares the realm Dynamo holds, leaving Rotor's own clients
  and group aside until Dynamo carries them (#109).
- A workflow that writes through a binding asking for an approval cannot
  start without one after a tool turned a call into a write (#90). Every
  start, by hand or from a trigger, checks the workflow against the tools
  read afresh as the run's person, and refuses with `tools_changed`.
- OpenBao keeps a record of who read Rotor's secrets (#92): the
  development stack declares an audit device, and Rotor's init script
  refuses an OpenBao without one. The chart and the api refuse a Valkey
  without a password outside development, and `valkey.tls` reaches one over
  TLS.
- A workflow's code node can no longer read the runner it runs under
  (#89). The runner makes itself not dumpable before it reads anything
  secret, so a code node cannot trace it or open its memory, environment
  or files, where its tool tokens and the provider key are.
- A person signing in to Rotor for the first time from two browsers at
  once lands signed in in both, instead of the second failing (#96).
- Signing a person out of every session in Keycloak ends all their Rotor
  sessions, not only the one its logout token names (#53). Rotor reads the
  time of that sign-out from Keycloak a moment after the token arrives, with
  the `rotor` client's service account, which now holds `view-users`; ending
  one Keycloak session still ends only the Rotor session it signed in.
- A run resumed after an approval answers only that approval: a sandbox
  replaced later no longer replays the decision to the next one.
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
