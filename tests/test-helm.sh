#!/usr/bin/env bash
# Tests for deploy/charts/rotor (#29), adapted from Exciter's: lint, a trial and a production render, refusals, the
# sandbox namespace's hardening and the golden renders in tests/helm (UPDATE=1 rewrites them), helm and ct in containers.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CHART="${SRC}/deploy/charts/rotor"
GOLDEN="${SRC}/tests/helm"
HELM_IMAGE="${HELM_IMAGE:-alpine/helm:3.19.0}"
CT_IMAGE="${CT_IMAGE:-quay.io/helmpack/chart-testing:v3.13.0}"
RELEASE=r
FULL="${RELEASE}-rotor"

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

helm() {
    docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "${CHART}:/chart:ro" "${HELM_IMAGE}" "$@"
}

# A setting the chart puts in the shared ConfigMap, as KEY: "value".
config_value() { # rendered name
    sed -n "s/^ *$2: \"\(.*\)\"\$/\1/p" <<<"$1" | sort -u | paste -sd ' '
}

# The values the workloads are given for an environment variable, other than the migrate Job's owner URL.
env_value() { # rendered name
    grep -A1 -- "- name: $2\$" <<<"$1" | sed -n 's/^ *value: "\(.*\)"$/\1/p' | grep -vF 'DB_OWNER_PASSWORD' | sort -u | paste -sd ' '
}

render() { # description args...
    local what="$1"
    shift
    if out=$(helm lint /chart --strict "$@" 2>&1); then
        pass "${what}: helm lint"
    else
        fail "${what}: helm lint"
        echo "${out}" | sed 's/^/      /'
    fi
    if RENDERED=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        pass "${what}: helm template"
        # Sandboxes never run on the Docker daemon outside development, and a chart is never development's stack.
        check "${what}: no Docker socket" "$(grep -c 'docker.sock' <<<"${RENDERED}")" "0"
    else
        fail "${what}: helm template"
        echo "${RENDERED}" | sed 's/^/      /'
        RENDERED=""
    fi
}

refused() { # description expected-message args...
    local what="$1" message="$2"
    shift 2
    if out=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        fail "${what}: rendered, but should have been refused"
    elif grep -qF -- "${message}" <<<"${out}"; then
        pass "${what}"
    else
        fail "${what}: refused without saying [${message}]"
        echo "${out}" | sed 's/^/      /'
    fi
}

golden() { # name args...
    local name="$1" file="${GOLDEN}/$1.yaml"
    shift
    local out
    if ! out=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        fail "golden ${name}: helm template"
        echo "${out}" | sed 's/^/      /'
        return
    fi
    if [[ "${UPDATE:-}" == 1 ]]; then
        mkdir -p "${GOLDEN}"
        printf '%s\n' "${out}" >"${file}"
        pass "golden ${name}: written"
    elif [[ ! -f "${file}" ]]; then
        fail "golden ${name}: ${file} is missing; run UPDATE=1 tests/test-helm.sh and review it"
    elif diff -u "${file}" <(printf '%s\n' "${out}") >"${GOLDEN}/.diff" 2>&1; then
        pass "golden ${name}: the render is unchanged"
    else
        fail "golden ${name}: the render differs; review it, then run UPDATE=1 tests/test-helm.sh"
        head -40 "${GOLDEN}/.diff" | sed 's/^/      /'
    fi
    rm -f "${GOLDEN}/.diff"
}

