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
  | 01220-01239 | #136 | `workspace_setting` |

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
| `unbounded_loop` | warning | a cycle with no `maxVisits` |
| `join_missing` | warning | several incoming edges and no `join` |

## What is left for later

- A vendor's coding agent as a node type; `docs/decisions.md`, 2026-10-22,
  says why not now.
- Workspaces that outlive a step, such as a warm cache between them.
