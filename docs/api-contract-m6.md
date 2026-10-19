# API contract for M6: triggers

M6 starts workflows without a person pressing Run:
- a schedule trigger runs a workflow on a cron schedule in a time zone
- an event trigger runs it when Armature, Exciter or Stator delivers an event
  that matches its filters
- either way the run acts as the person who owns the trigger, with their
  connections, and stops being able to start the moment their consent or
  account ends

This file is #33. The route tables in `backend/internal/httpapi/operations_m6.go`
and `operations_m6_hooks.go` are the source of truth for the HTTP operations;
this file says what they mean. Operations carry `pending: true` and answer 501
until #34 (schedules) and #35 (tool events) build them.

Conventions are M2's: paths under `/api/v1`; a workflow or trigger the caller
may not see is 404; refusals are sentences.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | Tables |
  |---|---|---|
  | 00600-00619 | #34 | `trigger` (both kinds' columns), `trigger_fire`, `run.trigger_id` |
  | 00620-00639 | #35 | `hook_event`, the tool's hook columns |

- Packages: `internal/triggers` (records, schedules, the scheduler, the fire pass and the sweep, #34) and `internal/hooks` (verifying, reading and matching deliveries, #35).
- Settings: none new; the worker's tick paces the scheduler.
- Limits, in `internal/config`:
  - `MinScheduleInterval` = 5 minutes between two firings of one schedule.
  - `TriggerBatch` = 20 triggers or firings one pass takes.
  - `TriggerFireRetryFor` = 15 minutes: how long a firing that failed for a passing reason, such as a tool that does not answer, is tried again.
  - `TriggerFireRetention` = 30 days of firings kept.
  - `TriggerSweepEvery` = 1 minute between sweeps for lost consent.
  - `MaxTriggerConditions` = 10, `MaxConditionPathDepth` = 10, `MaxTriggerTopics` = 20, `MaxTriggerProjects` = 50.
  - `MaxHookBody` = 256 KiB; `HookReplayWindow` = 5 minutes; `HookEventRetention` = 7 days.
  - `SchedulerLockKey`: the advisory lock one scheduler holds per pass.
- **Decided:** triggers are records each person makes, a triggered workflow writes only after an approval until #32, and a run starts only if its owner can read the event's subject. `docs/decisions.md`, 2026-10-19, has the reasons.

## Triggers

| Operation | Needs | Answers |
|---|---|---|
| `GET /triggers` | signed in | `{triggers}`: the caller's own |
| `GET /workflows/{workflowID}/triggers` | may see the workflow | `{triggers}`: the caller's on it; an administrator's list has everyone's |
| `POST /workflows/{workflowID}/triggers` | may see the workflow | 201 the trigger, enabled; 409 when a connection is missing; 422 with a field per problem |
| `GET /triggers/{triggerID}` | owner or admin | the trigger |
| `PUT /triggers/{triggerID}` | owner | the trigger; its kind stays |
| `DELETE /triggers/{triggerID}` | owner or admin | 204; runs it started stay |
| `POST /triggers/{triggerID}/enable` | owner | the trigger, checked again as when it was made |
| `POST /triggers/{triggerID}/disable` | owner or admin | the trigger |
| `POST /triggers/schedule` | signed in | `{nextRuns}`, the next five firings in UTC, or 422 |

- **A trigger** belongs to one workflow and one person, its owner:
  - `name`, `kind` (`schedule` or `event`), `enabled`.
  - `disabled {reason, message, at}`, set when Rotor turned it off; a trigger the owner turned off has none.
  - `schedule {cron, timeZone, noOverlap}` or `event {toolId, topics, projects, conditions}`.
  - `inputs`: fixed values the runs start with, checked against the workflow's inputs schema.
  - `nextRunAt` for a schedule, and `lastFire {outcome, reason, at, runId, runStatus}`.
- **Who a run acts as:** the owner, with the owner's connections, as if they had pressed Run on the workflow's current version. Each firing is checked as the owner first:
  1. The owner is active.
  2. The workflow is one they may see, and not archived.
  3. Every binding has their connection.
  4. No node can write without an approval before it.
  5. The inputs fit.

  A run started by a trigger carries `trigger` (`{id, name}`) in `GET /runs/{runID}`, and `run.started` in the audit log names it.
- **Writes wait for a person** until #32 holds them at the tool. A trigger is refused (422 `unattended_write`, naming the node) on a workflow where some node may write without an approval node on every path before it:
  - a tool node whose call the tool does not mark read-only
  - an agent or code node with a binding of `access: write`

  The binding's own `approval` field does not matter here. A version saved later that breaks this turns the trigger off at its next firing.
- **Schedules:**
  - `cron` has five fields (minute, hour, day of month, month, day of week) as `crontab(5)` reads them, with `*`, lists, ranges and steps. A `TZ=` or `CRON_TZ=` prefix is refused; the time zone is its own field, an IANA name such as `Europe/Berlin`.
  - Two firings are at least `MinScheduleInterval` apart.
  - Across a change to or from summer time, a time that does not exist that day is skipped, and a time that happens twice fires once.
  - One scheduler at a time takes the due triggers, under `pg_try_advisory_xact_lock`, and moves each `nextRunAt` on. A time missed while no worker ran fires once when one is back; earlier missed times are not made up.
  - With `noOverlap`, a firing whose trigger's last run has not ended, a run waiting for an approval included, is `skipped`.
- **Event filters:**
  - `topics` (required) are the tools' event names, such as `issue.created` (Armature), `review.decided` (Exciter; a requirement approved is `review.decided` with `decision: approve`) or `page.published` (Stator).
  - `projects`, when given, must hold the event's project key, read from `payload.issue.key` before its dash (Armature), `payload.requirement.project.key` (Exciter) or `payload.page.space.key` (Stator). An event without one matches no project filter.
  - `conditions` must all hold. Each has a dotted `path` into the event, such as `payload.review.decision`; an `op`; and a JSON `value`:
    - `equals` holds when the value at the path is the given value.
    - `in` holds when it is one of a given list.
    - `exists` holds when the path leads anywhere.

    Values are scalars; there are no patterns.
  - The run's inputs are the trigger's `inputs` and `event {tool, topic, id, occurredAt, payload}`, so the workflow's inputs schema must declare `event`.
- **Firings:** each due schedule time and each matching event becomes one row of `trigger_fire`, unique per trigger by its key (`schedule:<time>` or `event:<event id>`). The worker's fire pass takes them and starts the run with an `Idempotency-Key` of `trigger:<id>:<key>`, so a firing never starts two runs. A firing ends:
  - `started`: a run started.
  - `skipped`: no-overlap, or an event subject the owner cannot read.
  - `refused`: a check failed and the trigger was turned off.
  - `failed`: a passing failure outlasted `TriggerFireRetryFor`.
- **Turned off by Rotor:** a sweep every `TriggerSweepEvery`, and each firing, turn a trigger off with a reason the owner sees on the trigger and on the Workflows page:

  | Reason | When |
  |---|---|
  | `owner_switched_off` | the owner's account is switched off |
  | `connection_lost` | a binding of the current version has no live connection of the owner's |
  | `tool_removed` | the event trigger's tool was removed |
  | `workflow_archived` | the workflow was archived |
  | `workflow_hidden` | the owner no longer sees the workflow |
  | `unattended_write` | the current version may write without an approval |
  | `inputs_refused` | the current version's inputs schema refuses the trigger's inputs |

  A tool that does not answer turns nothing off; the firing is tried again.
- **Who sees triggers:** the owner, and administrators. The database holds this: `rotor_app` reads and writes only its own triggers, on workflows it may see, and a run may name only an enabled trigger of the person starting it.
- **Audit:** `trigger.created`, `trigger.changed`, `trigger.removed`, `trigger.enabled` and `trigger.disabled`, the last with no actor when Rotor turned it off, and with the reason.

- **As built (#34):**
  - **Cron** is read by `robfig/cron/v3` with the five-field parser. Rotor checks the shortest gap the minute and hour fields allow, across the hour too, and steps over a time summer time removes or repeats.
  - **The worker** runs the schedule, fire and sweep steps in each pass, before queued runs start, and counts firings by outcome in `rotor_trigger_fires_total{outcome}`. The sweep also deletes firings that ended more than `TriggerFireRetention` ago.
  - **Run responses:** a run carries `trigger {id, name}`, with the name shown to the trigger's owner alone.
  - **Refusal codes:** a trigger the checks refuse answers 422 `trigger_invalid`, or `unattended_write` when a write lacks an approval, with `fields` keyed `name`, `schedule/cron`, `schedule/timeZone`, `inputs/<pointer>` or `workflow/nodes/<i>`. A missing connection answers 409 `connections_needed`, as starting a run does. An administrator changing or turning on someone else's trigger gets 403 `not_trigger_owner`.

## Hooks

| Operation | Needs | Answers |
|---|---|---|
| `GET /tools/{toolID}/hook` | admin | `{url, product, secretSet, secretSetAt, lastReceivedAt}` |
| `PUT /tools/{toolID}/hook` | admin | `{secret}`; the hook as above, never the secret |
| `DELETE /tools/{toolID}/hook` | admin | the hook without a secret |
| `POST /hooks/{toolID}` | the tool's signature | see below |

- **Registering, by hand:**
  1. In the tool's webhook settings, an administrator adds a webhook with Rotor's `url` (`<app>/api/v1/hooks/<toolID>`) and the topics the triggers need.
  2. The tool shows its secret once (`armature_whs_`, `exciter_whs_` or `stator_whs_`); the administrator pastes it into Rotor right then.
  3. Rotor keeps it in OpenBao at `rotor/tools/{tool}/webhook`, never in the database, a log or an event.

  Turning the secret over in the tool means pasting the new one again. `docs/triggers.md` walks each tool through.
- **Delivering:** each tool signs its deliveries alike, with its product in the header names (`X-Armature-`, `X-Exciter-`, `X-Stator-`):

  | Header | Holds |
  |---|---|
  | `X-<P>-Timestamp` | the time it was sent, in Unix seconds |
  | `X-<P>-Signature-Timestamped-256` | `sha256=` and the hex HMAC-SHA256 of `<timestamp>.<body>` with the secret |
  | `X-<P>-Event` | the topic |
  | `X-<P>-Delivery` | this attempt's id |
  | `X-<P>-Webhook-Version` | `1`, or `2` for CloudEvents |

  Rotor checks the timestamped signature and that the timestamp is within `HookReplayWindow` of its clock; the untimestamped `X-<P>-Signature-256` alone is not enough.
- **Envelopes:**
  - Version 1, from all three: `{version: 1, id, topic, occurredAt, payload}`, with `source` from Exciter and `orgId` from Armature and Stator.
  - Version 2 from Exciter: CloudEvents 1.0 in structured mode (`application/cloudevents+json`), `{specversion, id, source, type: dev.exciter.<topic>, time, data}`.
  - Both read into one event `{tool, topic, id, occurredAt, payload}`.
- **Deduplication:** the envelope's `id` is the event's, the same across a tool's retries. `hook_event` keeps each tool's ids for `HookEventRetention`, and an id seen before answers 200 and starts nothing.
- **Answers:**

  | Status | When |
  |---|---|
  | 202 | the event is kept and its firings queued; the worker starts the runs within a tick, so a delivery never waits for a sandbox |
  | 200 | a duplicate, or the topic `ping` |
  | 401 | a missing or wrong signature, or one older than the replay window |
  | 404 | an unknown tool, a tool that is not Armature, Exciter or Stator, or one without a secret |
  | 413 | a body over `MaxHookBody` |
  | 422 | a version or envelope Rotor does not read, so the tool keeps it failing visibly |

- **A payload is the registering administrator's view:** the tools read a webhook's payload with the rights of whoever added the webhook. So an event trigger needs its owner connected to the tool, and before its run starts Rotor reads the event's subject at the tool as the owner: the requirement (Exciter), the issue (Armature) or the page (Stator). A subject the owner cannot read makes the firing `skipped`, and nothing of it reaches the run.
- **Audit:** `hook.secret_changed` (set or removed), never the secret.

- **As built (#35):**
  - **Refusal codes:**
    - `signature_invalid` (401).
    - `envelope_unknown` (422), which also covers a body that is no envelope at all.
    - `hook_not_product` (422) for a tool that is not one of the three; `GET /tools/{toolID}/hook` answers it too.
    - `too_large` (413).
    - A secret of the wrong shape answers 422 with the field `secret`.
  - **Delivery answers:** `{status}` reading `accepted`, `duplicate` or `pong`.
  - **The secret:** each api keeps a tool's secret in memory for `HookSecretCacheTTL` (1 minute). A secret set or removed applies at once on the api that took it, and within a minute on the others; an id Rotor does not know is never kept.
  - **The queue:** a matching event queues a `trigger_fire` with `inputs.event`, the event's id as its key, and the time it arrived as its due time.
  - **Tables:**
    - `hook_event` has `rotor_admin` access alone.
    - `tool.hook_secret_set_at` and `tool.hook_last_received_at` say when the secret was set and when the last event came.
    - Removing a tool removes its webhook secret from OpenBao too.
  - **The suite:**
    - The suite's Exciter runs its worker, which delivers webhooks, and lets `rotor-api` through its outbound guard.
    - `TestSuiteExciterDeliversItsEventsToRotorsHook` registers Rotor's webhook there in versions 2 and 1 and sees a created requirement queue the trigger.

## What is left for later

- Rotor registering its own webhook at Armature through Armature's partner webhooks (Armature #281, `webhooks.partner@1`), and at Exciter and Stator once they offer it.
- CloudEvents from Armature and Stator once they send them (Armature #286, Stator #434).
- Telling an owner by email or webhook that a trigger was turned off; the `trigger.disabled` audit entry names the event.
- Writes confirmed at the tool, which lifts the approval rule for triggered runs (#32).
