#!/usr/bin/env bash
# Tests for scripts/stack-ports.sh: every checkout's twenty ports stay inside 30000 to 32759, clear of
# Exciter's and Stator's ranges and the kernel's ephemeral range from 32768.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
PORTS="$SRC/scripts/stack-ports.sh"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

echo "== stack ports"

width="$("$PORTS" --width)"
read -r floor ceiling < <("$PORTS" --range)
mapfile -t denied < <("$PORTS" --denied)
mapfile -t bases < <("$PORTS" --all)

(( floor >= 30000 && ceiling < 32768 )) && pass "the range $floor to $ceiling sits above Stator's and below the ephemeral ports" \
  || fail "the range $floor to $ceiling overlaps another stack or the ephemeral ports"

bad=""
for base in "${bases[@]}"; do
  last=$((base + width - 1))
  (( base >= floor && last <= ceiling )) || bad+=" $base"
  for port in "${denied[@]}"; do
    (( port < base || port > last )) || bad+=" $base(holds $port)"
  done
done
[[ -z "$bad" ]] && pass "all ${#bases[@]} slots lie in the range and hold no denied port" \
  || fail "these slots break the rule:$bad"

free="$(printf '%s\n' "${bases[@]}" | sort -u | wc -l)"
(( free >= ${#bases[@]} - ${#denied[@]} )) && pass "$free distinct slots; only the denied ones are given up" \
  || fail "only $free distinct slots for ${#bases[@]} hashes"

a="$("$PORTS" /home/someone/rotor)"
b="$("$PORTS" /home/someone/rotor)"
c="$("$PORTS" /home/someone/rotor/.claude/worktrees/agent-x)"
[[ "$a" == "$b" ]] && pass "a checkout keeps its ports" || fail "the same path gave $a and $b"
[[ "$a" != "$c" ]] && pass "a worktree gets ports of its own" || fail "a checkout and its worktree share $a"

"$PORTS" > /dev/null 2>&1 && fail "a call without a path was accepted" || pass "a call without a path is refused"

grep -q 'scripts/stack-ports.sh' "$SRC/mk/stack.mk" && pass "mk/stack.mk takes its ports from the script" \
  || fail "mk/stack.mk does not use scripts/stack-ports.sh"

if (( FAILED )); then echo "FAIL: port tests"; exit 1; fi
echo "PASS: port tests"
