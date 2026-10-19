# API contract for M8: a run's data at rest (#41)

What a run read from the tools and wrote back is ciphertext wherever Rotor keeps it, and no credential of a known
shape is kept or said at all:
- every run has a data key of its own, which OpenBao transit makes and wraps; the control plane seals the run's data
  with it and keeps it wrapped beside the run
- the runner and the control plane cut credentials from every event, run output, span, audit record and log line
- a finished run's data is deleted once kept as long as the installation, or its workflow, says

Sealing changes no operation: the api answers as before, in plaintext, to whoever may see the run. Retention adds
the two operations of `backend/internal/httpapi/operations_m8_retention.go`.

## Migrations, packages and settings

- Migrations:

  | Migration | Change |
  |---|---|
  | 00800 | runs kept so far are deleted; `run.data_key` and the sealed columns below replace the plaintext ones |
  | 00801 | `retention_setting`: one row, how many days a finished run's data is kept, 90 at first |

- Packages: Go `internal/envelope` (the format and the keys in memory), `internal/redact` (the patterns) and
  `internal/retention` (the setting and the sweep); Python `redact.py`.
- Settings, in `internal/config/crypto.go`: the transit key `RunDataTransitKey` = `rotor-run-data`, 256-bit data
  keys, kept in memory `DataKeyCacheTTL` = 5 minutes after their last use, at most `DataKeyCacheEntries` = 4096.

## The data key

- When a run is started, the control plane asks `transit/datakey/plaintext/rotor-run-data` for a key, seals the
  run's inputs with it and stores the wrapped key in `run.data_key` (`vault:v<version>:...`). The key is opened
  again with `transit/decrypt/rotor-run-data`, several in one call for a page of runs.
- The control plane's OpenBao policy allows those two paths on that key and nothing else of transit: it cannot
  encrypt with the key, change it, or make keys with another.
- OpenBao rotates the key every 90 days (`auto_rotate_period=2160h`) and keeps older versions, so older runs open.

## The sealed format

Every sealed value is `0x01 | nonce (12 bytes) | AES-256-GCM ciphertext and tag`. The additional data is
`rotor/run-data`, a zero byte, the version byte and a zero byte, then the run's id (16 bytes), the column and the row,
each ended by a zero byte, so a value moved to another run, column or row does not open:

| Column | Row |
|---|---|
| `run.sealed_inputs` (`run.inputs`) | empty |
| `run.sealed_output` (`run.output`) | empty |
| `run_event.sealed_payload` (`run_event.payload`) | the event's `seq` |
| `run_control.sealed_message` (`run_control.message`) | the message's `seq` |
| `run_checkpoint.sealed_checkpoint`, `.sealed_metadata` (`run_checkpoint.checkpoint`, `.metadata`) | `ns\|checkpoint_id` |
| `run_checkpoint_write.sealed_value` (`run_checkpoint_write.value`) | `ns\|checkpoint_id\|task_id\|idx` |

The name in parentheses is the one bound into the additional data. A trigger's firing keeps the event it carried
only until it ends; the run it started holds it sealed.

## Credentials

`api/secret-patterns.json` lists the patterns, in syntax Go's RE2 and Python's re read alike, the text that stands
in for a credential (`[redacted]`), the words that mark a field holding one (`token`, `secret`, `password`, ...)
and the words such a field may hold instead (`set`, `kept`, `removed`). `make secret-patterns` copies it into the
backend and the runner; a test on each side refuses a copy that differs.

- The runner cuts every secret it holds, then the patterns, from each event, its run's output and reason, every
  span and every log line, tracebacks included.
- The control plane applies the patterns and the field names again before it stores an event, a run's output and
  reason, an approval's message, preview and values, a span, or an audit record, and to every log line the api and
  the worker write.

## Retention

- **The installation's:** `GET /settings/retention` and `PUT /settings/retention` `{runDataDays}`, for
  administrators, answer `{runDataDays, maxDays, changedBy, updatedAt}`. Days run from 1 to `maxDays` (3650); others
  are refused with 422 and a sentence on `runDataDays`. A change is audited as `retention.changed`
  `{runDataDays, previous}`. Everyone signed in may read the row in the database; only an administrator changes it.
- **A workflow's:** the spec's top-level `retention`, whole days such as `30d`, at most 3650 (`retention_too_long`
  otherwise). A run is kept as long as the shorter of the two, read from the workflow's current version.
- **Deleting:** every `RetentionSweepEvery` (an hour) one worker, holding the advisory lock `RetentionLockKey`,
  deletes up to `RetentionBatch` (500) runs at a time that ended and were torn down longer ago than their retention,
  oldest first, at most `RetentionMaxBatches` (20) batches a sweep. Their events, checkpoints, control messages,
  approvals and usage go with them; triggers and firings forget them. Each batch writes one `run.data_deleted` entry
  per workflow, `{workflowName, runs, oldestFinishedAt, newestFinishedAt}`, with no actor. Daily usage totals and the
  audit log are never deleted.

## Untrusted tool content (#40)

Content a run reads is data, and links an agent writes lead only to the run's tools. No migration.

- **The runner protocol:** `ToolBinding.link_hosts = 7` names the hosts a person reaches the binding's tool at, from
  the tool's address and its MCP resource (`tools.Tool.LinkHosts`), never the address the sandbox dials.
- **The prompt:** every agent node gets the fixed system policy of `rotor_runner/policy.py`. Each value a prompt
  quotes and each tool answer is fenced as `<untrusted-data id="ID" source="SOURCE">` ... `</untrusted-data
  id="ID">`, with `ID` 16 random hex characters per sandbox (`FENCE_ID_BYTES` = 8) and any marker inside defanged.
- **Links:** a link whose host is not exactly one of the run's link hosts becomes `[link removed]` (`LINK_REMOVED`), in
  an agent's answer, its streamed `llm.output` text and the arguments of the tool calls it makes. Code nodes and tool
  nodes are not changed.
- **A new event, `link.removed`:**

  | Field | Value |
  |---|---|
  | `where` | `output` or `arguments` |
  | `count` | how many links were removed |
  | `hosts` | the hosts they named, sorted, at most `LINK_EVENT_HOSTS` = 5 |
  | `tool` | the call whose arguments held them, for `arguments` |

- **`POST /workflows/validate`** answers `warnings` beside `problems`, in the same shape. A warning never makes
  `valid` false and never stops a save. The one warning so far is `untrusted_then_write`.
- **`GET /runs/{runID}`** answers `linkHosts`: the hosts of the tools the run's pinned version binds, sorted. The page
  makes links only to them; a list of runs leaves it out.
