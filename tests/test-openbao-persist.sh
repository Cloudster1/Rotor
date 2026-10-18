#!/usr/bin/env bash
# Proves the stack's OpenBao keeps a secret across a restart, as people's connections need. Run by
# make test-integration against this checkout's running stack, before the Go suite.
set -euo pipefail

: "${OPENBAO_PORT:?run this through make test-integration}"
addr="http://127.0.0.1:${OPENBAO_PORT}"
token="${OPENBAO_ROOT_TOKEN:-rotor-dev-root}"
path="rotor/data/gate/persist-$$"
value="kept-$RANDOM"

bao() { curl -fsS -H "X-Vault-Token: $token" "$@"; }

bao -X POST "$addr/v1/$path" -d "{\"options\":{\"cas\":0},\"data\":{\"value\":\"$value\"}}" >/dev/null
docker compose restart openbao >/dev/null 2>&1
docker compose up -d --wait openbao >/dev/null 2>&1
got="$(bao "$addr/v1/$path" | jq -r .data.data.value)"
bao -X DELETE "$addr/v1/${path/\/data\//\/metadata\/}" >/dev/null

if [[ "$got" != "$value" ]]; then
  echo "FAIL OpenBao forgot a secret when it restarted: read '$got', wrote '$value'" >&2
  exit 1
fi
echo "PASS OpenBao keeps a secret across a restart"
