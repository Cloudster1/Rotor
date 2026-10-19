#!/usr/bin/env bash
# Tests what scripts/supply.sh refuses before it touches a registry (#42); the kind test proves the signing itself.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
SUPPLY="${SRC}/scripts/supply.sh"
DIGEST=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }

refuses() { # description expected-message env-and-args...
    local what="$1" message="$2" out
    shift 2
    if out=$(env "$@" 2>&1); then
        fail "${what}: succeeded, but should have been refused"
    elif grep -qF -- "${message}" <<<"${out}"; then
        pass "${what}"
    else
        fail "${what}: refused without saying [${message}]: ${out}"
    fi
}

echo "== scripts/supply.sh"
refuses "no image" "Usage:" "${SUPPLY}" sign
refuses "an unknown step" "Usage:" "${SUPPLY}" publish "registry.example/rotor-runner@${DIGEST}"
refuses "an image by tag" "Name registry.example/rotor-runner:1.0 by its digest" \
    "${SUPPLY}" sign "registry.example/rotor-runner:1.0"
refuses "signing without a key" "Set COSIGN_KEY" COSIGN_KEY= "${SUPPLY}" sign "registry.example/rotor-runner@${DIGEST}"
refuses "verifying without a key" "Set COSIGN_PUB" COSIGN_PUB= "${SUPPLY}" verify "registry.example/rotor-runner@${DIGEST}"
refuses "attesting without an SBOM" "run make sbom first" COSIGN_KEY=/dev/null SBOM_DIR=/nonexistent \
    "${SUPPLY}" attest "registry.example/rotor-runner@${DIGEST}"
refuses "verifying keyless without an identity" "Set COSIGN_IDENTITY" COSIGN_KEYLESS=1 COSIGN_IDENTITY= \
    "${SUPPLY}" verify "registry.example/rotor-runner@${DIGEST}"
refuses "verifying keyless without an issuer" "Set COSIGN_ISSUER" COSIGN_KEYLESS=1 COSIGN_IDENTITY=x COSIGN_ISSUER= \
    "${SUPPLY}" verify "registry.example/rotor-runner@${DIGEST}"

echo "== the release signs keyless, as its workflow and the chart's admission expect"
release_identity=$(sed -n 's/^COSIGN_IDENTITY ?= //p' "${SRC}/mk/supply.mk")
if grep -qF -- "${release_identity}" "${SRC}/deploy/charts/rotor/values.yaml"; then
    pass "the chart trusts the identity the release signs with"
else
    fail "values.yaml's sandbox.admission.keyless.subjectRegExp is not mk/supply.mk's COSIGN_IDENTITY ${release_identity}"
fi

if [[ "${FAILED}" == 0 ]]; then echo "PASS: supply tests"; else echo "FAIL: supply tests"; fi
exit "${FAILED}"
