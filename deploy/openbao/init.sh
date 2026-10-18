#!/bin/sh
# Configures OpenBao for Rotor; safe to run again on every start.
set -eu

enabled() { bao "$1" list -format=json | grep -q "\"$2/\""; }

enabled secrets rotor || bao secrets enable -path=rotor -version=2 kv >/dev/null
# Every write names the version it read, so two writers never overwrite each other unseen.
bao write rotor/config cas_required=true max_versions=3 >/dev/null
enabled secrets transit || bao secrets enable transit
bao read transit/keys/rotor-checkpoints >/dev/null 2>&1 || bao write -f transit/keys/rotor-checkpoints >/dev/null

for policy in /rotor/policies/*.hcl; do
  bao policy write "$(basename "$policy" .hcl)" "$policy" >/dev/null
done

# The control plane signs in with a Keycloak service-account token. Keys come from the JWKS on the stack's
# network, while bound_issuer is the browser-facing address Keycloak writes into every token.
enabled auth jwt || bao auth enable jwt
bao write auth/jwt/config jwks_url="$ROTOR_OIDC_JWKS_URL" bound_issuer="$ROTOR_OIDC_ISSUER" >/dev/null
bao write auth/jwt/role/rotor-control-plane - >/dev/null <<JSON
{
  "role_type": "jwt",
  "bound_audiences": ["openbao"],
  "bound_claims": {"azp": "rotor-control-plane"},
  "user_claim": "azp",
  "token_policies": ["rotor-control-plane"],
  "token_ttl": "1h",
  "token_max_ttl": "24h"
}
JSON

echo "OpenBao is configured for Rotor"
