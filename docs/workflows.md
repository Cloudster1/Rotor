# Writing workflows

A workflow is a YAML file that says what a run does in Exciter, Armature and
Stator, and with which tools and model. A run always acts as the person who
starts it: it can do no more in a tool than that person may, and a workflow
shared with colleagues never shares its author's access.

The schema is [`api/workflow.v1.schema.json`](../api/workflow.v1.schema.json); editors offer completion
from it. The three examples below are in
[`api/workflow-fixtures/valid`](../api/workflow-fixtures/valid), where the
control plane's and the runner's tests check them.

## The parts of a workflow

```yaml
apiVersion: rotor.dev/v1      # always this
kind: Workflow
metadata:
  name: draft-requirements    # lower case letters, digits and dashes
  description: What it is for.
inputs: {}                    # a JSON Schema for what the person gives at the start
tools: {}                     # the tools a run may use, by a name the nodes refer to
model: { name: claude-sonnet-5-5 }   # for agent nodes; must be one an administrator allows
limits: { maxSteps: 40, timeout: 15m, maxTokens: 200000, maxCost: 2.5, maxConcurrent: 3 }
entry: draft                  # the node a run starts at
nodes: []                     # the steps
edges: []                     # which step follows which
```

### Tools

Each binding names a tool in Rotor and narrows what a run may do there:

```yaml
tools:
  exciter-read:
    tool: exciter                 # the tool's name in Rotor
    scopes: [mcp:read]            # what the run's token asks for, within the person's consent
    allowTools: [search_requirements, get_project]   # the MCP tools offered; nothing else is
  exciter-write:
    tool: exciter
    scopes: [mcp]
    access: write                 # read, the default, offers only tools that say they only read
    approval: beforeWrite         # a person approves before anything is written through it
    allowTools: [create_requirement]
```

Give each binding the least it needs. An agent with a read binding cannot
write, however its prompt or the content it reads is worded.

Each tool names its scopes its own way:

| Tool | Reading | Writing |
|---|---|---|
| Exciter | `mcp:read` | `mcp` |
| Armature | `read` | `issue.write`, `issue.transition`, `comment.write`, `git.write`; `read` goes along with each |

At Armature a run's token carries only what its binding asks for: a read
binding without scopes asks for `read`, and the token cannot write even where
the person could.

#### A repository binding (#137)

A binding with `repository` is a repository Armature connects to a project,
which the run clones and pushes through Armature's git proxy, as its person:

```yaml
tools:
  repo:
    tool: armature
    scopes: [read, issue.write]
    access: write
    repository: {project: "{{ inputs.project }}", name: "{{ inputs.repository }}"}
    allowTools: [list_repositories, create_issue_branch, open_pull_request]
```

- `project` is the project key and `name` the repository's `owner/name`, as
  `list_repositories` gives it. Each is fixed, or a template of the run's
  `inputs` alone, never of what a node read.
- The tool must offer git: Armature, once it lists `git-proxy@1` in its
  `/meta`.
- The run clones from the git proxy, `{Armature}/api/v1/git/{project}/{owner}/{name}.git`,
  with the binding's own token as a bearer. No host token reaches Rotor.
- With `access: write` the token also carries `git.write`, which pushes to a
  branch made for an issue the person may edit, never to the default branch;
  people connect Armature again to consent to it. With `read` the token
  never carries `git.write`, whatever `scopes` lists, and cannot push.
- `create_issue_branch` makes the issue's branch, and `open_pull_request`
  opens its pull request into the default branch. Both write, so the rules
  for writes, approvals and unattended calls apply to them as to any call.
