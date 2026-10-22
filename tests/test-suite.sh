#!/usr/bin/env bash
# Checks the suite's Exciter and Armature answer as tools Rotor can connect to, at the address a browser opens.
# Usage: tests/test-suite.sh <Exciter's web port> <Armature's web port>; make test-suite runs it after make suite-up.
set -u

EXCITER_PORT="${1:?name the suite Exciter web port}"
ARMATURE_PORT="${2:?name the suite Armature web port}"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:8.16.0}"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
get() { docker run --rm --network host "$CURL_IMAGE" -fsS "$BASE$1"; }

BASE="http://localhost:$EXCITER_PORT"
echo "== suite Exciter at $BASE"
get /readyz > /dev/null && pass "Exciter is ready" || fail "Exciter's /readyz does not answer 200"
meta="$(get /api/v1/meta)"
jq -e '.product == "exciter" and (.capabilities | index("mcp@1"))' <<< "$meta" > /dev/null \
  && pass "Exciter offers mcp@1" || fail "Exciter's /api/v1/meta lacks mcp@1: $meta"
resource="$(get /.well-known/oauth-protected-resource/api/v1/mcp)"
jq -e --arg b "$BASE" '.resource == ($b + "/api/v1/mcp") and .authorization_servers[0] == $b' <<< "$resource" > /dev/null \
  && pass "Exciter's MCP endpoint names Exciter as its authorization server" || fail "the protected resource reads $resource"
server="$(get /.well-known/oauth-authorization-server)"
jq -e --arg b "$BASE" '.issuer == $b and (.code_challenge_methods_supported | index("S256")) and .registration_endpoint != null' \
  <<< "$server" > /dev/null && pass "Exciter's authorization server takes S256 and registrations" || fail "the server metadata reads $server"

# Armature's web proxies its api and both documents, so a browser and Rotor discover it at one address (#45).
BASE="http://localhost:$ARMATURE_PORT"
echo "== suite Armature at $BASE"
meta="$(get /api/v1/meta)"
jq -e '.product == "armature" and (.capabilities | index("mcp@1")) and (.capabilities | index("oauth.token-exchange@1"))' \
  <<< "$meta" > /dev/null && pass "Armature offers mcp@1 and token exchange" || fail "Armature's /api/v1/meta reads $meta"
resource="$(get /.well-known/oauth-protected-resource)"
jq -e --arg b "$BASE" '.resource == ($b + "/api/v1") and .authorization_servers[0] == $b' <<< "$resource" > /dev/null \
  && pass "Armature's API names Armature as its authorization server" || fail "the protected resource reads $resource"
server="$(get /.well-known/oauth-authorization-server)"
jq -e --arg b "$BASE" '.issuer == $b and (.grant_types_supported | index("urn:ietf:params:oauth:grant-type:token-exchange"))
  and .registration_endpoint != null' <<< "$server" > /dev/null \
  && pass "Armature's authorization server exchanges tokens and takes registrations" || fail "the server metadata reads $server"

if (( FAILED )); then echo "FAIL: suite tests"; exit 1; fi
echo "PASS: suite tests"
