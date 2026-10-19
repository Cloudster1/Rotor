# API contract for M8: a run's data at rest (#41)

What a run read from the tools and wrote back is ciphertext wherever Rotor keeps it, and no credential of a known
shape is kept or said at all:
- every run has a data key of its own, which OpenBao transit makes and wraps; the control plane seals the run's data
  with it and keeps it wrapped beside the run
- the runner and the control plane cut credentials from every event, run output, span, audit record and log line

Nothing here changes an operation: the api answers as before, in plaintext, to whoever may see the run.

## Migrations, packages and settings

- Migrations:

  | Migration | Change |
  |---|---|
  | 00800 | runs kept so far are deleted; `run.data_key` and the sealed columns below replace the plaintext ones |

- Packages: Go `internal/envelope` (the format and the keys in memory) and `internal/redact` (the patterns);
  Python `redact.py`.
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
