# API contract for M13: workflows that write code

M13 lets a workflow implement a feature specified in Exciter: test designers,
a coder and reviewers as separate agents, a checkout of the run's branch for
the steps that work on code, the project's tests run as a step, and loops
that go on until every reviewer agrees or a person decides. Six changes to
the workflow spec make that possible:
- an agent node with its own model, role and step budget (#133)
- a loop that runs at most so often, then goes where the author says (#134)
- parallel branches and a node that waits for all of them (#135)
- a repository binding, through Armature (#137)
- a workspace on agent and code nodes: the run's branch checked out (#136)
- a `test` node that runs the project's tests and reports each (#138)

This file is #131. The schema `api/workflow.v1.schema.json` is the source of
truth for the spec; this file says what it means. Until the issue that builds
a field merges, a workflow that uses it is refused when saved and when a run
starts, with the problem `not_built` naming the issue. Settings routes carry
`pending: true` and answer 501 until built.

Conventions are M2's: paths under `/api/v1`; refusals are sentences.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | Tables |
  |---|---|---|
  | 01200-01219 | #133 | the model a run used, per node, in `run_usage` |
  | 01260-01279 | #136 | `workspace_setting`, numbered after #135's 01240, which merged first |

- Packages: `internal/workflow/spec` and the runner's `spec.py` (every field),
  `runner/src/rotor_runner/compile.py` (models, visits, joins),
  `runner/src/rotor_runner/workspace` (#136), `internal/sandbox` (the
  workspace pod, #136), `internal/tools/profiles` (the repository binding,
  #137).
- Limits, in `internal/config`:
  - `MaxRoleChars` = 8000 characters of an agent's role.
  - `MaxVisits` = 50 visits of one node.
  - `MaxWorkspaceSteps` = 50 workspace steps in one run.
  - `MaxCommandOutput` = 64 KiB of a command's output an agent reads.
  - `MaxTestReport` = 4 MiB of JUnit a test step reads.

## An agent's own model, role and steps (#133)

```yaml
- id: security
  type: agent
  model: { name: claude-opus-5-5, provider: anthropic }
  role: |
    You are a security reviewer. You look for injection, broken access
    control and secrets in code, and you never approve what you did not read.
  maxSteps: 60
  prompt: Review the change on the run's branch against {{ nodes.spec.cases }}.
```

- `model` has the shape of the workflow's `model`, and the same check: an
  administrator allows it. Without one, the node uses the workflow's.
- `role` is put under Rotor's fixed policy as the agent's instructions,
  outside the fences, at most `MaxRoleChars`. It may not hold templates: what
  a run read never becomes an instruction.
- `maxSteps` caps the agent's own model and tool steps, at most the
  workflow's `limits.maxSteps`; without one, the workflow's.
- Usage and `maxCost` count each call at its own model's price. A run's
  usage names each model it used; the run page shows each agent's.

### As built (#133)

- **A role that is a template** is refused with its own problem,
  `role_template`, at `/nodes/<i>/role`, when saving and in the runner.
- **Only an agent's own model is checked when saving.** The workflow's
  `model` is resolved when a run starts, as before, and an agent on it fails
  with `model_missing` when it resolves to none. A run whose agent's own
  model is no longer allowed is refused when it starts, with the usage
  refusal `model_not_allowed` (422), beside `unpriced_model`; `maxCost`
  needs prices for every model the run uses.
- **The protocol:** `StartRun.agent_models` (field 11) maps an agent node
  with a model of its own to its `ModelConfig`; the others use
  `StartRun.model`. An agent whose model resolved to none is missing from the
  map and fails with `model_missing`, never on the workflow's model. A
  `SecretGrant` goes out for each provider the run's models use.
- **Usage per agent** is the table `run_usage_agent` (migration 01200), one
  row per agent node with its provider, model and the prices that model had
  when the run's stream first opened. An `llm.usage` event is priced by its
  `node`'s row, else at the run's prices; `run_usage` keeps the run's totals.
  `GET /runs/{id}` carries them as `usage.agents: [{node, provider, model,
  inputTokens, outputTokens, cost}]`; a list of runs does not.
- **The role in the prompt** follows the policy after a blank line, as "The
  workflow's author gives you this role:", with any fence marker in it
  defanged.

## Loops (#134)

```yaml
- id: code
  type: agent
  maxVisits: 5
  onExhausted: escalate
  ...
- id: verdict
  type: router
  on: nodes.gather.verdict
  cases: { approve: finish, changes: code }
```

- Edges and router cases may lead back to an earlier node; they always could.
- `maxVisits` on any node counts its visits. The visit past it does not run
  the node: the run goes to `onExhausted`, or, without one, ends as
  `failed` with the reason `loop_exhausted`.
- `visits.<id>` is how often a node has run so far, readable in templates
  (`{{ visits.code }}`) and in a router's `on`. State paths take
  `inputs.`, `nodes.` and `visits.`.
- A node's output is its latest visit's. The run's timeline numbers each
  visit, and the canvas draws an edge back as a loop.
- **Warning** `unbounded_loop`, on save: a cycle in which no node carries
  `maxVisits`. `limits.maxSteps` still ends such a run.

### As built (#134)

- **The state** is `{inputs, nodes, visits}`: a node adds its visit to
  `visits` when it finishes, so the count is in every checkpoint and an
  approval that resumes counts on. A node that has not run reads 0.
- **The guard is on the way in:** every way into a node with `maxVisits`
  checks the count as the run takes it, so a node never starts past its last
  visit, and a chain of `onExhausted` nodes with no visit left either ends the
  run with `loop_exhausted` rather than going round.
- **Events:** `node.started` carries `visit`, from 1, as `{node, type,
  visit}`. A resumed approval starts again with the same number.
- **The graph** (`Spec.Graph()` and the canvas) draws `onExhausted` as an edge
  labelled `onExhausted`, and the checks count it as a way on, so a node only
  it reaches is not `unreachable`. An edge back to a node on the way to it is
  drawn dashed, with an arrow.
- **The warning** treats a way into a node with `maxVisits` as also going to
  its `onExhausted`: a loop that only goes there and back is still unbounded.
  It is one warning per loop, at its first node.
- **Templates** naming no node are now refused in prompts and approval
  messages too, for `nodes.` and `visits.` alike; arguments and form values
  were checked already. Code nodes get `state["visits"]`.

## Parallel branches (#135)

```yaml
edges:
  - { from: test, to: review-code }
  - { from: test, to: review-security }
  - { from: review-code, to: gather }
  - { from: review-security, to: gather }
nodes:
  - { id: gather, type: code, join: all, ... }
```

- Several edges out of a node start their targets at once; they always did.
- `join: all` runs a node once, after every node with an edge into it in the
  current pass has ended. Without it, a node with several incoming edges
  runs once per arriving branch, and the save warning
  `join_missing` says so.
- Parallel branches that each reach an approval wait together: every waiting
  approval is in the inbox, and the run goes on once each is decided.
- Limits, usage and the tool call rate count every branch.

### As built (#135)

- **The join is LangGraph's deferred node** (`defer=True`), not an edge from
  several sources: it runs once no branch has a step left. That holds when a
  branch reaches it through a router or past a counted node (#134), where
  `add_edge([a, b], c)` cannot, and it means a join also waits for a branch
  that does not lead to it. The `onExhausted` nodes past a join wait the same
  way, so a join with no visit left sends the run on once, not per branch.
- **Without a join** branches that arrive in the same step start a node once,
  and branches of different lengths start it once each.
- **The warning** counts `edges` only, not router cases or `onExhausted`, and
  leaves out an edge that closes a loop through the node.
- **Approvals wait together when they are reached in the same step.** A run
  stops at the end of the step its first approval is reached in, so a branch
  that would reach its own approval a step later gets there once the first
  wait is decided.
- **The protocol:** a waiting run's `Done.output_json` is the first interrupt,
  as before, with `waiting` listing every one when there are several.
  `StartRun.resume_answers` (field 11) maps each interrupt id to its answer;
  `resume_json` and `resume_interrupt_id` still carry the first of them.
- **The control plane:** migration 01240 drops the index that allowed one
  pending approval per run. A decision resumes the run only when none of its
  approvals is pending any more; the run's row is held, so of two decided at
  once the later resumes it. The answers are the approved approvals of the
  run's last wait, which share `requested_at` since one `Done` opens them in
  one transaction. A rejection ends the run and cancels the others; an expiry
  ends it as `approval_expired`, as with one.
- **The API:** a run carries `approvalIds`, every approval it waits for in the
  order they were opened; `approvalId` stays the first. A graph's nodes carry
  `join`, and the canvas and the run page draw a join below the longest
  branch into it, the branches side by side above it.
- **Limits:** `maxSteps` counts every node a branch starts in a sandbox, where
  LangGraph alone counts a step of parallel nodes as one; it ends the run on
  the same node a single branch always did. The tool call rate and
  `maxTokens` were already counted over every node of the run, and usage is
  reported per model call.
- **The canvas** leaves `join` in the YAML: its main fields are one text each
  per kind of node, and a join is a choice any node may make.

## The repository binding (#137)

```yaml
tools:
  repo:
    tool: armature
    scopes: [read, git.write, issue.write]
    access: write
    repository: { project: "{{ inputs.armatureProject }}", name: "{{ inputs.repository }}" }
    allowTools: [create_issue_branch, open_pull_request]
```

- A binding with `repository` names a repository Armature connects to a
  project: `project` is the project key, `name` the repository's `owner/name`.
  Both may be templates of `inputs` alone.
- Its tool must be one whose profile offers git: Armature, once it lists its
  git proxy in `/meta` (Cloudster1/Armature#333). The clone address is its git
  proxy, `{base}/api/v1/git/{project}/{name}.git`, and the credential the
  run's own Armature token for this binding. No host token reaches Rotor.
- The binding's `allowTools` are Armature's MCP tools as with any binding:
  `create_issue_branch` names the branch, `open_pull_request` ends the work
  (Cloudster1/Armature#332).
- When saving, a fixed `project` and `name` are checked with
  `list_repositories` as the person; templates are checked when the run
  starts.

### As built (#137)

- **The scope follows the access.** A repository binding with
  `access: write` asks for `git.write` beside its `scopes`, and one with
  `read` never does, whatever `scopes` lists; the start page's bindings and
  the check that the person consented use the same scopes.
- **Templates at the start.** A run whose inputs leave a repository's name
  out, or name one the person does not see, is refused as `inputs_invalid`,
  the problem placed at the input (`inputs/repository`), not as
  `tools_changed`.
- **The runner protocol.** `ToolBinding.repository` carries `project`,
  `name` and `clone_url`, at an address the sandbox reaches; the credential
  is the binding's token from `TokenRequest`. The runner offers it as
  `RunContext.repository(binding)`, an address and token for its own git
  alone (#136): code nodes never get it.
- **The tool double** answers `list_repositories`,
  `get_issue_development`, `create_issue_branch` and `open_pull_request`,
  lists `git-proxy@1`, and serves its repositories over git's smart HTTP
  (version 0, shallow fetches included), refusing what Armature's proxy
  refuses.

## Workspaces (#136)

```yaml
- id: code
  type: agent
  tools: [exciter-read]
  workspace: { repository: repo, branch: "{{ nodes.branch.name }}", image: golang-1.26, access: write }
  prompt: Implement {{ nodes.spec.requirements }} with tests tagged as the cases say.
```

- `workspace` on an agent or code node checks out `branch` of the
  repository binding at the step's start, in a sandbox with the toolchain
  `image`. The image is a name an administrator allows, not a reference.
- With `access: write` the step commits what changed and pushes the branch
  at its end; a step that fails pushes nothing. With `read` it pushes never.
- An agent gets the tools `list_files`, `read_file`, `search` and
  `run_command`, and with `write` also `write_file` and `edit_file`. A code
  node gets `ctx.workspace` with the same calls.
- `run_command` runs an argument list in the toolchain image, never a
  shell line, with a timeout and at most `MaxCommandOutput` of output read.
- What a file or a command says is fenced as data, as a tool's answer is.
- Nothing but the branch is kept between steps; a waiting run holds no
  workspace.
- Egress of a workspace step: the run's tools, the model provider and the
  package mirror an administrator names.
- **Settings:** `GET`/`PUT /settings/workspaces` (admin):
  `{images: [{name, reference}], packageMirror, maxStepTime, maxVolume}`.
- **The shape, as #132 decided** (`docs/decisions.md`, "Workspaces in a
  toolchain container beside the runner"):
  - a run whose workflow has workspace steps gets, in its gVisor pod, one
    toolchain container per image beside the runner, sharing the work tree;
  - the runner alone does git: it fetches the run's branch at depth 1,
    commits and pushes, so the run's token never reaches the container the
    repository's code runs in, and the git directory stays where only the
    runner sees it;
  - commands reach the toolchain container through a small exec agent an
    init container copies in, over the pod's loopback, with a secret per
    sandbox that code nodes do not hold; between steps it ends every process
    it started;
  - the workspace tools never follow a symbolic link, so code that plants
    one cannot make a read leak what the runner holds;
  - builds fetch dependencies from the one package mirror an administrator
    names (`GOPROXY`, `GOSUMDB`, `npm_config_registry`);
  - in compose the toolchain joins the runner's network namespace the same
    way, for the gate.

### As built (#136)

- **Settings.** `maxStepTime` (`1h` unless set, from `1m` to `6h`) bounds a
  whole workspace step, and `maxVolume` (`1Gi` unless set, from `64Mi` to
  `8Gi`) the work tree. An image's `reference` must carry its digest, a name
  is lowercase as workflows give it, and `packageMirror` is one `http(s)`
  address without credentials, or empty. A change is audited as
  `workspaces.changed`. Without a mirror `GOPROXY` is `off`.
- **Checks.** `workspace_binding` also refuses `access: write` through a
  binding without `access: write`, and a `branch` reading anything but
  `inputs` or a node. `image_not_allowed` is checked when saving and when a
  run starts (`422 image_not_allowed`); an image unallowed between the start
  and the sandbox ends the run with the reason `image_not_allowed`.
- **The sandbox.** The worker reads the pinned version's images, resolves
  them with the setting and puts them in `sandbox.Spec.Workspace`; nothing
  changed in the runner protocol. The runner learns its toolchains from
  `ROTOR_WORKSPACE` (`{agents: {image: port}, stepSeconds}`) and the agents'
  secret from `ROTOR_WORKSPACE_SECRET`, which it drops from its environment
  as it does the bootstrap token and holds in its redactor. Agents listen on
  `127.0.0.1:7700` and up, one port per image. The exec agent is the image
  `rotor-agent` (`deploy/Dockerfile.agent`, `backend/cmd/workspace-agent`),
  released and signed with the others. A pod with toolchains is labelled
  `rotor.dev/workspace`, which the mirror's egress selects; its runner's end
  ends the sandbox, as the toolchains serve on.
- **Steps.** Each step gets `/workspace/<n>-<node>` and
  `/private/<n>-<node>`, both removed at its end after the agent's reset.
  The runner commits as `Rotor <rotor@rotor.invalid>`; the push carries the
  person's token. A push whose branch moved on since the fetch fails the
  step. A step's events: `workspace.checkout` (branch, commit, image),
  `workspace.command` (argv, exit code, duration, the output's tail),
  `workspace.pushed` (branch, commit, parent) or `workspace.unchanged`.
  Failures end the run with `workspace_error`, `workspace_timeout`,
  `workspace_binding`, `workspace_branch`, `workspace_steps` or
  `workspace_unavailable`.
- **Runner code for #138.** `async with ctx.workspaces.step(node, state,
  ctx) as ws:` gives a step's `Workspace`; `await ws.run_command(argv,
  cwd=".", timeout_s=None)` answers a `CommandResult` (`exit_code`,
  `timed_out`, `duration_ms`, `output`, `truncated`), and
  `ws.tree.read_bytes(path, limit)` reads a file without following a link.
- **Checks of the shape.** The runner image's `--workspace-check` proves a
  sandbox from inside: the agent refuses a caller without the secret, the
  work tree is shared, a planted link is refused and committed as a link,
  the reset ends a process that left its group, and a push lands. The Docker
  backend's integration test and the Kubernetes test under gVisor run it.

## The `test` node (#138)

```yaml
- id: test
  type: test
  workspace: { repository: repo, branch: "{{ nodes.branch.name }}", image: golang-1.26, access: read }
  command: [gotestsum, --junitfile, reports/junit.xml, --, ./...]
  reports: [reports/junit.xml]
  timeout: 20m
```

- Runs `command` in the workspace and reads the JUnit files `reports` names,
  at most `MaxTestReport`.
- Its output: `{passed, failed, skipped, failures: [{name, message, file}],
  cases: [{name, result, verifies: [KEY]}], logTail}`. `verifies` comes from
  `[verifies KEY]` tags in a test's name.
- Failing tests are a result, not a failed step; a command that cannot run
  fails the step with the reason.
- A command has no shell to redirect output, so the test tool writes the
  report itself, such as `gotestsum` in a Go image.
- `timeout` is the step's own, 15 minutes when unset and at most the run's:
  each step starts with empty build caches, so a test step pays a cold
  build (about four minutes for Rotor's backend under gVisor).

### As built (#138)

- **Checks.** A new problem, `test_node`: a test node whose workspace has
  `access: write` (a test step only reads, and the runner forces `read`
  whatever the spec says), one naming no report, a report path that is
  absolute, climbs out with `..` or names no file, and a `timeout` longer
  than the run's `limits.timeout`.
- **The command** runs from the work tree's root through
  `Workspace.run_command`, for its `timeout` (15 minutes unless set) and at
  most what is left of the step. Its `workspace.command` event is #136's.
- **The reports.** `MaxTestReport` (4 MiB, in `internal/config` and the
  runner's `config.py`) bounds all of a step's reports together. Each is read
  with `WorkTree.read_bytes`, so never through a link. The parser is expat
  with any DTD refused, so no entity is expanded or fetched; no new
  dependency. Test cases are found wherever suites nest them, as node's own
  junit reporter writes some outside any suite.
- **The output** is the agreed `{passed, failed, skipped, failures: [{name,
  message, file}], cases: [{name, result, verifies}], logTail}`:
  - `name` joins the case's `classname` and `name` with ` > `; `result` is
    `passed`, `failed` (an `<error>` too) or `skipped`.
  - `verifies` reads `[verifies KEY]` in the name and the classname, several
    keys apart by commas or spaces, with Go's underscores for spaces, and a
    JUnit property `verifies` (pytest's `record_property`). A key starts with
    a letter and holds letters, digits, `.` and `-`.
  - `file` is the case's `file`, its suite's, or its classname, relative to
    the work tree.
  - Caps (`config.py`): `cases` at most 2000, those that verify a case or fail
    kept first; `failures` at most 50; a name 300 and a message 2000
    characters, start and end kept; `logTail` the last 8 KiB. The counts hold
    every case.
- **Failures** end the run with `test_command` (the command could not start,
  or exited with an error while its reports name no failing test),
  `test_timeout` or `test_report` (missing, a link, larger than the cap, not
  JUnit, or declaring a DTD); the reason quotes the last 500 characters of the
  output. A checkout or push failure stays #136's `workspace_*`.
- **A new event, `test.reported`** (node: the test node): `{passed, failed,
  skipped, failures: [{name, file}]}`, the first ten failures, which the run's
  page shows. The canvas draws the node as `Tests`, and shows `command` as a
  line the YAML changes.

## Problems and warnings

| Code | Kind | When |
|---|---|---|
| `not_built` | problem | a field whose issue has not merged yet |
| `model_not_allowed` | problem | an agent's model is not one an administrator allows |
| `steps_over_limit` | problem | an agent's `maxSteps` above the workflow's |
| `exhausted_to_nowhere` | problem | `onExhausted` names no node |
| `repository_binding` | problem | `repository` on a tool without git, or a template of anything but `inputs` |
| `workspace_binding` | problem | `workspace.repository` names no repository binding |
| `image_not_allowed` | problem | an image no administrator allows |
| `test_node` | problem | a test node that writes, names no report or one outside the work tree, or outlasts its run |
| `unbounded_loop` | warning | a cycle with no `maxVisits` |
| `join_missing` | warning | several incoming edges and no `join` |

## What is left for later

- A vendor's coding agent as a node type; `docs/decisions.md`, 2026-10-22,
  says why not now.
- Workspaces that outlive a step, such as a warm cache between them.
