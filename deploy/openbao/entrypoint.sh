#!/bin/sh
# Starts OpenBao with file storage, initializes it once, unseals it, configures it for Rotor and stays in the
# foreground with it. The one unseal key and the first root token stay on the volume: development only.
set -eu

marker=/tmp/rotor-configured
keys=/openbao/file/rotor-init.json
rm -f "$marker"

bao server -config=/rotor/config.hcl &
server=$!
trap 'kill "$server" 2>/dev/null' TERM INT

# bao status exits 0 unsealed, 2 sealed and 1 when it cannot tell, which is while the server starts.
answering() {
  status=0
  bao status >/dev/null 2>&1 || status=$?
  [ "$status" -ne 1 ]
}
tries=0
until answering; do
  tries=$((tries + 1))
  [ "$tries" -lt 60 ] || { echo "OpenBao did not answer within a minute" >&2; exit 1; }
  sleep 1
done

field() { tr -d ' \n' <"$keys" | grep -o "\"$1\":\[*\"[^\"]*\"" | sed 's/.*"\([^"]*\)"$/\1/'; }

if ! bao status -format=json 2>/dev/null | tr -d ' \n' | grep -q '"initialized":true'; then
  bao operator init -key-shares=1 -key-threshold=1 -format=json >"$keys.tmp"
  mv "$keys.tmp" "$keys"
fi

if [ "${ROTOR_OPENBAO_STAY_SEALED:-}" = "1" ]; then
  echo "OpenBao stays sealed, as asked"
  touch "$marker"
  wait "$server"
  exit 0
fi

bao operator unseal "$(field unseal_keys_b64)" >/dev/null

# The documented root token is made from the first one, so docs and tests name a token that never changes.
if ! bao token lookup "$BAO_TOKEN" >/dev/null 2>&1; then
  BAO_TOKEN="$(field root_token)" bao token create -id="$BAO_TOKEN" -policy=root -orphan >/dev/null
fi

sh /rotor/init.sh
touch "$marker"
wait "$server"
