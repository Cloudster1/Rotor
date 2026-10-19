# Triggers

A trigger starts a workflow without anyone pressing Run. Each person makes
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

## When Rotor turns a trigger off

Rotor checks every firing as the trigger's owner and turns the trigger off
when it no longer can run as them. That happens when the owner is switched
off, a tool the workflow uses is no longer connected, the workflow is
archived or no longer shared with them, a new version writes without an
approval, or its inputs no longer fit. The trigger says why, the Workflows
page flags the workflow, and the owner turns it on again once that is
mended. A tool that does not answer does not turn anything off; the firing
is tried again for a while.