- A step with a [workspace](#workspaces-136) checks the branch out, and
  with `access: write` pushes what it changed.

A run a trigger starts writes only after an approval node, except through
calls its binding names `unattended` (#128). The trigger's owner agrees to
each of them, and an administrator can forbid them for everyone;
[triggers](triggers.md#what-a-triggered-run-may-do) says how.

```yaml
  armature-write:
    tool: armature
    scopes: [comment.write]
    access: write
    allowTools: [add_comment]
    unattended: [add_comment]     # may write in a triggered run with no approval before it
```

### Nodes

| Type | What it does | Fields |
|---|---|---|
| `agent` | a model that may call the tools of its bindings | `prompt`, `tools`, `output.schema`, `model`, `role`, `maxSteps` |
| `tool` | one MCP call | `tool` (a binding), `call`, `arguments` |
| `code` | a Python function | `source`, `output.schema`, `tools`, `timeout`, `memory` |
| `approval` | waits until a person decides | `message`, `approvers`, `form`, `expiresIn` |
| `router` | chooses the next node by a value | `on`, `cases`, `default` |
| `test` | runs the project's tests in a workspace and reports each case | `workspace`, `command`, `reports`, `timeout` |

A node's output is kept under its id: `nodes.draft.title` is the `title` the node
`draft` returned, and `inputs.project` is what the person gave. A string in a
tool node's arguments, or in a prompt, may hold `{{ inputs.project }}` or
`{{ nodes.draft.title }}`. These are paths into the run's state, never code.

An agent is a specialist when it says so (#133). Each of these is optional:
- `model`: a model of its own, of the same shape as the workflow's `model`.
  It must be one an administrator allows: the check runs when the workflow is
  saved and again when a run starts. Without it, the agent uses the
  workflow's model.
- `role`: who the agent is, put under Rotor's fixed rules as its instructions,
  at most 8000 characters. A role is plain text and never a template: what a
  run read goes into the prompt, fenced as data, never into the instructions.
- `maxSteps`: how many model and tool steps the agent may take, at most the
  workflow's `limits.maxSteps`, which it has without one.

Each model call counts at its own model's prices toward the run's `maxCost`,
and the run's page shows which model each agent ran on and what it used.

```yaml
- id: security
  type: agent
  model: { name: claude-opus-5-5, provider: anthropic }
  role: |
    You are a security reviewer. You look for injection, broken access
    control and secrets in code, and you never approve what you did not read.
  maxSteps: 30
  prompt: Review the change {{ inputs.change }} for security problems.
```

A code node defines `def run(state, ctx) -> dict`, runs in a separate process
with no access to the run's tokens, may use only the libraries the runner
image has, and calls tools through `ctx.call_tool(binding, name, arguments)`.
It returns JSON that must match `output.schema`.

An approval shows its message, its form and the writes the run may make before
the next approval, then waits. The person who started the run decides, or,
with `approvers: {group: quality}`, only the members of that group do. A
`form` offers fields an approver may change before approving: a JSON Schema
object and the values the run proposes, as templates. The approved values are
the node's output, with `approved`, `comment` and `decidedBy`, so the nodes
after it read `{{ nodes.review.title }}`. A rejection ends the run, and so
does an approval nobody decides within `expiresIn` (72h unless it says, at
most 168h).

```yaml
- id: review
  type: approval
  message: File "{{ nodes.draft.title }}" in Exciter?
  approvers: { group: quality }
  expiresIn: 48h
  form:
    schema:
      type: object
      properties: { title: { type: string } }
    values: { title: "{{ nodes.draft.title }}" }
```

A router reads one value and picks a case; its next nodes come from its cases,
never from `edges`. A node with no edge after it ends the run.

### Loops (#134)

An edge or a router's case may lead back to an earlier node, so a coder and
its reviewer can go round until the review passes. `maxVisits` on a node, from
1 to 50, bounds how often it runs; the visit past it does not run the node:
the run goes to `onExhausted` instead, or, without one, ends as `failed` with
the reason `loop_exhausted`. Any node may carry both.

```yaml
- id: write
  type: agent
  maxVisits: 5
  onExhausted: escalate        # a person takes over after the fifth attempt
  prompt: You have tried {{ visits.write }} times so far. {{ nodes.review.notes }}
- id: verdict
  type: router
  on: nodes.review.verdict
  cases: { approve: done, changes: write }
```

`visits.<id>` is how often a node has run so far, 0 before it first runs:
templates read it as `{{ visits.write }}`, a router as `on: visits.write`, and
a code node as `state["visits"]["write"]`. The count is kept with the run, so
it carries on after an approval and in a sandbox that replaced a lost one. A
node's output is its latest visit's, and the run's timeline numbers the visits
of a node that ran more than once. A loop in which no node sets `maxVisits` is
saved with the warning `unbounded_loop`; only `limits.maxSteps` ends it.

### Parallel branches (#135)

Several edges out of a node start their targets at once, so reviewers work
side by side. `join: all` on the node they lead to runs it once, after every
branch still running has ended, so a review round takes as long as its slowest
reviewer. Without `join`, a node runs once for each branch that reaches it at
a different step, and the workflow is saved with the warning `join_missing`.

```yaml
edges:
  - { from: test, to: review-code }
  - { from: test, to: review-security }
  - { from: review-code, to: gather }
  - { from: review-security, to: gather }
nodes:
  - { id: gather, type: code, join: all, ... }
```

A join waits for every branch, also one that does not lead to it. With
`maxVisits`, the visit past its last goes to its `onExhausted` once, after the
branches, as the join would. Approvals that parallel branches reach in the same
step wait together: each is in the inbox, the run page links to each, and the
run goes on once every one is decided. One rejected ends the run, and one
expired expires it, as a single approval does.

### Workspaces (#136)

An agent or a code node with `workspace` works on a checkout of the run's
branch of a repository binding, in a toolchain image an administrator allows:

```yaml
- id: code
  type: agent
  workspace: {repository: repo, branch: "{{ nodes.branch.branch.name }}", image: golang-1.26, access: write}
  prompt: Implement {{ inputs.issue }} with a test, and run the tests until they pass.
```

- `repository` names a [repository binding](#a-repository-binding-137);
  with `access: write` that binding needs `access: write` too.
- `branch` is the branch to work on, fixed or a template of the run's inputs
  or a node's output, such as the branch `create_issue_branch` made.
- `image` is a name from the images an administrator allows in the
  settings, never a reference; a workflow naming another is refused when it
  is saved and when a run starts.
- At the step's start the runner fetches the branch alone, at depth 1. At
  its end, with `access: write`, it commits what changed and pushes it with
  the run's token, as the run's person. A step that fails pushes nothing, and
  neither does one that reads. A branch someone moved on meanwhile is never
  overwritten; the step fails and says so.
- An agent gets the tools `list_files`, `read_file`, `search` and
  `run_command`, and with `write` also `write_file` and `edit_file`. What a
  file or a command says reaches the model fenced as data, like any tool's
  answer. A code node gets the same calls as `ctx.workspace`, such as
  `ctx.workspace.run_command(["go", "test", "./..."])`, which answers
  `exitCode`, `timedOut`, `durationMs`, `output` and `truncated`.
- `run_command` runs a list of arguments in the toolchain, never a shell
  line: `["go", "test", "./..."]`, or `["sh", "-c", "..."]` where a shell is
  what you want. It may run 15 minutes unless the call says, at most what is
  left of the step; past that its whole process group ends. The output keeps
  its first and last 32 KiB.
- No tool follows a symbolic link and every path stays inside the work tree,
  so a link the repository or a command plants never reads the runner's own
  files. A link is committed as a link.
- Nothing but the branch is kept between steps: each step starts from the
  pushed branch with empty build caches, and what a step started is ended
  when it ends. A run waiting for an approval holds no workspace.
- Builds fetch packages from the one mirror an administrator names, as
  `GOPROXY`, `GOSUMDB` and `npm_config_registry`; nothing else on the
  internet. Without a mirror, `GOPROXY` is `off`.
- A run takes at most 50 workspace steps; a step takes at most the time an
  administrator sets (an hour unless they say), and its work tree at most the
  size they set (1 GiB unless they say).

#### A test step (#138)

A `test` node runs the project's tests on a checkout of the run's branch and
reports each test case:

```yaml
- id: test
  type: test
  workspace: {repository: repo, branch: "{{ nodes.branch.branch.name }}", image: golang-1.26, access: read}
  command: [gotestsum, --junitfile, junit.xml, --, ./...]
  reports: [junit.xml]
  timeout: 20m
```

- `workspace` is a [workspace](#workspaces-136) that only reads: a test step
  never pushes, and `access: write` is refused (`test_node`).
- `command` is a list of arguments run in the toolchain without a shell, so
  it cannot redirect: the test tool writes the report itself, such as
  `gotestsum --junitfile junit.xml -- ./...` for Go, `pytest
  --junitxml=junit.xml` or `vitest run --reporter=junit
  --outputFile=junit.xml`, or `node --test --test-reporter=junit
  --test-reporter-destination=junit.xml`; node's reporter does not make the
  directory a report goes into.
- `reports` are the JUnit files the command writes, as paths inside the work
  tree; at least one. The runner reads them after the command, never through
  a symbolic link, at most 4 MiB together, and refuses a report that declares
  a DTD or an entity, which no JUnit file needs.
- `timeout` is how long the command may run, 15 minutes unless the node says
  and at most the run's `timeout`, and at most what is left of the step. Each
  step builds cold, from empty caches: a Go backend the size of Rotor's takes
  about four minutes.
- Its output:

  ```json
  {"passed": 12, "failed": 1, "skipped": 0,
   "failures": [{"name": "example.com/sum > TestSubtracts/[verifies_CP-T2]_by_one", "message": "sum_test.go:14: 2-1 = 1, want 2", "file": "example.com/sum"}],
   "cases": [{"name": "example.com/sum > TestSubtracts/[verifies_CP-T2]_by_one", "result": "failed", "verifies": ["CP-T2"]}],
   "logTail": "..."}
  ```

  - A case's `name` is its class or file, then its own name, as `>` joins a
    vitest describe block; `result` is `passed`, `failed` or `skipped`, and an
    error counts as failed.
  - `verifies` are the test case keys a `[verifies KEY]` tag names in the
    test's name or class, such as `[verifies CP-T1, CP-T2]`; Go's subtests
    write the spaces as underscores, which count too. In pytest a parameter's
    id (`pytest.param(..., id="verifies CP-T1")`) or
    `record_property("verifies", "CP-T1")` tags a test.
  - The counts hold every case. `cases` names at most 2000, those that verify
    a case or fail first, and `failures` at most 50, each message at most
    2000 characters, its start and end kept. `file` is the report's file for
    the case, or its class or package, inside the work tree. `logTail` is the
    last 8 KiB of the command's output.
  - What the tests wrote comes from the repository's code: a later agent reads
    `{{ nodes.test.failures }}` fenced as data, like any step's output.
- Failing tests are a result: the step succeeds and a router or an agent
  decides what follows. The step fails, and the run with it, when the command
  cannot run (`test_command`), runs out of time (`test_timeout`), writes no
  report or one that is not JUnit (`test_report`), or exits with an error
  while its reports name no failing test (`test_command`), such as a build
  that broke before any test ran. The reason quotes the end of the output.
- The run's page tells how many tests passed, failed and were skipped, and
  names the first ten that failed.

### Limits

`maxSteps` bounds how many steps a run takes, each node of every branch one,
`timeout` how long it runs (`30s`, `15m`, `2h`), and `maxTokens` how much the
model may read and write, counted across every sandbox the run used. A run
that passes one ends and says which. Three more keep a workflow from costing
too much or flooding a tool:
- `maxCost`: the most the run's model calls may cost, in US dollars, each at the
  prices an administrator set for its own model. A run whose models are not
  all priced cannot carry a `maxCost`, and is refused when it starts.
- `maxConcurrent`: how many runs of the workflow may be starting or running at
  once. More wait, queued, and start as others end; a run waiting for an
  approval holds no place.
- `toolCallsPerMinute`: how many tool calls the run may make in any minute, 60
  unless it says; one more ends the run. Every branch's calls count.

An administrator may also set, for everyone, how many tokens and how much
money one person's runs may use in a day, and how many of their runs may run
at once.

### Retention

`retention`, beside `limits`, says how long a finished run's inputs, output,
events and checkpoints are kept before Rotor deletes them, in whole days such
as `30d`. The installation keeps them 90 days unless an administrator says
otherwise; a workflow may keep its runs for fewer days than the installation,
never for more, and at most 3650. The audit log keeps its record of every run.

## What is checked

When a workflow is saved, and again when a run starts:
- **The YAML and the schema:** every field known, of the right kind.
- **Its sense:**
  - node ids unique
  - edges, cases, `onExhausted` and templates, in arguments, prompts, messages
    and form values, name nodes that exist
  - every node reachable from the entry
  - a retention of at most 3650 days
  - tool nodes call through bindings that allow the call
  - an approval's form names only its own fields, and no binding allows a
    call that decides a review, which a person does in the tool itself
  - every `unattended` call is one of its binding's `allowTools`
  - an agent takes at most the workflow's `maxSteps`, and its role holds no
    template
  - a repository's `project` and `name` read nothing but `inputs`
  - a workspace names a repository binding, and writes only through one that
    may push (`workspace_binding`)
  - a test step only reads its workspace, names at least one report inside
    the work tree, and asks for no longer than its run (`test_node`)
- **The models an administrator allows:** an agent's own `model` is one of
  them (`model_not_allowed`).
- **The tools as you see them,** when saving, and when a run starts, read
  afresh from the tools as the run's person, so a call a tool turned into a
  write since is caught:
  - the tool exists and offers the scopes
  - a repository binding's tool offers git, and the repository is one you
    see; one named by `inputs` is checked once the run's inputs are known
  - a read binding allows only tools that only read
  - a binding with `approval: beforeWrite` cannot be written through on any way that skips an approval
  - a workspace's image is one an administrator allows (`image_not_allowed`)

Each problem names where it is, by line and column, and what to do about it.

When saving, a workflow can also get **warnings**, which never stop a save:
- `untrusted_then_write`: a node may write after another read from a tool,
  with no approval node in between, or an agent or code node both reads and
  may write in one step. Text in what was read could steer the write; read in
  one node, put an approval node after it, and write in the next.
- `unbounded_loop`: a loop in which no node sets `maxVisits`, so only
  `limits.maxSteps` ends it. A way past a node with no visit left counts as
  going to its `onExhausted`, so a loop that only leads there and back again
  is still one.
- `join_missing`: several edges lead into a node that sets no `join`, so it
  runs once for each branch that reaches it. An edge back round a loop is not
  a branch. Set `join: all` to run it once, after all of them.

## Drawing a workflow (#46)

The editor has two tabs over the same YAML: **YAML** and **Canvas**. The canvas
draws the workflow as the run page does, and every change made on it is written
into the YAML text, so switching tabs shows the YAML you could have written
yourself. The browser remembers which tab you used last.

- **What the canvas edits.**
  - Adding a node of any kind with an id. A new node starts with its required
    fields empty, so the checks say what it still needs.
  - Removing a node, with its edges and the router cases and default that lead
    to it. The entry is left as it was; the checks say when it names no node.
  - Renaming a node, which also renames it in edges, the entry, router cases,
    the default, `onExhausted`, a router's `on` and `{{ nodes.<id>... }}` and
    `{{ visits.<id> }}` templates. Python in a code node is your own and keeps
    the old name. Removing a node also takes out an `onExhausted` naming it.
  - The edges out of a node, and a router's cases and its default (a router
    takes no edges).
  - Which node the workflow starts at.
  - Each kind's main field: an agent's `prompt`, a tool node's binding and
    `call`, a code node's `source`, an approval's `message` and a router's `on`.
    A test node's `command` is shown as one line; it is a list, changed in the
    YAML with the node's workspace and reports.
  - An agent's `role`, which an emptied field takes out again, and its
    `model`, chosen among the models administrators allow or left to the
    workflow's.
- **What stays in the YAML.** Output schemas, a tool call's `arguments`, an
  approval's form and approvers, a node's `join`, limits, bindings and the
  rest. **Edit in
  YAML** on a node opens the YAML tab at it.
- **Your text stays yours.** An edit changes only the lines it is about:
  comments, quoting, flow or block style and the order of everything else stay
  as you wrote them. A new item follows the style of the items beside it.
- **No layout is stored.** The canvas lays the graph out in layers from the
  entry every time, so the YAML holds nothing but the workflow. An edge back
  to a node on the way to it closes a loop and is drawn dashed, with an arrow,
  around the nodes; so is it on the run page. A node's `onExhausted` is drawn
  as an edge labelled `onExhausted`. Parallel branches stand side by side,
  and a join sits below the longest of them.
- **Problems on the nodes.** A node the checks found something wrong with is
  marked with how many problems it has; choosing it lists them, and each opens
  the YAML at its line. A YAML that does not parse is not drawn: the canvas
  says so and offers the YAML tab.
- **Without a pointer.** The tabs take the arrow keys, and every change has a
  form beside the graph: choose a node from the list, then edit it.

## Content a run reads (#40)

Anyone who can write an issue, a requirement, a wiki page or a tool event can
put text in front of a run's model. Rotor treats all of it as data:
- **A fixed policy.** Every agent node starts with the same system prompt: only
  the request outside the fences says what to do, and links may point only at
  the run's tools.
- **Fences.** Every value a prompt quotes with `{{ ... }}`, and every answer a
  tool gives an agent, is wrapped in
  `<untrusted-data id="..." source="...">`, closed by
  `</untrusted-data id="...">`. The id is random for each sandbox, and a marker
  inside the content is defanged, so content cannot close its fence. Only the
  words you write in `prompt` instruct.
- **Links.** A link in an agent's answer, its streamed text or the arguments it
  sends a tool is replaced by `[link removed]` unless its host is one of the
  run's tools' hosts, and the run's timeline says so. The run's page makes
  links only to those hosts too. Code nodes are your own code, so their links
  are left as written.

Fences make a model less likely to follow planted text; they do not make it
impossible. What a run can do stays bounded by its bindings, its approval nodes
and its sandbox's network.

## Examples

[`examples/`](../examples) holds workflows to start from; the Go and Python checks validate each, and
[`draft-requirements.yaml`](../examples/draft-requirements.yaml) is the one the suite runs against the real
Exciter (#25): an agent drafts a title and a statement, and a tool node files them with `create_requirement` as the
person who runs it.

### Clarify a story, then write its requirements

[`story-to-requirements.yaml`](../examples/story-to-requirements.yaml) (#130)
turns an Armature user story into Exciter requirements, holding the
conversation on the ticket. A trigger on Armature's `issue.created` and
`comment.added` starts it as the trigger's owner:
1. **Gather**, a code node, reads the issue, its comments and the
   requirements Rotor filed for it before. It ends the run unless the issue
   is a Story, and, on a comment, unless the comment answers the questions
   Rotor asked last.
2. **Analyse**, an agent with no tools, reads what gather found and answers
   either questions or the requirements to create or update. Nothing the
   story says can make it write.
3. With questions, a code node posts them as one comment headed
   **Questions from Rotor**, and the run ends. The answer starts the next run.
4. Otherwise a code node writes the requirements in the trigger's Exciter
   project, links each to the story as `implements`, asks for their review,
   and comments **Requirements from Rotor** on the ticket with their keys.
   The person reviewing them in Exciter is the gate.

Its writes are `unattended`, so the trigger's owner agrees to them, and the
run's own comments do not start it again. To use it, add the workflow, then a
trigger on Armature's events with the topics `issue.created` and
`comment.added`, your projects, the condition `payload.internal` does not
equal `true`, and `exciterProject` set to the Exciter project; agree to the
calls the form lists. [`triggers.md`](triggers.md#the-story-workflow-against-armature)
walks through it, Armature's webhook included. The suite runs it against the
real Armature and Exciter (`TestSuiteAStoryInArmatureBecomesRequirementsInExciter`).

### Implement a feature through reviewed, tested code

[`implement-feature.yaml`](../examples/implement-feature.yaml) (#140) takes a
feature specified in Exciter to a pull request in Armature that every reviewer
approved, with a trace from the requirements to passing tests. A person
starts it with the Exciter project, the feature (a section or a requirement),
the section test cases go under, the Armature project, issue and repository,
and optionally the test plan whose execution records the results:
1. **Gather**, a code node, reads the feature's requirements from the tree,
   the test cases linked to them by `verifies`, and which requirements changed:
   one whose `verifies` link is suspect, because it moved past the revision
   its test cases were reviewed against, or that no test case verifies yet.
2. **Test design**, at most three rounds: a test designer drafts the test
   specification and the cases to add or update, each naming the requirements
   it verifies; a test reviewer approves or names what to change, and the
   router sends changes back. After the third draft a person decides whether
   to file it as it is.
3. **File tests**, a code node, writes the cases as Test case requirements
   under the test section, of the project's test case type, and links each to
   the requirements it verifies.
4. **Branch**, a code node, takes the issue's branch in the repository, or
   makes it with `create_issue_branch`.
5. **Implementation**, five rounds before a person is asked:
   - the coder works on the branch in a [workspace](#workspaces-136) that
     writes, tagging each test `[verifies KEY]` with the test case it
     automates and `[implements KEY]` with the requirement it implements;
   - a [test step](#a-test-step-138) runs the tests;
   - four reviewers run side by side, each in a workspace that only reads:
     code review, security review, test run analysis (which reads
     `nodes.test`), and conformance to the requirements;
   - **verdict**, a code node with `join: all`, gathers the four verdicts and
     their findings, and adds a failing test or a test case no passing test
     verifies. Any finding sends the coder another round, with every finding
     in its prompt. Past the fifth round, each further one is a person's to
     allow; rejecting stops the run, and the branch keeps what was pushed.
6. **Finish**, a code node, opens the pull request with the reviewers'
   summary and each test case's result, starts a test execution of the plan
   holding the cases, records each case from the tests that verify it, closes
   it, and comments on the issue with the pull request. Nothing merges: the
   pull request is the gate.

Each agent has its own `role` and `model` and only the bindings it needs. No
agent holds a binding that writes, the reviewers never push, and every write
in Exciter and Armature is a code node's. The example runs node's own test
runner in the `node-26` image; set the workspaces' `image`, the test step's
`command` and `reports` to your project's toolchain, such as `golang-1.26`
with `gotestsum`, and the models to ones your administrators allow. The
toolchain is the workflow's, not an input: an image is checked when the
workflow is saved, and a command is never a template.

The integration test runs it as shipped against the tool double's Exciter and
Armature, on a fixture repository (`TestAFeatureIsImplementedThroughReviewedTestedCode`);
the suite runs its test design against the real Exciter and Armature
(`TestSuiteAFeaturesTestCasesAreDesignedAndFiledInExciter`). The suite's
Armature has no Gitea yet, so its repository half runs on the double alone.

The examples below are the golden fixtures, which show more of the spec.

### Draft requirements, file them after approval

[`draft-requirements.yaml`](../api/workflow-fixtures/valid/draft-requirements.yaml):
1. An agent reads the project through a read binding and drafts a requirement.
2. A member of the quality group checks the draft, may change its title and
   statement, and approves.
3. A tool node files the approved title and statement through a write binding
   that asks for approval first.

The agent cannot write: its binding offers only reading tools.

### Triage, and ask before requesting a review

[`triage-with-approval.yaml`](../api/workflow-fixtures/valid/triage-with-approval.yaml):
1. An agent judges a requirement and answers `review` or `fine`.
2. A router follows that answer:
   - **`review`**: an approval comes first, then a tool node requests the review.
   - **`fine`**: a code node writes a summary.

### Count requirements by status, without a model

[`transform-with-code.yaml`](../api/workflow-fixtures/valid/transform-with-code.yaml):
1. A tool node lists a project's requirements.
2. A code node counts them by status.

There is no model and no agent: workflows need neither.
