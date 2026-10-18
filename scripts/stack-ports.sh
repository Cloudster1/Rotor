#!/usr/bin/env bash
# Prints the first of the 20 host ports a checkout's stack publishes, derived from the checkout's path.
# Usage: scripts/stack-ports.sh <checkout path> | --slot <n> | --all | --width | --denied | --range
set -euo pipefail

# Exciter holds 10000 to 19999 and Stator 20000 to 29999; the kernel's ephemeral ports start at 32768.
PORT_FLOOR=30000
SLOT_WIDTH=20
SLOT_COUNT=138
# Ports that other services on the host are known to hold; none fall in Rotor's range today.
DENIED_PORTS=()

slot_is_free() {
  local base=$(($1 * SLOT_WIDTH + PORT_FLOOR)) port
  for port in "${DENIED_PORTS[@]}"; do
    (( port < base || port >= base + SLOT_WIDTH )) || return 1
  done
}

# A slot holding a denied port gives way to the next free one, so every hash still lands somewhere.
base_for_slot() {
  local slot=$(($1 % SLOT_COUNT)) tries=0
  until slot_is_free "$slot"; do
    slot=$(((slot + 1) % SLOT_COUNT))
    (( ++tries < SLOT_COUNT )) || { echo "stack-ports.sh: every slot holds a denied port" >&2; exit 1; }
  done
  echo $((slot * SLOT_WIDTH + PORT_FLOOR))
}

case "${1:-}" in
  --slot) base_for_slot "$2" ;;
  --all) for ((s = 0; s < SLOT_COUNT; s++)); do base_for_slot "$s"; done ;;
  --width) echo "$SLOT_WIDTH" ;;
  --denied) (( ${#DENIED_PORTS[@]} == 0 )) || printf '%s\n' "${DENIED_PORTS[@]}" ;;
  --range) echo "$PORT_FLOOR $((PORT_FLOOR + SLOT_COUNT * SLOT_WIDTH - 1))" ;;
  "" | -*) echo "Usage: $0 <checkout path> | --slot <n> | --all | --width | --denied | --range" >&2; exit 2 ;;
  *) base_for_slot "$(printf '%s' "$1" | cksum | cut -d' ' -f1)" ;;
esac
