#!/usr/bin/env bash
# Checks the suite's Exciter answers as a tool Rotor can connect to: ready, offering mcp@1, and describing its
# authorization server. Usage: tests/test-suite.sh <Exciter's web port>; make test-suite runs it after make suite-up.
set -u

PORT="${1:?name the suite Exciter web port}"
BASE="http://localhost:$PORT"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:8.16.0}"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
get() { docker run --rm --network host "$CURL_IMAGE" -fsS "$BASE$1"; }

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

if (( FAILED )); then echo "FAIL: suite tests"; exit 1; fi
echo "PASS: suite tests"
