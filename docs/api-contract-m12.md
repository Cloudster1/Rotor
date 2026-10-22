# API contract for M12: triggers for a story workflow

M12 lets a triggered workflow hold a conversation on a ticket: it asks
questions on an Armature story, starts again when someone answers, and hands
the requirements it writes in Exciter to a person's review. Four changes to
triggers make that possible:
- a condition can say what an event must not be
- a workflow does not start itself on what its own runs wrote
- a trigger's owner can let a few named writes happen without an approval
- an administrator can forbid that for everyone

This file is #124. The route tables in `backend/internal/httpapi` are the
source of truth for the HTTP operations; this file says what they mean.
Operations carry `pending: true` and answer 501 until the issue that builds
them: #126 (conditions), #127 (the write ledger) and #128 (unattended
writes). Fields that existing operations gain arrive with the issue that
builds them, so no operation accepts a field it ignores.

Conventions are M2's: paths under `/api/v1`; a workflow or trigger the caller
may not see is 404; refusals are sentences.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | Tables |
  |---|---|---|
  | 01100-01119 | #127 | `run_write` |
  | 01120-01139 | #128 | `trigger.allow_unattended`, `unattended_setting` |

- Packages: `internal/hooks` (operators, an event's written subject, #126 and
  #127), `internal/triggers` (the ledger check at a firing, the agreed calls,
  #127 and #128), `internal/workflow/spec` and the runner's `spec.py` (the
  binding field, #128), `internal/runnerctl` (keeping the ledger and auditing
  unattended writes, #127 and #128).
- Limits, in `internal/config`:
  - `MaxWriteSubjects` = 20 subjects one tool call may record.
  - `MaxUnattendedCalls` = 20 calls one binding may name.
- **Decided:** named unattended writes are allowed before #32, with the owner
  agreeing to each call and an administrator able to forbid them.
  `docs/decisions.md`, 2026-10-22, has the reasons.

## Conditions that exclude (#126)

`event.conditions[].op` takes three more values beside `equals`, `in` and
`exists`:

| Op | Holds when | Value |
|---|---|---|
| `notEquals` | the path leads nowhere, or to something other than the value | a scalar |
| `notIn` | the path leads nowhere, or to none of the values | a non-empty list of scalars |
| `absent` | the path leads nowhere | none |

A missing path equals nothing, so `payload.internal notEquals true` holds for
an event without `internal`. The refusal for an unknown op names all six.

## The write ledger (#127)

A run's own writes do not start the workflow that made them.

- **The runner reports what it wrote.** The `tool.called` event, from a tool
  node, a code node's `ctx.call_tool` and an agent's tool call alike, gains:
  - `write`: true when the tool does not mark the call read-only
    (`readOnlyHint`).
  - `subjects`: for a write that succeeded, what it wrote, read from the
    whole answer before it is summarised. For each member of the answer's
    top-level object that is itself an object, such as `comment` or
    `requirement`:
    - `<member>:<id>` when it has an `id`;
    - `<member>:<key>` when it has a `key`;
    - `<member>:<key>@<revision>` when it has a `key` and a `revision`.

    At most `MaxWriteSubjects`, in that order. An answer of another shape
    records nothing.
- **The control plane keeps them** in `run_write` (run, tool, call, subject,
  when), one row per subject, in the transaction that stores the event. The
  rows belong to the run: `rotor_app` reads them through the run's own
  policy and never writes them, and they are deleted with the run.
- **Each event names what it is about:**

  | Tool | Topic | Subject |
  |---|---|---|
  | Armature | `comment.added` | `comment:<payload.commentId>` |
  | Armature | `issue.created`, `issue.updated`, `issue.transitioned` | `issue:<payload.key>` |
  | Exciter | `requirement.created`, `requirement.revised` | `requirement:<payload.requirement.key>@<payload.requirement.revision>` |
  | Exciter | `comment.created` | `comment:<payload.comment.id>` |

  Every other event names nothing and fires as before.
- **At a firing,** before the run starts, an event whose subject a run of the
  same workflow recorded for the same tool is `skipped`, with the reason
  "The event is this workflow's own write, by run `<id>`." Runs of other
  workflows still hear it, and so do people's own changes to the same
  issue, which name another subject or come later.
- **Why not the tools' marks.** Exciter's `actor.via` names a write's client
  by the name it registered, which any client may also choose, and Armature
  marks nothing. The ledger is the only signal, and it needs nothing from the
  tools.
- **Seen by the owner:** `lastFire.reason` is shown on the workflow page for
  a firing that was skipped.

## Unattended writes (#128)

### The binding

```yaml
tools:
  armature-write:
    tool: armature
    scopes: [comment.write]
    access: write
    allowTools: [add_comment]
    unattended: [add_comment]
```

- `unattended` lists calls of the binding's `allowTools` that may run, in a
  triggered run, with no approval node before them. At most
  `MaxUnattendedCalls`.
- Problems, when a workflow is saved and when a run starts:
  - `unattended_call_not_allowed` at `/tools/<name>/unattended/<i>`: the call
    is not in `allowTools`.
  - `decides_review` at the same path: the call decides a review, which a
    person does in the tool itself.
- A run started by a person ignores the field: nothing stops such a run from
  writing anyway.

### The trigger

- **What the workflow needs:** the workflow response gains `unattendedCalls`,
  the sorted `<binding>.<call>` pairs its current version names.
- **What the owner agrees to:** `POST /workflows/{workflowID}/triggers` and
  `PUT /triggers/{triggerID}` take `allowUnattended`, a list of
  `<binding>.<call>` pairs, and the trigger answers with it.
- **The check** (rule 4 of M6's firing checks) becomes: no node can write
  without an approval before it, except through a call that its binding
  names in `unattended` and the trigger's `allowUnattended` holds.
  - Making or changing a trigger whose workflow names a call the owner did
    not agree to is refused with 422 `unattended_not_allowed`, with the field
    `allowUnattended` saying which calls are missing.
  - A write that is neither unattended nor behind an approval is refused, as
    before, with `unattended_write`.
  - Agreeing to more than the workflow names is allowed; it changes nothing.
- **Turned off by Rotor,** at a firing and by the sweep, two more reasons:

  | Reason | When |
  |---|---|
  | `unattended_widened` | the current version names a call the owner did not agree to |
  | `unattended_forbidden` | an administrator forbade unattended writes, and the current version names some |

  The owner turns the trigger on again with `PUT` and the wider
  `allowUnattended`, or once unattended writes are allowed again.
- **Audit:** each unattended write of a triggered run writes
  `trigger.unattended_write`, with no actor, naming the trigger, the run, the
  call and its subjects.

### The setting

| Operation | Needs | Answers |
|---|---|---|
| `GET /settings/unattended-writes` | admin | `{allowed, updatedAt}` |
| `PUT /settings/unattended-writes` | admin | `{allowed}` in, the setting out |

- `allowed` is true on a new installation.
- Switching it off turns every trigger that relies on an unattended call off
  at once, with `unattended_forbidden`. Switching it on again turns none on.
- The audit log records `unattended.switched` with the new and the
  previous value.

### As built (#128)

- **Where the calls are named:** `unattendedCalls` is on the workflow's
  `version`, beside its `bindings`, since it is what that version names.
- **A review-deciding call** is refused where it already was, at
  `/tools/<name>/allowTools/<i>`; an unattended call must be in `allowTools`,
  so it never needs a second problem.
- **Forbidding** turns off every enabled trigger whose `allowUnattended` is
  not empty, in the request that forbids, as Rotor.
- **The audit entry** has `run`, `binding`, `call`, `tool` and `subjects`,
  the last empty until the write ledger (#127) reports them.

## What is left for later

- Holding a write back at the tool (#32) replaces the approval rule; named
  unattended calls stay as the owner's agreement.
- Tools that mark writes made through a connected app in their events would
  let Rotor drop the ledger for those tools.
