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
| Armature | `read` | `issue.write`, `issue.transition`, `comment.write`; `read` goes along with each |

At Armature a run's token carries only what its binding asks for: a read
binding without scopes asks for `read`, and the token cannot write even where
the person could.

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

### Limits

`maxSteps` bounds how many steps a run takes, `timeout` how long it runs
(`30s`, `15m`, `2h`), and `maxTokens` how much the model may read and write,
counted across every sandbox the run used. A run that passes one ends and says
which. Three more keep a workflow from costing too much or flooding a tool:
- `maxCost`: the most the run's model calls may cost, in US dollars, each at the
  prices an administrator set for its own model. A run whose models are not
  all priced cannot carry a `maxCost`, and is refused when it starts.
- `maxConcurrent`: how many runs of the workflow may be starting or running at
  once. More wait, queued, and start as others end; a run waiting for an
  approval holds no place.
- `toolCallsPerMinute`: how many tool calls the run may make in any minute, 60
  unless it says; one more ends the run.

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
- **The models an administrator allows:** an agent's own `model` is one of
  them (`model_not_allowed`).
- **The tools as you see them,** when saving, and when a run starts, read
  afresh from the tools as the run's person, so a call a tool turned into a
  write since is caught:
  - the tool exists and offers the scopes
  - a read binding allows only tools that only read
  - a binding with `approval: beforeWrite` cannot be written through on any way that skips an approval

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
  - An agent's `role`, which an emptied field takes out again, and its
    `model`, chosen among the models administrators allow or left to the
    workflow's.
- **What stays in the YAML.** Output schemas, a tool call's `arguments`, an
  approval's form and approvers, limits, bindings and the rest. **Edit in
  YAML** on a node opens the YAML tab at it.
- **Your text stays yours.** An edit changes only the lines it is about:
  comments, quoting, flow or block style and the order of everything else stay
  as you wrote them. A new item follows the style of the items beside it.
- **No layout is stored.** The canvas lays the graph out in layers from the
  entry every time, so the YAML holds nothing but the workflow. An edge back
  to a node on the way to it closes a loop and is drawn dashed, with an arrow,
  around the nodes; so is it on the run page. A node's `onExhausted` is drawn
  as an edge labelled `onExhausted`.
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
