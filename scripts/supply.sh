#!/usr/bin/env bash
# Lists what an image is made of and proves it is the one Rotor built (#42), in the kind tools container. Images are
# named by digest, since a signature covers exactly one image and a tag can be moved after it was signed.
set -euo pipefail

usage() {
    cat >&2 <<'EOF'
Usage: supply.sh sbom|sign|attest|verify IMAGE@sha256:DIGEST...

  sbom    write each image's SPDX SBOM to $SBOM_DIR/<name>.spdx.json
  sign    sign each image with the cosign key $COSIGN_KEY
  attest  attach each image's SBOM to it as a signed attestation
  verify  check each image's signature and SBOM attestation against $COSIGN_PUB

REGISTRY_HTTP=1 talks plain HTTP to the registry, as a local one serves.
EOF
    exit 2
}

[[ $# -ge 2 ]] || usage
action="$1"
shift

SBOM_DIR="${SBOM_DIR:-reports/sbom}"
# A key pair and no transparency log by default: a private registry's images have no business in a public log, and
# the release workflow (#44) signs keyless with its own flags.
COSIGN_SIGN_FLAGS="${COSIGN_SIGN_FLAGS:---tlog-upload=false --use-signing-config=false --new-bundle-format=false}"
COSIGN_VERIFY_FLAGS="${COSIGN_VERIFY_FLAGS:---insecure-ignore-tlog=true}"
http_flags=()
if [[ "${REGISTRY_HTTP:-}" == 1 ]]; then
    http_flags=(--allow-http-registry --allow-insecure-registry)
    export SYFT_REGISTRY_INSECURE_USE_HTTP=true
fi

need() { # variable what
    [[ -n "${!1:-}" ]] || { echo "Set $1 to $2, then run this again." >&2; exit 2; }
}

for ref in "$@"; do
    [[ "${ref}" == *@sha256:* ]] || { echo "Name ${ref} by its digest, as repository@sha256:..., so the signature covers exactly that image." >&2; exit 2; }
done

sbom_of() { # ref
    local name="${1%@*}"
    echo "${SBOM_DIR}/${name##*/}.spdx.json"
}

case "${action}" in
sbom)
    mkdir -p "${SBOM_DIR}"
    for ref in "$@"; do
        syft scan -q "registry:${ref}" -o "spdx-json=$(sbom_of "${ref}")"
        echo "sbom    ${ref} -> $(sbom_of "${ref}")"
    done
    ;;
sign)
    need COSIGN_KEY "the cosign private key's path"
    for ref in "$@"; do
        # shellcheck disable=SC2086 # the flags are words on purpose
        cosign sign --yes --key "${COSIGN_KEY}" ${COSIGN_SIGN_FLAGS} "${http_flags[@]}" "${ref}" 2>/dev/null
        echo "signed  ${ref}"
    done
    ;;
attest)
    need COSIGN_KEY "the cosign private key's path"
    for ref in "$@"; do
        sbom="$(sbom_of "${ref}")"
        [[ -f "${sbom}" ]] || { echo "No SBOM for ${ref} at ${sbom}; run make sbom first." >&2; exit 1; }
        # shellcheck disable=SC2086
        cosign attest --yes --key "${COSIGN_KEY}" --type spdxjson --predicate "${sbom}" ${COSIGN_SIGN_FLAGS} \
            "${http_flags[@]}" "${ref}" 2>/dev/null
        echo "attests ${ref}"
    done
    ;;
verify)
    need COSIGN_PUB "the cosign public key's path"
    for ref in "$@"; do
        # shellcheck disable=SC2086
        cosign verify --key "${COSIGN_PUB}" ${COSIGN_VERIFY_FLAGS} "${http_flags[@]}" "${ref}" >/dev/null 2>&1 ||
            { echo "${ref} carries no signature by ${COSIGN_PUB}." >&2; exit 1; }
        # shellcheck disable=SC2086
        cosign verify-attestation --key "${COSIGN_PUB}" --type spdxjson ${COSIGN_VERIFY_FLAGS} "${http_flags[@]}" \
            "${ref}" >/dev/null 2>&1 || { echo "${ref} carries no SBOM signed by ${COSIGN_PUB}." >&2; exit 1; }
        echo "verified ${ref}"
    done
    ;;
*) usage ;;
esac
