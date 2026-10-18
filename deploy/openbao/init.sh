#!/bin/sh
# Configures OpenBao for Rotor; safe to run again. Roles and policies arrive with the tool connections (M1).
set -eu

enabled() { bao "$1" list -format=json | grep -q "\"$2/\""; }

enabled secrets rotor || bao secrets enable -path=rotor -version=2 kv
enabled secrets transit || bao secrets enable transit
bao read transit/keys/rotor-checkpoints >/dev/null 2>&1 || bao write -f transit/keys/rotor-checkpoints

# The control plane signs in with a Keycloak service-account token. Keys come from the JWKS on the stack's
# network, while bound_issuer is the browser-facing address Keycloak writes into every token.
enabled auth jwt || bao auth enable jwt
bao write auth/jwt/config jwks_url="$ROTOR_OIDC_JWKS_URL" bound_issuer="$ROTOR_OIDC_ISSUER"

echo "OpenBao is configured for Rotor"
