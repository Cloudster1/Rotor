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
- The pod's shape, the command channel and the sizes are the ones #132
  decides; this section is corrected from its findings.

## The `test` node (#138)

```yaml
- id: test
  type: test
  workspace: { repository: repo, branch: "{{ nodes.branch.name }}", image: golang-1.26, access: read }
  command: [go, test, -json, ./...]
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
