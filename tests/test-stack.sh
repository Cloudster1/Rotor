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

  # The api signs in to OpenBao as the realm's service account, with the secret the realm holds for it.
  read -r bao_id bao_secret < <(jq -r '.services.api.environment | [.ROTOR_CONTROL_PLANE_CLIENT_ID // "", .ROTOR_CONTROL_PLANE_CLIENT_SECRET // ""] | @tsv' <<< "$config")
  [[ "$bao_id" == rotor-control-plane && "$(client rotor-control-plane | jq -r .secret)" == "$bao_secret" ]] \
    && pass "the api signs in to OpenBao as rotor-control-plane with the realm's secret" \
    || fail "the api signs in to OpenBao as '$bao_id' with a secret the realm does not hold"
  [[ "$(jq -r '.services.api.environment.ROTOR_OPENBAO_ADDR // ""' <<< "$config")" == http://openbao:8200 ]] \
    && pass "the api keeps its secrets in the stack's OpenBao" || fail "the api does not name the stack's OpenBao"
  jq -e '.services.openbao.volumes[] | select(.target == "/openbao/file" and .type == "volume")' <<< "$config" > /dev/null \
    && pass "OpenBao keeps its data on a volume" || fail "OpenBao keeps its data nowhere that survives a restart"
  # Sandboxes join one network with no route out; the worker alone holds the Docker socket, and only in development.
  [[ "$(jq -r '.networks.sandbox.internal // false' <<< "$config")" == true ]] \
    && pass "the sandbox network has no route out" || fail "the sandbox network reaches beyond the stack"
  joined="$(jq -r '[.services | to_entries[] | select(.value.networks // {} | has("sandbox")) | .key] | sort | join(" ")' <<< "$config")"
  [[ "$joined" == "api openbao tool-stub" ]] && pass "only the api, OpenBao and the tool double share the sandbox network" \
    || fail "the sandbox network is shared with: $joined"
  socket="$(jq -r '[.services | to_entries[] | select([(.value.volumes // [])[] | .source] | index("/var/run/docker.sock")) | .key] | join(" ")' <<< "$config")"
  [[ "$socket" == worker ]] && pass "the worker alone holds the Docker socket" || fail "the Docker socket is mounted into: $socket"
  [[ "$(jq -r '[.services[] | .ports // [] | .[] | select(.target == 9443)] | length' <<< "$config")" == 0 ]] \
    && pass "nothing publishes a sandbox port" || fail "a sandbox port is published"
  [[ "$(jq -r '.services["openbao-sealed"].environment.ROTOR_OPENBAO_STAY_SEALED // ""' <<< "$config")" == 1 ]] \
    && pass "a second OpenBao stays sealed, for the readiness test" || fail "no sealed OpenBao for the readiness test"
else
  fail "compose refuses the file: $config"
fi

# The observe profile (#37): a collector, Jaeger and Prometheus, on the loopback alone, which the control plane feeds.
if observed="$(docker compose -f "$COMPOSE" --profile observe config --format json 2>&1)"; then
  extra="$(jq -r '[.services | keys[] | select(. == "otel-collector" or . == "jaeger" or . == "prometheus")] | join(" ")' <<< "$observed")"
  [[ "$extra" == "jaeger otel-collector prometheus" ]] && pass "the observe profile adds a collector, Jaeger and Prometheus" \
    || fail "the observe profile adds: $extra"
  open="$(jq -r '.services | to_entries[] | .key as $s | (.value.ports // [])[] | select(.host_ip != "127.0.0.1") | "\($s):\(.published)"' <<< "$observed")"
  [[ -z "$open" ]] && pass "the observe profile publishes on 127.0.0.1 alone" || fail "the observe profile publishes beyond the loopback: $open"
  for service in api worker; do
    [[ "$(jq -r --arg s "$service" '.services[$s].environment.ROTOR_OTLP_ENDPOINT // ""' <<< "$observed")" == http://otel-collector:4318/v1/traces ]] \
      && pass "the $service sends its traces to the collector" || fail "the $service does not name the collector"
  done
else
  fail "compose refuses the observe profile: $observed"
fi

echo "== tool test double"
if [[ -n "${config:-}" ]] && jq -e .services <<< "$config" > /dev/null 2>&1; then
  # The control API lets anyone end grants and forget clients, so only the stack's network may reach it.
  [[ "$(jq -r '[.services["tool-stub"].ports[]?.target] | map(tostring) | join(" ")' <<< "$config")" == 8080 ]] \
    && pass "tool-stub publishes its tools alone, never its control API" || fail "tool-stub publishes other than port 8080"
  stub_port="$(jq -r '.services["tool-stub"].ports[0].published' <<< "$config")"
  [[ ",$(jq -r '.services.api.environment.ROTOR_TOOLS_BACKCHANNEL // ""' <<< "$config")," == *",http://localhost:$stub_port=http://tool-stub:8080,"* ]] \
    && pass "the api reaches the tool-stub's published address on the stack's network" \
    || fail "ROTOR_TOOLS_BACKCHANNEL does not rewrite the tool-stub's address $stub_port"
  # The suite's Armature is reached past its web, at the api the suite's network and the sandboxes know (#130).
  [[ ",$(jq -r '.services.api.environment.ROTOR_TOOLS_BACKCHANNEL // ""' <<< "$config")," =~ ,http://localhost:[0-9]+=http://suite-armature-api:8080, ]] \
    && pass "the api reaches the suite's Armature at its api" \
    || fail "ROTOR_TOOLS_BACKCHANNEL does not rewrite the suite's Armature to suite-armature-api:8080"
fi
[[ "$(tr -d '[:space:]' < "$SRC/deploy/suite/EXCITER_REF")" =~ ^[0-9a-f]{40}$ ]] \
  && pass "the suite pins Exciter to a commit" || fail "deploy/suite/EXCITER_REF is not a full commit hash"
for tool in exciter armature stator; do
  [[ -s "$SRC/api/tools/$tool/openapi.json" && "$(cut -d' ' -f2 "$SRC/api/tools/$tool/SOURCE")" =~ ^[0-9a-f]{40}$ ]] \
    && pass "$tool's document is vendored at a commit" || fail "api/tools/$tool lacks its document or the commit it came from"
done
# Each tool's tools/list is recorded at the suite's pin, and its document vendored at the same commit, so the stub's
# answers and the list it serves are one release of the tool (#129, #139).
for tool in exciter armature; do
  ref="$(tr -d '[:space:]' < "$SRC/deploy/suite/${tool^^}_REF")"
  [[ "$ref" =~ ^[0-9a-f]{40}$ && "$(cut -d' ' -f2 "$SRC/api/tools/$tool/SOURCE")" == "$ref" ]] \
    && pass "$tool's document is vendored at the commit its tools/list is recorded at" \
    || fail "deploy/suite/${tool^^}_REF is not the commit api/tools/$tool/SOURCE names"
  jq -e '.tools | length > 0' "$SRC/api/tools/$tool/tools-list.json" > /dev/null 2>&1 \
    && pass "$tool's tools/list is recorded" || fail "api/tools/$tool/tools-list.json is no tools/list result"
done
[[ -f "$SRC/deploy/suite/exciter-snapshot.yml" && -f "$SRC/deploy/suite/armature-snapshot.yml" ]] \
  && pass "each tool's snapshot has a throwaway installation" || fail "deploy/suite lacks a <tool>-snapshot.yml"

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

echo "== the realm check against Dynamo's"
# Data URLs stand in for Dynamo, so the check is tested offline: the shared realm must match, Rotor's part may differ.
NODE_IMAGE="${NODE_IMAGE:-node:26-alpine}"
check() { # url, as Dynamo's
  docker run --rm -v "$SRC/deploy/keycloak:/realm:ro" "$NODE_IMAGE" node /realm/realm-check.mjs "$1" /realm/realm.json >/dev/null 2>&1
}
as_url() { echo "data:application/json;base64,$(base64 -w0)"; }
shared="$(jq '.clients |= map(select(.clientId != "rotor" and .clientId != "rotor-control-plane"))
  | .groups |= map(select(.name != "rotor-administrators"))
  | .users |= map(select((.serviceAccountClientId // "") | startswith("rotor") | not) | (.groups //= []) | .groups -= ["/rotor-administrators"])' "$REALM")"
check "$(as_url <<< "$shared")" && pass "a Dynamo realm without Rotor's part matches" || fail "the shared realm does not match itself"
check "$(as_url < "$REALM")" && pass "a Dynamo realm with Rotor's part matches too" || fail "Rotor's part in Dynamo's realm makes a difference"
changed="$(jq '.clients |= map(if .clientId == "exciter" then .protocolMappers = [] else . end)' <<< "$shared")"
check "$(as_url <<< "$changed")" && fail "a changed shared client passes the check" || pass "a changed shared client fails the check"

if (( FAILED )); then echo "FAIL: stack tests"; exit 1; fi
echo "PASS: stack tests"
