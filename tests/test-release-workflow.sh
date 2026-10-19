#!/usr/bin/env bash
# Tests for .github/workflows/release.yml (#44), which runs only for a release: it pushes and signs the images the chart
# runs, under the chart's names, the packed chart has the version, and every step is there. Adapted from Exciter's.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
WF="${SRC}/.github/workflows/release.yml"
COMPOSE="${SRC}/deploy/docker-compose.yml"
VALUES="${SRC}/deploy/charts/rotor/values.yaml"
SUPPLY="${SRC}/mk/supply.mk"

W="$(mktemp -d)" || exit 1
trap 'rm -rf "${W}"' EXIT

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

echo "== release.yml pushes every image the chart runs"
prefix=$(sed -n 's/^  IMAGE_PREFIX: *//p' "${WF}")
check "the images go to the project's ghcr namespace, in lower case" "${prefix}" "ghcr.io/cloudster1/rotor"
pushed=$(sed -n 's/^ *- image: *//p' "${WF}" | sort | paste -sd' ')
# An image whose tag and digest are empty runs at the chart's appVersion, which only a release publishes.
released=$(sed -n 's/^  \([a-z]*\): { repository: \([^,]*\), tag: "", digest: "" }$/\1 \2/p' "${VALUES}")
[[ -n "${released}" ]] || fail "values.yaml lists no image with an empty tag this test can read"
while read -r name repository; do
    [[ -n "${name}" ]] || continue
    check "the chart pulls ${name} from where release.yml pushes it" "${repository}" "${prefix}-${name}"
done <<<"${released}"
check "release.yml pushes exactly the images the chart runs at its appVersion" \
    "${pushed}" "$(cut -d' ' -f1 <<<"${released}" | sort | paste -sd' ')"
check "the release notes list the same images" \
    "$(sed -n 's/^ *for image in \(.*\); do$/\1/p' "${WF}" | tr ' ' '\n' | sort | paste -sd' ')" "${pushed}"

# The stack and the runner image are built from the same Dockerfiles, so a release ships what the gate tested.
dockerfiles=$(sed -n 's,^ *dockerfile: \(deploy/Dockerfile\..*\),\1,p' "${WF}" | sort -u)
while read -r built; do
    [[ -n "${built}" ]] || continue
    grep -qxF "${built}" <<<"${dockerfiles}" && pass "release.yml builds ${built}, as the stack does" \
        || fail "docker-compose.yml builds ${built}, which release.yml does not push"
done < <(sed -n 's,^ *dockerfile: \(deploy/Dockerfile\..*\),\1,p' "${COMPOSE}" | sort -u)
runner_dockerfile=$(sed -n 's,.*docker build .* -f $(ROOT)/\(deploy/Dockerfile\.[a-z]*\) .*,\1,p' "${SRC}/mk/python.mk")
grep -qxF "${runner_dockerfile}" <<<"${dockerfiles}" && pass "release.yml builds the runner as make runner-image does" \
    || fail "make runner-image builds ${runner_dockerfile:-nothing this test can read}, which release.yml does not push"
while read -r dockerfile; do
    [[ -f "${SRC}/${dockerfile}" ]] && pass "${dockerfile} exists" || fail "release.yml builds ${dockerfile}, which does not exist"
done <<<"${dockerfiles}"

echo "== release.yml runs only actions pinned to a commit"
unpinned=$(grep -E '^ *(- )?uses: ' "${WF}" | grep -vE 'uses: [^@ ]+@[0-9a-f]{40} # v[0-9.]+$' || true)
check "every action is pinned to a commit, its version beside it" "${unpinned}" ""

echo "== release.yml covers every step of a release"
has() { grep -qF -- "$2" "${WF}" && pass "$1" || fail "$1: release.yml no longer contains: $2"; }
has "a merged release pull request starts it" "types: [closed]"
has "only a release branch is tagged" "startsWith(github.event.pull_request.head.ref, 'release/v')"
has "the tag goes on the merge commit" 'scripts/release-tag.sh "$BRANCH" "$MERGE_COMMIT"'
has "a tag pushed by hand starts it" 'tags: ["v*"]'
has "the version files must agree with the tag" 'scripts/version-files.sh check "$version"'
has "the build stamp must say the tag's version" 'stamped=$(scripts/build-stamp.sh version)'
has "the images carry the version" 'VERSION=${{ needs.resolve.outputs.version }}'
has "the images carry the commit" 'COMMIT=${{ needs.resolve.outputs.commit }}'
has "the images are tagged with the version" ':${{ needs.resolve.outputs.version }}'
has "the images are tagged with the commit" ':${{ needs.resolve.outputs.commit }}'
has "the images are built for amd64" "platforms: linux/amd64"
has "the job's identity signs" "id-token: write"
has "each image is signed and its SBOM attested by digest, then checked" \
    'make sbom sign attest verify IMAGES="$IMAGE" COSIGN_KEYLESS=1'
has "the image is named by the digest it was pushed as" '@${{ steps.build.outputs.digest }}'
has "the chart is packaged" "make chart-package"
has "the chart is pushed" "make chart-push"
has "the chart is signed by its digest" 'make sign IMAGES="$chart" COSIGN_KEYLESS=1'
has "the chart goes to an OCI repository" "CHART_REPOSITORY: oci://"
has "the chart and the SBOMs are attached to the release" 'gh release upload "$TAG" "dist/rotor-$VERSION.tgz" dist/*.spdx.json'
has "the release notes come from the tag message" "%(contents)"
has "the digests follow the notes" "cat digests.md >> notes.md"
has "Dynamo is told which tool was released" 'tool: "rotor"'
has "Dynamo hears it as tool-released" 'event_type: "tool-released"'
has "a missing Dynamo token is a notice, not a failure" "::notice::DYNAMO_DISPATCH_TOKEN is not set"

echo "== what the release signs with is what verify and the chart trust"
identity=$(sed -n 's/^COSIGN_IDENTITY ?= //p' "${SUPPLY}")
check "the identity is this workflow at a version tag" "${identity}" \
    "^https://github.com/Cloudster1/Rotor/.github/workflows/release.yml@refs/tags/v"
check "the issuer is GitHub's" "$(sed -n 's/^COSIGN_ISSUER ?= //p' "${SUPPLY}")" "https://token.actions.githubusercontent.com"

echo "== the packaged chart carries the release version"
version=$(tr -d '[:space:]' <"${SRC}/VERSION")
if out=$(make -s -C "${SRC}" chart-package CHART_DIST="${W}" 2>&1); then
    pass "make chart-package packs the chart"
    chart_yaml=$(tar -xzOf "${W}/rotor-${version}.tgz" rotor/Chart.yaml 2>/dev/null)
    check "the package is named for VERSION" "$([[ -n "${chart_yaml}" ]] && echo found)" "found"
    check "its version is VERSION" "$(sed -n 's/^version: //p' <<<"${chart_yaml}")" "${version}"
    check "its appVersion is VERSION" "$(sed -n 's/^appVersion: //p' <<<"${chart_yaml}" | tr -d '"')" "${version}"
else
    fail "make chart-package failed"
    sed 's/^/      /' <<<"${out}"
fi

if (( FAILED )); then
    echo "release workflow tests failed"
    exit 1
fi
echo "release workflow tests passed"
