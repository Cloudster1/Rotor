# API contract for M5: approvals

M5 lets a person decide before a run goes on:
- an `approval` node stops the run and shows what it is about to write
- the person who started the run, or the group the node names, approves,
  rejects or changes the fields the node offers, with a comment
- the run goes on in a fresh sandbox, ends when rejected, and ends when nobody
  decides in time
- an inbox with a badge lists what waits for a person

This file is #30. The route table in `backend/internal/httpapi/operations_m5.go`
is the source of truth for the HTTP operations, `api/workflow.v1.schema.json`
for the spec and `proto/rotor/runner/v1/runner.proto` for the runner protocol;
this file says what they mean. Operations carry `pending: true` and answer 501
until #31 builds them.

Conventions are M2's: paths under `/api/v1`; an approval or run the caller may
not see is 404; refusals are sentences.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | Tables |
  |---|---|---|
  | 00500-00519 | #31 | `approval`, who sees and decides it, and the run policy for group approvers |
  | 00520-00539 | #32 | what a binding's token may do until a write is confirmed |

- Packages:
  - Go: `internal/approvals` (list, read, decide), with `internal/runnerctl` opening and closing approvals as runs wait and end.
  - Python, in `runner/src/rotor_runner`: `preview.py`, the writes an approval shows.
- Settings: `ROTOR_APPROVAL_TTL`, how long an approval waits when its node sets no `expiresIn`; 72 hours, at most `WaitingTTL`.
- Limits, in `internal/config`:
  - `MaxApprovalExpiry` = 168 hours, the same as `WaitingTTL`.
  - `MaxApprovalPreviewBytes` = 32 KiB: a larger preview is cut, and says so.
  - `MaxApprovalCommentChars` = 2000.
  - `ApprovalListLimit` = 200.
- **Decided:** approvers are set per node, and a group decides alone; the preview comes from the runner; a run resumes by interrupt id. `docs/decisions.md`, 2026-10-19, has the reasons.

## The spec

An `approval` node gains three optional fields:

```yaml
- id: review
  type: approval
  message: File "{{ nodes.draft.title }}" in Exciter?
  approvers: { group: quality }     # without it, the person who started the run decides
  expiresIn: 48h                    # without it, ROTOR_APPROVAL_TTL; at most 168h
  form:
    schema:                         # a JSON Schema object; its properties are what an approver may change
      type: object
      required: [title]
      properties:
        title: { type: string, maxLength: 200 }
        statement: { type: string }
    values:                         # what the run proposes, templates into the state
      title: "{{ nodes.draft.title }}"
      statement: "{{ nodes.draft.statement }}"
```

- **After the decision** the node's output is the form's values as approved, with `approved`, `comment` and `decidedBy`. Later nodes read `{{ nodes.review.title }}`.
- **Checks, in Go and in the runner:**
  - `form.schema` is an object schema with `properties`, and `form.values` names only those properties.
  - `approved`, `comment` and `decidedBy` are not field names.
  - Templates in `form.values` name nodes that exist.
  - `expiresIn` is at most `MaxApprovalExpiry`.
