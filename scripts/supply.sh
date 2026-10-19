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

COSIGN_KEYLESS=1 signs with the CI job's own identity instead of a key, and verify then
checks for $COSIGN_IDENTITY (a regular expression) issued by $COSIGN_ISSUER.
REGISTRY_USER and REGISTRY_TOKEN sign in to each image's registry first.
REGISTRY_HTTP=1 talks plain HTTP to the registry, as a local one serves.
EOF
    exit 2
}

[[ $# -ge 2 ]] || usage
action="$1"
shift

SBOM_DIR="${SBOM_DIR:-reports/sbom}"
# Signatures stay tags beside the image, the format policy-controller reads. A key keeps them out of the public log;
# a keyless one (the release, #44) needs the log, which vouches for its short-lived certificate after it expired.
LEGACY_FLAGS="--use-signing-config=false --new-bundle-format=false"
keyless=""
[[ "${COSIGN_KEYLESS:-}" == 1 ]] && keyless=1
if [[ -n "${keyless}" ]]; then
    COSIGN_SIGN_FLAGS="${COSIGN_SIGN_FLAGS:-${LEGACY_FLAGS}}"
    COSIGN_VERIFY_FLAGS="${COSIGN_VERIFY_FLAGS:-}"
else
    COSIGN_SIGN_FLAGS="${COSIGN_SIGN_FLAGS:---tlog-upload=false ${LEGACY_FLAGS}}"
    COSIGN_VERIFY_FLAGS="${COSIGN_VERIFY_FLAGS:---insecure-ignore-tlog=true}"
fi
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

# Who signs: the key, or with no flag at all the CI job's identity.
signer=()
signing() {
    if [[ -n "${keyless}" ]]; then
        signer=()
    else
        need COSIGN_KEY "the cosign private key's path"
        signer=(--key "${COSIGN_KEY}")
    fi
}
# Whom a signature must come from: the key's owner, or the CI identity and the issuer that vouched for it.
trusted() {
    if [[ -n "${keyless}" ]]; then
        need COSIGN_IDENTITY "the signer's identity as a regular expression, such as the release workflow's URL"
        need COSIGN_ISSUER "the OIDC issuer that identity comes from"
        signer=(--certificate-identity-regexp "${COSIGN_IDENTITY}" --certificate-oidc-issuer "${COSIGN_ISSUER}")
    else
        need COSIGN_PUB "the cosign public key's path"
        signer=(--key "${COSIGN_PUB}")
    fi
}
whose() { if [[ -n "${keyless}" ]]; then echo "${COSIGN_IDENTITY}"; else echo "${COSIGN_PUB}"; fi; }

# cosign and syft both read the credentials cosign login leaves in $HOME/.docker, one login per registry.
if [[ -n "${REGISTRY_USER:-}" && -n "${REGISTRY_TOKEN:-}" ]]; then
    for host in $(printf '%s\n' "$@" | cut -d/ -f1 | sort -u); do
        out=$(printf %s "${REGISTRY_TOKEN}" | cosign login "${host}" -u "${REGISTRY_USER}" --password-stdin 2>&1) ||
            { echo "${out}" >&2; echo "Signing in to ${host} as ${REGISTRY_USER} failed; check REGISTRY_TOKEN." >&2; exit 1; }
    done
fi

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
    signing
    for ref in "$@"; do
        # shellcheck disable=SC2086 # the flags are words on purpose
        cosign sign --yes "${signer[@]}" ${COSIGN_SIGN_FLAGS} "${http_flags[@]}" "${ref}" 2>/dev/null
        echo "signed  ${ref}"
    done
    ;;
attest)
    signing
    for ref in "$@"; do
        sbom="$(sbom_of "${ref}")"
        [[ -f "${sbom}" ]] || { echo "No SBOM for ${ref} at ${sbom}; run make sbom first." >&2; exit 1; }
        # shellcheck disable=SC2086
        cosign attest --yes "${signer[@]}" --type spdxjson --predicate "${sbom}" ${COSIGN_SIGN_FLAGS} \
            "${http_flags[@]}" "${ref}" 2>/dev/null
        echo "attests ${ref}"
    done
    ;;
verify)
    trusted
    for ref in "$@"; do
        # shellcheck disable=SC2086
        cosign verify "${signer[@]}" ${COSIGN_VERIFY_FLAGS} "${http_flags[@]}" "${ref}" >/dev/null 2>&1 ||
            { echo "${ref} carries no signature by $(whose)." >&2; exit 1; }
        # shellcheck disable=SC2086
        cosign verify-attestation "${signer[@]}" --type spdxjson ${COSIGN_VERIFY_FLAGS} "${http_flags[@]}" \
            "${ref}" >/dev/null 2>&1 || { echo "${ref} carries no SBOM signed by $(whose)." >&2; exit 1; }
        echo "verified ${ref}"
    done
    ;;
*) usage ;;
esac
