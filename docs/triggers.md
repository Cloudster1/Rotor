# Triggers

A trigger starts a workflow without anyone pressing Run: on a schedule, or when
Armature, Exciter or Stator delivers an event. Each person makes
their own on a workflow they may see, and its runs act as them: with their
connections, and only while their account and consent last.
`docs/api-contract-m6.md` has the details.

## Schedules

On a workflow's page, under Triggers, add a schedule:
- **Cron fields:** minute, hour, day of month, month and day of week, as
  `crontab(5)` reads them. `0 8 * * 1` is Mondays at 8:00, and `*/15 9-17 * * 1-5`
  every quarter hour of a working day. Two times are at least five minutes
  apart.
- **Time zone:** the fields are read there. On the day summer time starts, a
  time that does not exist is skipped; on the day it ends, a time that
  happens twice fires once.
- **Skip a time while the last run has not ended:** a run waiting for an
  approval has not ended either.
- **Inputs:** what each run starts with, as the Run dialog asks for them.

"Show the next runs" lists the next five times. A time missed while Rotor was
down fires once when it is back; earlier missed times are not made up.

## What a triggered run may do

Nobody watches a triggered run, so until Rotor holds writes back at the tool
(#32), a trigger is allowed only on a workflow where every node that may
write comes after an approval node. Whoever the approval names decides in
the Approvals inbox.

There is one exception (#128). A binding may name calls `unattended`, such as
a comment on the ticket that started the run or a draft requirement that a
person still reviews in Exciter. When you add the trigger, the form lists
those calls under **Writes without an approval**, and you agree to them;
without that the trigger is refused. If a later version of the workflow
names more calls, Rotor turns the trigger off at its next firing until you
agree again. Every such write is in the audit log, with the trigger and the
run. A call that decides a review is never allowed.

An administrator can forbid writes without an approval for everyone, under
**Settings**, **Writes without an approval**. That turns off at once every
trigger whose owner agreed to such a call; allowing them again turns none
back on.

## When Rotor turns a trigger off

Rotor checks every firing as the trigger's owner and turns the trigger off
when it no longer can run as them. That happens when the owner is switched
off, a tool the workflow uses is no longer connected, the workflow is
archived or no longer shared with them, a new version writes without an
approval or names unattended calls you did not agree to, an administrator
forbade unattended writes, or its inputs no longer fit. The trigger says why, the Workflows
page flags the workflow, and the owner turns it on again once that is
mended. A tool that does not answer does not turn anything off; the firing
is tried again for a while.

## Tool events

An event trigger starts its workflow when a tool delivers an event it listens
for. Before any trigger can hear a tool, an administrator of both the tool and
Rotor connects them once.

### Registering Rotor's webhook in a tool

1. In Rotor, open **Settings**, find the tool under **Tools**, and copy the
   **Webhook address** shown under **Events**. It is
   `<Rotor's address>/api/v1/hooks/<tool id>`, and the tool must be able to
   reach it.
2. In the tool, add a webhook with that address:

   | Tool | Where | Notes |
   |---|---|---|
   | Exciter | Settings, Webhooks, or `POST /api/v1/webhooks` as an administrator | Choose version 2 (CloudEvents) or 1; you may limit it to projects. |
   | Armature | Settings, Webhooks, or `POST /api/v1/webhooks` (needs `org.administer`) | Version 1. |
   | Stator | Settings, Webhooks, or `POST /api/v1/webhooks` as an administrator | Version 1. |

   Subscribe to the topics your triggers need, or to all of them.
3. The tool shows the webhook's secret once: `exciter_whs_...`,
   `armature_whs_...` or `stator_whs_...`. Paste it into Rotor's **Webhook
   secret** field and save. Rotor keeps it in OpenBao, never shows it again
   and never writes it to its database or logs.
4. Send the tool's test delivery. Rotor answers a `ping` with 200, and the tool
   panel in Rotor shows when the last event arrived.

When you turn the secret over in the tool, the old one stops working at once:
paste the new one into Rotor straight away. **Forget secret** in Rotor refuses
the tool's deliveries until a secret is set again.

### What Rotor checks

Each delivery carries the tool's headers, with its product in their names:

| Header | Checked |
|---|---|
| `X-<Product>-Timestamp` | within five minutes of Rotor's clock, either way |
| `X-<Product>-Signature-Timestamped-256` | `sha256=` and the HMAC-SHA256 of `<timestamp>.<body>` with the secret |
| `X-<Product>-Webhook-Version` | `1`, or `2` from Exciter |
| `X-<Product>-Event` | not needed; the topic is read from the body |

`<Product>` is `Armature`, `Exciter` or `Stator`. A delivery whose signature
does not hold, or is older than five minutes, is refused with 401, so a
recorded delivery cannot be played back later. The body's `id` is the event's,
the same on every retry; Rotor keeps ids for seven days and answers a repeat
with 200 without starting anything twice.

| Answer | Meaning |
|---|---|
| 202 | kept; the matching triggers start within seconds |
| 200 | a repeat, or a `ping` |
| 401 | the signature is missing, wrong or too old: check the secret and the tool's clock |
| 404 | Rotor holds no secret for this tool, or does not know it |
| 413 | the body is larger than 256 KiB |
| 422 | an envelope version Rotor does not read: set the webhook to version 1 (or 2 for Exciter) |

### Topics and projects

An event trigger listens to one or more topics, and may be narrowed to
projects and conditions on the event.

| Tool | Topics, for example | Project |
|---|---|---|
| Exciter | `requirement.created`, `requirement.revised`, `review.requested`, `review.decided`, `comment.created` | `payload.requirement.project.key` |
| Armature | `issue.created`, `issue.updated`, `issue.transitioned`, `comment.added` | the part of `payload.key` (`payload.issueKey` on `sla.breached` and `vcs.*`) before the dash |
| Stator | `page.published`, `page.moved`, `page.deleted`, `comment.created` | `payload.page.space.key` |

A requirement approved in Exciter arrives as `review.decided` with
`payload.review.decision` set to `approve`; a condition picks it out. The tools'
own documents list every topic and payload.

### Adding an event trigger

On a workflow's page, under Triggers, add a trigger and choose **On a tool's
event**:
- **Tool:** Armature, Exciter or Stator. You need your own connection to it,
  and an administrator must have set up its webhook.
- **Topics:** tick the ones offered for the tool, or type others.
- **Projects:** project keys, or leave it empty for every project.
- **Conditions:** a dotted path into the event, such as
  `payload.review.decision`, compared with one of:
  - `equals` (`equals`) and `does not equal` (`notEquals`) one value;
  - `is one of` (`in`) and `is none of` (`notIn`) a list of values,
    separated by commas;
  - `exists` (`exists`) and `is missing` (`absent`), without a value.

  A path that leads nowhere equals nothing: `payload.internal` set to
  `does not equal` `true` leaves out Armature's internal comments and keeps
  an event without `internal`.

The workflow's inputs must declare `event`; the form asks only for the other
inputs, which each run starts with.

### What a run sees

The run starts with the trigger's fixed inputs and `event`:
`{tool, topic, id, occurredAt, payload}`, so the workflow's inputs schema must
declare `event`. A tool reads a webhook's payload with the rights of the
administrator who added the webhook, so before a run starts Rotor asks the tool,
as the trigger's owner, for the event's requirement, issue or page; if the owner
cannot read it, nothing starts.

### A workflow does not start itself

When a run writes through a tool, Rotor keeps what it wrote: the comment,
issue or requirement revision the tool answered with. An event about one of
those, within ten minutes of the write, does not start the same workflow
again; the trigger's last firing says it was skipped as the workflow's own
write, and names the run. Other workflows still hear the event, and so does
this one when a person changes the same thing later. Rotor does not rely on
the tools to say who wrote something, since a tool names a connected app by
the name the app chose (#127).

### The story workflow against Armature

[`story-to-requirements.yaml`](../examples/story-to-requirements.yaml)
(`docs/workflows.md`) holds a conversation on an Armature story and files its
requirements in Exciter (#130). Setting it up takes an administrator of each
tool once, and the trigger's owner after that:
1. **Connect Armature.** An administrator of Rotor adds Armature by its
   address under **Settings**, **Tools**; it reads **Awaiting approval** until
   an Armature administrator allows Rotor under **Settings**, **Connected
   apps**, or by connecting first.
2. **Connect Exciter to Armature.** In Exciter, an administrator connects the
   installation to Armature, and the trigger's owner stores their own Armature
   token there, since Exciter traces each requirement to the story with it.
3. **Armature's webhook.** An Armature administrator adds a webhook at Rotor's
   **Webhook address** for Armature, version 1, with the topics
   `issue.created` and `comment.added`, and pastes its secret into Rotor, as
   above.
4. **The trigger's owner** connects to both tools, adds the workflow, and adds
   a trigger **On a tool's event**: Armature, the topics `issue.created` and
   `comment.added`, the Armature projects whose stories it reads, the
   condition `payload.internal` `does not equal` `true`, and `exciterProject`
   set to the Exciter project the requirements go to. The form lists the five
   calls the workflow makes without an approval (Armature's `add_comment`,
   Exciter's `create_requirement`, `update_requirement`, `link_armature_issue`
   and `request_reviews`); agree to them.

The owner needs to read the stories in Armature, to comment there, and to
edit requirements in the Exciter project; the comments and requirements are
theirs, written through Rotor. Exciter asks for a review only in a project
with an approval rule, so give the project one; without it the requirements
are filed and linked, and nobody is asked. A story then gets Rotor's questions as a
comment; the answer of anyone who may comment starts the next run, and Rotor's
own comments start none.

### Later

Armature lets a connected app register its own webhook (Armature #281); Rotor
will use it, and the other tools' once they offer it, so no secret has to be
copied by hand.