TRIAL=(-f /chart/values-trial.yaml --set openbao.addr=http://openbao:8200 --set sandbox.egress.openbao.namespace=openbao)
PRODUCTION=(--set openbao.addr=https://openbao.example:8200 --set oidc.issuer=https://id.example/realms/dynamo
    --set oidc.clientSecret.name=rotor-oidc --set database.host=db.example --set valkey.host=valkey.example
    --set valkey.auth=true --set valkey.tls=true
    --set images.runner.digest=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    --set images.backend.digest=sha256:1111111111111111111111111111111111111111111111111111111111111111
    --set images.web.digest=sha256:2222222222222222222222222222222222222222222222222222222222222222
    --set openbao.configure.enabled=true --set openbao.configure.tokenSecret.name=openbao-admin
    --set sandbox.egress.openbao.cidr=10.20.0.0/24 --set sandbox.egress.cilium=true
    --set 'sandbox.egress.hosts={exciter.example.com,api.anthropic.com}')

echo "== chart-testing"
if out=$(docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "${SRC}:/src:ro" -w /src "${CT_IMAGE}" \
    ct lint --charts deploy/charts/rotor --validate-maintainers=false --check-version-increment=false \
    --lint-conf tests/helm/lintconf.yaml 2>&1); then
    pass "ct lint"
else
    fail "ct lint"
    echo "${out}" | tail -30 | sed 's/^/      /'
fi

echo "== a trial"
render "trial" "${TRIAL[@]}"
check "the bundled database serves the app role" "$(env_value "${RENDERED}" ROTOR_DB_URL)" \
    "postgres://rotor_app:\$(DB_APP_PASSWORD)@${FULL}-postgresql:5432/rotor?sslmode=disable"
check "the bundled Valkey has a password" "$(env_value "${RENDERED}" ROTOR_VALKEY_URL)" \
    "redis://:\$(VALKEY_PASSWORD)@${FULL}-valkey:6379/0"
check "sandboxes dial the api's Service" "$(config_value "${RENDERED}" ROTOR_RUNNER_DIAL)" "${FULL}-api.default.svc:9443"
check "the runner certificate names that Service" "$(config_value "${RENDERED}" ROTOR_RUNNER_HOST)" "${FULL}-api.default.svc"
check "sandboxes run under gVisor" "$(config_value "${RENDERED}" ROTOR_SANDBOX_RUNTIME_CLASS)" "gvisor"
check "the sandbox namespace" "$(config_value "${RENDERED}" ROTOR_SANDBOX_NAMESPACE)" "default-sandboxes"
check "the sandbox namespace enforces the restricted standard" \
    "$(grep -c 'pod-security.kubernetes.io/enforce: restricted' <<<"${RENDERED}")" "1"
check "the sandboxes' ServiceAccount carries no token" \
    "$(grep -A10 'name: rotor-sandbox$' <<<"${RENDERED}" | grep -c 'automountServiceAccountToken: false')" "1"
check "the worker may touch pods and secrets alone" \
    "$(grep -A3 'rules:' <<<"${RENDERED}" | grep -c 'resources: \["pods", "secrets"\]')" "1"
check "no workload runs as root or writes its root filesystem" \
    "$(grep -c 'readOnlyRootFilesystem: true' <<<"${RENDERED}")" "$(grep -c '^ *image: ' <<<"${RENDERED}")"

check "nothing reaches a sandbox and it reaches only what is allowed" \
    "$(grep -A16 'kind: NetworkPolicy' <<<"${RENDERED}" | grep -c 'policyTypes: \[Ingress, Egress\]')" "1"
check "a sandbox reaches the api's runner port" "$(grep -c 'port: 9443 }' <<<"${RENDERED}")" "1"
check "a sandbox reaches OpenBao's port" "$(grep -c 'port: 8200 }' <<<"${RENDERED}")" "1"
check "without Cilium no hostname is allowed" "$(grep -c 'kind: CiliumNetworkPolicy' <<<"${RENDERED}")" "0"

echo "== production"
render "production" "${PRODUCTION[@]}"
check "the runner image is named by digest" "$(config_value "${RENDERED}" ROTOR_SANDBOX_IMAGE)" \
    "ghcr.io/cloudster1/rotor-runner@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
check "the control plane signs in to OpenBao as itself" "$(config_value "${RENDERED}" ROTOR_OPENBAO_AUTH)" "kubernetes"
check "cookies are secure" "$(config_value "${RENDERED}" ROTOR_SECURE_COOKIES)" "true"
check "the tools and the provider are allowed by name" "$(grep -c 'matchName: "\(exciter.example.com\|api.anthropic.com\)"' <<<"${RENDERED}")" "2"
check "names are learned through Cilium's DNS proxy" "$(grep -c 'matchPattern: "\*"' <<<"${RENDERED}")" "1"
check "OpenBao outside the cluster is allowed by range on its port" "$(grep -A3 'cidr: 10.20.0.0/24' <<<"${RENDERED}" | grep -c 'port: 8200 }')" "1"
check "OpenBao is configured for the api's and the worker's accounts" \
    "$(env_value "${RENDERED}" ROTOR_CONTROL_PLANE_SERVICE_ACCOUNTS)" "${FULL}-api,${FULL}-worker"
check "the api, the worker and the migrations run the backend image by digest" \
    "$(grep -c 'image: ghcr.io/cloudster1/rotor-backend@sha256:1111' <<<"${RENDERED}")" "3"
check "the web runs its image by digest" "$(grep -c 'image: ghcr.io/cloudster1/rotor-web@sha256:2222' <<<"${RENDERED}")" "1"
check "no signature is required unless asked" "$(grep -c 'kind: ClusterImagePolicy' <<<"${RENDERED}")" "0"
check "Valkey is reached over TLS with its password" "$(env_value "${RENDERED}" ROTOR_VALKEY_URL)" \
    "rediss://:\$(VALKEY_PASSWORD)@valkey.example:6379/0"
check "OpenBao without an audit device is refused" "$(env_value "${RENDERED}" ROTOR_OPENBAO_REQUIRE_AUDIT)" "true"

echo "== sandboxes run signed images only"
# A cosign public key, made for this test alone; the chart only passes it on.
PUBLIC_KEY='-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE1/FIgJXJxXSzGnW7CZFfZZjndy4b
Pwutfi/afYYrwM7iAeW4sRf2uPv+Aahl9mNB2pfcqIT6C5tSw+7UW1BLkQ==
-----END PUBLIC KEY-----'
render "admission" "${PRODUCTION[@]}" --set sandbox.admission.requireSignature=true \
    --set-string "sandbox.admission.publicKey=${PUBLIC_KEY}"
check "the sandbox namespace is handed to policy-controller" \
    "$(grep -c 'policy.sigstore.dev/include: "true"' <<<"${RENDERED}")" "1"
check "one policy wants the signature and another the signed SBOM" \
    "$(grep -c 'name: r-rotor-sandboxes-\(signed\|sbom\)$' <<<"${RENDERED}")" "2"
check "both cover the runner's repository by digest and by tag" \
    "$(grep -c 'glob: "ghcr.io/cloudster1/rotor-runner[@:]\*"' <<<"${RENDERED}")" "4"
check "both trust the runner key" "$(grep -c 'Pwutfi/afYYrwM7iAeW4sRf2uPv+Aahl9mNB2pfcqIT6C5tSw+7UW1BLkQ==' <<<"${RENDERED}")" "2"
check "the SBOM is an SPDX attestation" "$(grep -c 'predicateType: spdxjson' <<<"${RENDERED}")" "1"
check "a key leaves the release's identity out" "$(grep -c 'keyless:' <<<"${RENDERED}")" "0"

echo "== without a key, sandboxes run images Rotor's release signed"
render "keyless admission" "${PRODUCTION[@]}" --set sandbox.admission.requireSignature=true
check "both policies trust the release identity alone" "$(grep -c 'name: rotor-release$' <<<"${RENDERED}")" "2"
check "the identity is GitHub's" \
    "$(grep -c 'issuer: "https://token.actions.githubusercontent.com"' <<<"${RENDERED}")" "2"
check "the identity is the release workflow at a version tag" \
    "$(grep -cF 'subjectRegExp: "^https://github.com/Cloudster1/Rotor/.github/workflows/release.yml@refs/tags/v"' <<<"${RENDERED}")" "2"
check "the SBOM must be signed by it too" "$(grep -c 'predicateType: spdxjson' <<<"${RENDERED}")" "1"
check "no key is trusted" "$(grep -c 'name: runner-key' <<<"${RENDERED}")" "0"

echo "== workspaces (#136)"
render "workspaces" "${PRODUCTION[@]}" --set sandbox.admission.requireSignature=true \
    --set 'sandbox.workspaces.toolchainImages[0]=docker.io/library/golang@sha256:*' \
    --set sandbox.workspaces.packageMirror.cidr=10.21.0.0/24
check "the exec agent is signed like the runner" \
    "$(grep -c 'glob: "ghcr.io/cloudster1/rotor-agent[@:]\*"' <<<"${RENDERED}")" "4"
check "the allowed toolchains run as they are" "$(grep -c 'glob: "docker.io/library/golang@sha256:\*"' <<<"${RENDERED}")" "1"
check "the worker starts toolchains with the agent" \
    "$(config_value "${RENDERED}" ROTOR_WORKSPACE_AGENT_IMAGE)" "ghcr.io/cloudster1/rotor-agent:$(sed -n 's/^appVersion: *"\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' "${CHART}/Chart.yaml")"
check "workspace pods alone reach the mirror" \
    "$(grep -A24 'name: r-rotor-workspace-mirror$' <<<"${RENDERED}" | grep -c 'rotor.dev/workspace: "1"\|cidr: 10.21.0.0/24')" "2"
render "no mirror" "${PRODUCTION[@]}"
check "without a mirror workspaces reach nothing more" "$(grep -c 'workspace-mirror' <<<"${RENDERED}")" "0"
refused "a mirror by hostname without Cilium" "packageMirror.host names a hostname" "${TRIAL[@]}" \
    --set sandbox.workspaces.packageMirror.host=mirror.example.com

echo "== refusals"
refused "runc in production" "would run hostile code without gVisor" "${PRODUCTION[@]}" \
    --set sandbox.runtimeClass= --set sandbox.allowRunc=true
refused "no gVisor without the override" "sandboxes would run without gVisor" "${TRIAL[@]}" --set sandbox.runtimeClass=
refused "no OpenBao" "Set openbao.addr" -f /chart/values-trial.yaml
refused "a Valkey without a password" "valkey.auth is off" "${PRODUCTION[@]}" --set valkey.auth=false
refused "production without sign-in" "Set oidc.issuer" --set openbao.addr=https://openbao.example:8200 --set database.host=db
refused "no database" "Set database.host" --set openbao.addr=https://openbao.example:8200 --set env=development
refused "configuring OpenBao without a token" "openbao.configure is on" "${TRIAL[@]}" --set openbao.configure.enabled=true
refused "hostnames without Cilium" "which only Cilium can allow" "${TRIAL[@]}" --set 'sandbox.egress.hosts={exciter.example.com}'
refused "no word on where OpenBao is" "Say where OpenBao is" -f /chart/values-trial.yaml --set openbao.addr=http://openbao:8200
refused "a signature required without a key or an identity" "set sandbox.admission.publicKey" "${TRIAL[@]}" \
    --set sandbox.admission.requireSignature=true --set sandbox.admission.keyless.subjectRegExp=

echo "== the OpenBao script the chart carries"
if diff -r "${SRC}/deploy/openbao/policies" "${CHART}/files/openbao/policies" >/dev/null &&
    diff "${SRC}/deploy/openbao/init.sh" "${CHART}/files/openbao/init.sh" >/dev/null; then
    pass "files/openbao is deploy/openbao's script and policies"
else
    fail "files/openbao differs from deploy/openbao; copy deploy/openbao/init.sh and policies/ into the chart again"
fi

echo "== golden renders"
golden trial "${TRIAL[@]}"
golden production "${PRODUCTION[@]}"

if [[ "${FAILED}" == 0 ]]; then echo "PASS: chart tests"; else echo "FAIL: chart tests"; fi
exit "${FAILED}"
