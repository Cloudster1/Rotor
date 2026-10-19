# API contract for M7: usage, cost and rate (#38)

Runaway runs stop before they cost too much or flood a tool:
- the runner reports what each model call used, and the control plane totals it per run, per person and per UTC day
- administrators price each model, so cost can be counted and bounded
- a workflow bounds its own tokens, cost, concurrent runs and tool calls per minute; administrators bound each
  person's day and concurrent runs
- going over a limit stops the run with the reason; a run over a concurrency limit waits, queued

The route table in `backend/internal/httpapi/operations_m7_usage.go` is the source of truth for the operations.
Conventions are M2's: paths under `/api/v1`, refusals are sentences.

## Migrations, packages and settings

- Migrations:

  | Migration | Tables |
  |---|---|
  | 00740 | `model_price`: each model's input and output price per million tokens, in US dollars |
  | 00741 | `run_usage` (per run, with the prices it began with) and `usage_daily` (per person and UTC day) |
  | 00742 | `usage_limit`: one row, the installation's limits on one person; empty is no limit |

  Only `rotor_admin` writes `run_usage` and `usage_daily`. Whoever sees a run sees its usage; a person sees their
  own days, administrators everyone's. Everyone signed in reads the prices and the limits; administrators set them.
- Packages: Go `internal/usage`; Python `usage.py` (the reporting callback) and `limits.py` (the tool-call rate).
- Limits, in `internal/config/usage.go` and the runner's `config.py`: prices are per `TokensPerPriceUnit` (a million)
  tokens; one call reports at most `MaxUsageTokensPerCall`; a run's tool calls default to
  `DEFAULT_TOOL_CALLS_PER_MINUTE` = 60.

## The spec

`limits` gains three fields, checked by the schema in Go and in the runner:

| Field | Meaning |
|---|---|
| `maxCost` | the most the run's model calls may cost, in US dollars, above 0, at most 100000 |
| `maxConcurrent` | how many runs of the workflow may be starting or running at once, 1 to 100 |
| `toolCallsPerMinute` | how many tool calls the run may make in any minute, 1 to 600, 60 when unset |

`maxTokens` now counts across every sandbox of a run, by the control plane, besides the runner's own count.

## Usage

- **Reporting:** after each model call the runner sends a numbered event `llm.usage`
  `{model, inputTokens, outputTokens}` under the workflow node that made it. The control plane adds it to the run's
  and the person's day's totals in the transaction that stores the event, so a resent event counts once.
- **Prices:** when a run's stream first opens, its model's prices are kept with its usage; a price changed later
  leaves the run's accounting as it was. A model without prices counts tokens only.
- **Stopping:** after each `llm.usage` the run is checked, in this order, and the first limit passed ends it
  `failed` with the reason, a `run.finished` event saying it with the totals, and a `Cancel` to its runner:

  | Reason | Limit |
  |---|---|
  | `max_tokens` | the workflow's `maxTokens`, across sandboxes |
  | `max_cost` | the workflow's `maxCost` |
  | `daily_tokens` | the person's tokens a day |
  | `daily_cost` | the person's budget a day |

  A run whose tool calls pass `toolCallsPerMinute` ends `failed` with `tool_rate`, in the runner, inside an agent too.
- **Starting:** `POST /workflows/{id}/runs`, and a trigger's firing, are refused with 422:
  - `quota_reached` when the person's day's tokens or budget are used up;
  - `unpriced_model` when the workflow sets `maxCost` and its model has no prices.

  A trigger's firing refused this way is tried again for `TriggerFireRetryFor`, then fails; the trigger stays on.
- **Concurrency:** a queued run starts only while fewer than its workflow's `maxConcurrent`, and fewer than its
  person's concurrent runs, are starting or running; otherwise it stays queued and starts as others end. A run
  waiting for an approval holds no sandbox and no place. The worker counts again under a lock per workflow and per
  person before a run starts, so workers running side by side never start more than the limits allow.

| Operation | Needs | Answers |
|---|---|---|
| `GET /usage` | signed in | `{today, month, days, limits}`: the caller's own, days of the last `UsageHistoryDays` |
| `GET /usage/people` | admin | `{people}`: each person's today and this month, most first |
| `GET /settings/limits` | admin | `{dailyTokens, dailyCost, concurrentRuns}`, null for no limit |
| `PUT /settings/limits` | admin | as GET; 422 with a field per value out of range; audited `limits.changed` |
| `GET /providers/{providerID}/prices` | admin | `{prices: [{model, inputPerMillion, outputPerMillion}]}` |
| `PUT /providers/{providerID}/prices` | admin | replaces them; 422 for a model the provider does not list; audited `provider.changed` |

`GET /runs/{runID}` and the run lists gain `usage {inputTokens, outputTokens, cost}`.

## What is left for later

- Usage metrics for Prometheus, with the observability work (#37).
- Limits per group, per workflow owner or per tool.
- Usage is as honest as the runner that reports it: a tampered sandbox could under-report. The tools' and providers'
  own records are the check.
