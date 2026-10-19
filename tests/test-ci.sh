#!/usr/bin/env bash
# Tests that CI's jobs between them run every layer run-tests.sh knows and every browser shard once,
# so a layer dropped from the matrix cannot stop gating pull requests without anything turning red.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CI="$SRC/.github/workflows/ci.yml"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

echo "== ci.yml runs the whole gate"

ci_layers=$(sed -n 's/^ *layers: *//p' "$CI" | tr ' ' '\n' | sort)
for layer in $("$SRC/run-tests.sh" --list); do
  if grep -qx "$layer" <<< "$ci_layers"; then
    pass "a job runs $layer"
  else
    fail "no job in ci.yml runs $layer; add it to a job's layers"
  fi
done

for layer in $(sort -u <<< "$ci_layers"); do
  "$SRC/run-tests.sh" --list | grep -qx "$layer" \
    || fail "ci.yml names $layer, which run-tests.sh does not know"
done

dupes=$(uniq -d <<< "$(grep -vx test-e2e <<< "$ci_layers")")
[[ -z "$dupes" ]] && pass "no layer but the browser suite runs twice" \
  || fail "these layers run in more than one job: $dupes"

shards=$(sed -n 's/^ *shard: *//p' "$CI")
e2e_jobs=$(grep -cx test-e2e <<< "$ci_layers")
totals=$(cut -d/ -f2 <<< "$shards" | sort -u)
if [[ $(wc -l <<< "$totals") -ne 1 ]]; then
  fail "the browser jobs disagree on how many parts there are: $(paste -sd' ' <<< "$totals")"
elif [[ "$e2e_jobs" -ne "$totals" || $(wc -l <<< "$shards") -ne "$totals" ]]; then
  fail "$e2e_jobs browser jobs and $(wc -l <<< "$shards") shards for $totals parts; each part needs one job"
else
  missing=""
  for i in $(seq "$totals"); do grep -qx "$i/$totals" <<< "$shards" || missing+=" $i/$totals"; done
  [[ -z "$missing" ]] && pass "the $totals browser jobs run each part once" \
    || fail "no browser job runs part$missing"
fi

echo "== deps.yml audits every lock file (#42)"
DEPS="$SRC/.github/workflows/deps.yml"
grep -q 'run: make audit-deps' "$DEPS" && pass "deps.yml runs make audit-deps" || fail "deps.yml does not run make audit-deps"
for lock in backend/go.sum runner/uv.lock web/package-lock.json; do
  grep -qx " *- $lock" "$DEPS" && pass "a change to $lock runs the audit" \
    || fail "deps.yml's paths leave out $lock, so a change to it is never audited"
done

echo "== Renovate waits two weeks for a release (#42)"
age=$(jq -r '.minimumReleaseAge // ""' "$SRC/renovate.json")
[[ "${age%% *}" =~ ^[0-9]+$ && "${age%% *}" -ge 14 && "$age" == *day* ]] && pass "minimumReleaseAge is $age" \
  || fail "renovate.json's minimumReleaseAge is [$age]; Rotor takes no release younger than 14 days"

if [[ $FAILED -eq 0 ]]; then echo "ci.yml tests passed"; else echo "ci.yml tests failed"; fi
exit $FAILED
