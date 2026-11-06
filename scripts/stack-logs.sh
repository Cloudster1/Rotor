#!/usr/bin/env bash
# Saves the logs of the compose stack's services that are not running healthy, so a stack that failed to start
# leaves a clue after stack-down removes its containers (#278). CI uploads reports/ with the other reports.
# Usage: scripts/stack-logs.sh [dir], default reports/stack-logs; the stack comes from COMPOSE_FILE and COMPOSE_PROJECT_NAME.
set -uo pipefail

dir="${1:-reports/stack-logs}"
# Bounded, so a chatty service cannot fill the artifact.
tail_lines="${STACK_LOGS_TAIL:-2000}"
mkdir -p "$dir"

docker compose --profile observe ps -a --format json >"$dir/ps.jsonl" 2>"$dir/ps.err" || true

# A service that is running without a health check, healthy, or exited 0 (a one-shot that did its job) is fine.
jq -r 'select(((.State == "running") and ((.Health // "") | . == "" or . == "healthy"))
    or (.State == "exited" and .ExitCode == 0) | not) | .Service' "$dir/ps.jsonl" 2>/dev/null | sort -u >"$dir/.bad"

while read -r service; do
  [[ -n "$service" ]] || continue
  docker compose --profile observe logs --no-color --timestamps --tail "$tail_lines" "$service" >"$dir/$service.log" 2>&1 || true
  echo "Saved the logs of $service to $dir/$service.log" >&2
done <"$dir/.bad"
rm -f "$dir/.bad" "$dir/ps.err"
