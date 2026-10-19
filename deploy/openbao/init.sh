#!/bin/sh
# Configures OpenBao for Rotor; safe to run again on every start. The development stack runs it in its OpenBao,
# the chart in a hook Job against an installation's own. What it signs the control plane in with depends on what
# it is given: a Keycloak JWKS for JWT auth, a cluster's address for Kubernetes auth (#26), or both.
set -eu

POLICY_DIR="${ROTOR_POLICY_DIR:-/rotor/policies}"
# The name sandboxes dial the control plane by, which its runner certificate carries.
RUNNER_HOST="${ROTOR_RUNNER_HOST:-api}"

enabled() { bao "$1" list -format=json | grep -q "\"$2/\""; }

# Who read which secret is known only from OpenBao's audit log, which it takes from its configuration alone (#92).
if ! bao audit list -format=json 2>/dev/null | grep -q '"type"'; then
  if [ "${ROTOR_OPENBAO_REQUIRE_AUDIT:-true}" = true ]; then
    echo "OpenBao has no audit device, so nobody could tell who read Rotor's secrets. Declare one in OpenBao's" \
      "configuration (docs/deployment.md), or set ROTOR_OPENBAO_REQUIRE_AUDIT=false for a throwaway test server." >&2
    exit 1
  fi
  echo "OpenBao has no audit device; going on, as ROTOR_OPENBAO_REQUIRE_AUDIT=false asks" >&2
fi

enabled secrets rotor || bao secrets enable -path=rotor -version=2 kv >/dev/null
# Every write names the version it read, so two writers never overwrite each other unseen.
bao write rotor/config cas_required=true max_versions=3 >/dev/null
enabled secrets transit || bao secrets enable transit
# The installation's key for runs' data keys (#41), rotated every 90 days; older versions still open older runs.
bao read transit/keys/rotor-run-data >/dev/null 2>&1 \
  || bao write transit/keys/rotor-run-data auto_rotate_period=2160h >/dev/null

# A CA for the runner protocol's TLS: the api is issued a certificate for the runner host, sandboxes trust the CA.
enabled secrets pki || bao secrets enable pki >/dev/null
bao secrets tune -max-lease-ttl=87600h pki >/dev/null
bao read -field=certificate pki/cert/ca >/dev/null 2>&1 \
  || bao write -field=certificate pki/root/generate/internal common_name="Rotor runner CA" ttl=87600h >/dev/null
bao write pki/roles/runnerctl allowed_domains="$RUNNER_HOST" allow_bare_domains=true allow_subdomains=false \
  allow_localhost=false key_type=ec key_bits=256 max_ttl=72h >/dev/null

for policy in "$POLICY_DIR"/*.hcl; do
  bao policy write "$(basename "$policy" .hcl)" "$policy" >/dev/null
done

# Outside a cluster the control plane signs in with a Keycloak service-account token. Keys come from the JWKS on
# the stack's network, while bound_issuer is the browser-facing address Keycloak writes into every token.
if [ -n "${ROTOR_OIDC_JWKS_URL:-}" ]; then
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
fi

# In a cluster the api and the worker sign in with their own ServiceAccount tokens. Sandboxes have none: they only
# unwrap what they are handed, which needs no login.
if [ -n "${ROTOR_KUBERNETES_HOST:-}" ]; then
  enabled auth kubernetes || bao auth enable kubernetes
  bao write auth/kubernetes/config kubernetes_host="$ROTOR_KUBERNETES_HOST" >/dev/null
  bao write auth/kubernetes/role/rotor-control-plane \
    bound_service_account_names="$ROTOR_CONTROL_PLANE_SERVICE_ACCOUNTS" \
    bound_service_account_namespaces="$ROTOR_CONTROL_PLANE_NAMESPACE" \
    token_policies=rotor-control-plane token_ttl=1h token_max_ttl=24h >/dev/null
fi

echo "OpenBao is configured for Rotor"
