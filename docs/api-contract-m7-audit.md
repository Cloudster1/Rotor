# API contract for M7: the audit log (#36)

Administrators read and export who did what in Rotor, and what Rotor did on
its own. The route table in `backend/internal/httpapi/operations_m7_audit.go`
is the source of truth; this file says what it means. Conventions are M2's.

## What the log holds

Every entry has an action, the target it is about (`target_type`,
`target_id`), the person who acted (none when Rotor acted on its own), its
data with anything that looks like a secret replaced by `[redacted]`, and the
address and request it came from.

| Area | Actions |
|---|---|
| sign-in | `user.signed_in`, `user.signed_out`, `user.role_changed`, `user.switched_off`, `session.ended_by_provider` |
| tools and connections | `tool.added`, `tool.removed`, `hook.secret_changed`, `connection.granted`, `connection.ended` |
| tokens | `token.minted`, `token.revoked`: the tool, scopes and run, never the token |
| workflows and providers | `workflow.created`, `workflow.changed`, `workflow.shared`, `workflow.archived`, `provider.changed` |
| runs and sandboxes | `run.started`, `run.cancelled`, `run.ended` (`status`, `reason`), `sandbox.spawned` (`sandboxId`, `image`), `sandbox.destroyed` (`sandboxId`, `reason`) |
| approvals and triggers | `approval.requested`, `approval.decided`, `trigger.created`, `trigger.changed`, `trigger.removed`, `trigger.enabled`, `trigger.disabled` |
| the log itself | `audit.exported` (the filter, the format and how many entries) |

A sandbox's entries name what started, never its environment: the bootstrap
token travels in it.

**Append-only:** `rotor_app` and `rotor_admin` may insert and read entries,
never update, delete or truncate them. Only administrators read them, by the
table's own policy.

## Operations

| Operation | Needs | Answers |
|---|---|---|
| `GET /audit` | admin | `{entries, next}`: one window, newest first; `limit` up to 200, `cursor` from the window before |
| `GET /audit/facets` | admin | `{actions, actors, targetTypes}` the log holds |
| `GET /audit/export` | admin | the filtered log as `audit-log.csv` or `audit-log.json` (`format=csv` or `json`) |

- **Filters,** on all three: `action` (one the log records, else 422), `actor` (a person's id), `targetType`, `target`, `from` and `to`. A day `YYYY-MM-DD` is the whole day in UTC; a time with its zone is that instant; `to` must come after `from`.
- **Windows** are keyset pages over `(created_at, id)`, so entries arriving meanwhile neither repeat nor go missing. A cursor not written by the server is a 422.
- **Exports:**
  - CSV has the columns `time, action, actor_id, actor, target_type, target_id, ip, request_id, data`; a cell starting with `= + - @`, a tab or a carriage return is prefixed with `'`, so a spreadsheet never runs it.
  - JSON is an array of the entries as `GET /audit` answers them.
  - An export holds at most 50000 entries; a filter matching more is refused with 422 `export_too_large` rather than cut short.
  - The export is recorded as `audit.exported` before the file leaves; if that fails, nothing leaves.

## The page

`/settings/audit`, linked from an administrators' section of the settings:
filters for the action, the person, the kind of target and the dates; a
person or target in a row narrows the log to them; newer and older windows;
and the CSV and JSON exports of what the filters show.
