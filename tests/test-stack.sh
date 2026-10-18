#!/usr/bin/env bash
# Tests the stack's definition without starting it: compose accepts the file, nothing is published beyond
# the loopback, and the realm holds the clients and groups Rotor and its siblings sign in through.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE="$SRC/deploy/docker-compose.yml"
REALM="$SRC/deploy/keycloak/realm.json"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
client() { jq -c --arg c "$1" '.clients[] | select(.clientId == $c)' "$REALM"; }

echo "== compose stack"

if config="$(docker compose -f "$COMPOSE" config --format json 2>&1)"; then
  pass "compose accepts deploy/docker-compose.yml"
  open="$(jq -r '.services | to_entries[] | .key as $s | (.value.ports // [])[] | select(.host_ip != "127.0.0.1") | "\($s):\(.published)"' <<< "$config")"
  [[ -z "$open" ]] && pass "every published port is bound to 127.0.0.1" || fail "published beyond the loopback: $open"
  missing="$(jq -r '.services | to_entries[] | select(.value.healthcheck == null and .value.restart != "no") | .key' <<< "$config")"
  [[ -z "$missing" ]] && pass "every long-running service declares a health check" || fail "no health check: $missing"
  oneshot="$(jq -r '.services | to_entries[] | select(.value.restart == "no") | .key' <<< "$config" | sort | tr '\n' ' ')"
  waited="$(jq -r '[.services[] | (.depends_on // {}) | to_entries[] | select(.value.condition == "service_completed_successfully") | .key] | unique | join(" ")' <<< "$config")"
  [[ "${oneshot% }" == "$waited" ]] && pass "every one-shot service is waited for, so up --wait accepts it" \
    || fail "one-shot services ($oneshot) and the ones something waits for ($waited) differ"

  # Every service that names an issuer, client or secret names the realm's own, so a renamed one fails here.
  realm="$(jq -r .realm "$REALM")"
  while IFS=$'\t' read -r service issuer id secret; do
    [[ -z "$issuer" || "${issuer##*/realms/}" == "$realm" ]] || { fail "$service names issuer $issuer, not realm $realm"; continue; }
    if [[ -n "$id" ]]; then
      [[ "$(client "$id" | jq -r .secret)" == "$secret" ]] || { fail "$service signs in as $id with a secret the realm does not hold"; continue; }
    fi
    pass "$service names the realm's own issuer${id:+ and client $id}"
  done < <(jq -r '.services | to_entries[] | select(.value.environment.ROTOR_OIDC_ISSUER != null)
    | [.key, .value.environment.ROTOR_OIDC_ISSUER, (.value.environment.ROTOR_OIDC_CLIENT_ID // ""),
       (.value.environment.ROTOR_OIDC_CLIENT_SECRET // "")] | @tsv' <<< "$config")
  [[ "$(jq -r '.services.api.environment.ROTOR_OIDC_CLIENT_ID // ""' <<< "$config")" == rotor ]] \
    && pass "the api signs people in as the realm's client rotor" || fail "the api does not sign in as client rotor"
else
  fail "compose refuses the file: $config"
fi

echo "== keycloak realm"
[[ "$(jq -r .realm "$REALM")" == dynamo ]] && pass "the realm is dynamo" || fail "the realm is not dynamo"

# Armature's sign-in sends no PKCE challenge yet, so its client asks for none.
for id in rotor exciter stator armature; do
  want=$'S256\t1'
  [[ "$id" == armature ]] && want=$'\t1'
  got="$(client "$id" | jq -r '[.attributes["pkce.code.challenge.method"] // "",
    ([.protocolMappers[] | select(.config["claim.name"] == "groups")] | length)] | @tsv')"
  [[ "$got" == "$want" ]] && pass "client $id carries the groups claim, with PKCE where its product sends it" \
    || fail "client $id is missing, or lacks the groups claim or the PKCE its product sends: '$got'"
done

backchannel="$(client rotor | jq -r '[.attributes["backchannel.logout.url"] // "", .attributes["backchannel.logout.session.required"] // ""] | @tsv')"
[[ "$backchannel" == $'http://api:8080/api/v1/auth/oidc/backchannel-logout\ttrue' ]] \
  && pass "client rotor sends back-channel logouts with the session id to the api" \
  || fail "client rotor's back-channel logout is '$backchannel'"

control="$(client rotor-control-plane | jq -r '[.serviceAccountsEnabled, .standardFlowEnabled,
  ([.protocolMappers[] | select(.config["included.custom.audience"] == "openbao")] | length)] | @tsv')"
[[ "$control" == $'true\tfalse\t1' ]] && pass "client rotor-control-plane is a service account whose tokens name openbao" \
  || fail "client rotor-control-plane is '$control'"
jq -e '.users[] | select(.serviceAccountClientId == "rotor-control-plane")' "$REALM" > /dev/null \
  && pass "the realm holds rotor-control-plane's service account" || fail "rotor-control-plane has no service account user"

jq -e '.users[] | select(.username == "alice") | .groups | index("/rotor-administrators")' "$REALM" > /dev/null \
  && pass "alice administers Rotor" || fail "alice is not in rotor-administrators"
unknown="$(jq -r '[.groups[].path] as $g | .users[] | (.groups // [])[] | select(. as $x | $g | index($x) | not)' "$REALM")"
[[ -z "$unknown" ]] && pass "every user's groups exist" || fail "users name groups that do not exist: $unknown"

if (( FAILED )); then echo "FAIL: stack tests"; exit 1; fi
echo "PASS: stack tests"