- **No tool decides a review** (Exciter's decision #182): a binding whose `allowTools` names a call that approves, rejects or signs a review is refused with `decides_review`, and the runner refuses such a call too. Rotor's own approvals decide whether a run goes on, never a review in a tool.

## #31 Approvals

| Operation | Needs | Answers |
|---|---|---|
| `GET /approvals` | signed in | `{approvals}`, newest first, at most `ApprovalListLimit`; `status=pending` (the default), `decided` or `all`; `waitingOnMe=true` for those the caller may decide |
| `GET /approvals/count` | signed in | `{waitingOnMe}`, pending approvals the caller may decide |
| `GET /approvals/{approvalID}` | may see | the approval with its `preview`, `form` and `decision` |
| `POST /approvals/{approvalID}/decision` | may decide | `{decision: approve or reject, comment?, values?}`; the approval as decided |

- **An approval** belongs to one run and one interrupt:
  - `message`, rendered from the state.
  - `preview`: the writes the run may make before it reaches another approval or ends.
  - `form`, with its schema and proposed values, when the node has one.
  - `approvers.group`, or null when the starter decides.
  - `requestedAt` and `expiresAt`, plus once decided `decidedAt` and `decision {decision, by, comment, values, at}`.
  - `canDecide`: whether the caller may decide it now.
- **The preview** is what the runner can know before the run goes on:
  - A tool node writing through a binding is `{node, binding, tool, call, arguments}`, its arguments rendered from the state and the form's proposed values.
  - An agent or code node with a write binding is `{node, binding, tool, calls}`, the calls the binding allows, since those choose their own arguments.
  - Every branch of a router is shown. A preview above `MaxApprovalPreviewBytes` is cut to what fits, and its last entry says so.
- **States:** an approval is `pending` until one of these moves, and never moves again:
  - `approved` or `rejected` by a decision
  - `expired` when `expiresAt` passes
  - `cancelled` when its run ends any other way: cancelled, failed or past its timeout
- **Who sees and who decides:**

  | Who | Sees the approval and the run | Decides |
  |---|---|---|
  | the person who started the run | yes | when the node names no group |
  | a member of the node's group | yes, but the run is not in their `GET /runs` and they cannot cancel it | yes |
  | the workflow's owner, an administrator | yes | no, unless one of the above |
  | anyone else | no, 404 | no |

  - Group membership is the person's groups at their last sign-in.
  - The run goes on as the person who started it: an approver lends their judgement, never their access.
  - The database's policies hold this for reads. Decisions go through the service, which checks the approver first.
- **Deciding:**
  - Approving merges `values` over the proposed values; a field the schema does not have, or a value it refuses, answers 422 with `fields` keyed `values/<pointer>`.
  - Approving moves the run from `waiting` to `starting`. A fresh sandbox resumes it from its checkpoint and answers exactly the interrupt the approval was for (`StartRun.resume_interrupt_id`). A sandbox lost later never answers a later approval with this one's decision.
  - Rejecting ends the run `cancelled` with reason `rejected`, and no sandbox starts.
  - `values` are ignored when rejecting. `comment` is at most `MaxApprovalCommentChars`.
- **Expiry:** a worker pass ends a run whose pending approval passed `expiresAt`: `expired`, reason `approval_expired`. `WaitingTTL` stays as a backstop.
- **Events** on the run's stream, beside M2's:

  | Type | Payload |
  |---|---|
  | `approval.requested` | `{approvalId, node, message, approvers, expiresAt}` |
  | `approval.decided` | `{approvalId, decision, by, comment}` |

  `run.finished` gains the reasons `rejected` and `approval_expired`. `GET /runs/{runID}` gains `approvalId`, the pending approval's id or null.
- **Refusals:**
  - 403 `not_approver`: the caller may see the approval but not decide it. The sentence names who decides.
  - 409 `approval_decided`: it is no longer pending; the sentence says how it ended, by whom and when.
  - 403 on `POST /runs/{runID}/cancel` for a group approver who is not the starter, the owner or an administrator: the sentence says to reject the approval instead.
- **Audit:** `approval.decided` with `{run, workflow, node, decision, startedBy, edited}`, where `edited` names the fields the approver changed and never their values.
- **Notifications:** the inbox, its badge and the two events above. Email and webhooks are left for later, keyed on the same event names, so adding them changes no workflow.

## #32 Write confirmation (agreed, built once tools offer token exchange)

- A binding with `approval: beforeWrite` holds a read-only token (`mcp:read`) until the approval before its write is decided.
- On approval, the control plane mints the write token through token exchange (Cloudster1/Exciter#235). It is narrowed to the calls the preview showed and lives no longer than the run.
- The sandbox is never trusted to hold back: a code node that calls a write tool before the decision is refused by the tool, since its token cannot write.
- `confirmWrites: always | never | after-untrusted-input` per binding, with an administrator's minimum, replaces `approval: beforeWrite` in #32; `beforeWrite` stays as `always`.
- Until #32 is built, `approval: beforeWrite` is enforced when a workflow is saved (no path to a write skips an approval) and by the runner, not by the tool.

## What is left for later

- Email and webhook notifications, keyed on `approval.requested` and `approval.decided`.
- Write confirmation enforced at the tool (#32, blocked on Cloudster1/Exciter#235).
- The audit log in the browser (M7).
