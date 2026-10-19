#!/usr/bin/env bash
# Tests for release.sh and scripts/release-tag.sh (#44): the version it works out, the notes, the files, branch and
# pull request it makes and the tag on the merge; tests/test-release-refusals.sh tests the rest.

# shellcheck source=tests/release-fixture.sh
. "$(dirname "$0")/release-fixture.sh"

# --- Version derivation ---

echo "== release.sh: the next version is inferred from the commits"

use_fixture tagged
commit "fix: repair one thing"
commit "fix: repair another thing"
expect_release && {
    check "a fix-only history bumps the patch" "$(released_version)" "0.1.1"
    check "the deciding rule is printed" \
        "$(grep -c 'a fix -> PATCH' "${W}/out.log")" "1"
    check "the previous version is printed" \
        "$(grep -c 'Previous version: 0.1.0' "${W}/out.log")" "1"
    check "the counts per type are printed" \
        "$(grep -c 'Commits since:    2 (fix: 2)' "${W}/out.log")" "1"
}

use_fixture tagged
commit "fix: repair one thing"
commit "feat: add a reminder setting"
expect_release && {
    check "a feat bumps the minor" "$(released_version)" "0.2.0"
    check "the feat rule is printed" \
        "$(grep -c 'a feat -> MINOR' "${W}/out.log")" "1"
}

use_fixture tagged
commit "feat!: drop the old catalog format"
expect_release && {
    check "a breaking change stays below 1.0.0" "$(released_version)" "0.2.0"
    check "the degraded breaking rule is printed" \
        "$(grep -c 'below 1.0.0 -> MINOR' "${W}/out.log")" "1"
}

use_fixture tagged
commit "refactor: split the schedule engine" "BREAKING CHANGE: stored schedules are rebuilt"
expect_release && \
    check "a BREAKING CHANGE trailer counts as breaking" "$(released_version)" "0.2.0"

use_fixture tagged
for type in chore docs test build ci refactor style perf; do
    commit "${type}: housekeeping for ${type}"
done
expect_release && {
    check "housekeeping alone bumps the patch" "$(released_version)" "0.1.1"
    check "housekeeping alone is called out" \
        "$(grep -c 'nothing user-facing -> PATCH' "${W}/out.log")" "1"
}

use_fixture untagged
commit "feat: add another feature"
expect_release && {
    check "the first release keeps the version in VERSION" \
        "$(released_version)" "0.1.0"
    check "the first-release rule is printed" \
        "$(grep -c 'nothing released yet' "${W}/out.log")" "1"
}

echo "== release.sh: an empty history aborts without touching anything"
use_fixture tagged
BEFORE="$(tree_state)$(git -C "${REPO}" tag -l)"
if run_release; then
    fail "an empty history was released"
else
    pass "an empty history is rejected"
fi
check "the empty history changed nothing" "$(tree_state)$(git -C "${REPO}" tag -l)" "${BEFORE}"
check "the abort names the tag it looked at" \
    "$(grep -c 'No commits since v0.1.0' "${W}/out.log")" "1"

echo "== release.sh: a version given by hand wins over the inferred one"
use_fixture tagged
commit "feat: would infer a minor bump"
expect_release 0.4.2 && {
    check "the given version is used verbatim" "$(released_version)" "0.4.2"
    check "nothing is inferred when a version is given" \
        "$(grep -c 'Working out the next version' "${W}/out.log")" "0"
}

echo "== release.sh: a malformed version is rejected before anything is written"
use_fixture tagged
commit "fix: something"
BEFORE="$(tree_state)$(git -C "${REPO}" tag -l)"
if run_release "0.2.0-rc1"; then
    fail "a malformed version was accepted"
else
    pass "a malformed version is rejected"
fi
check "the rejected run changed nothing" "$(tree_state)$(git -C "${REPO}" tag -l)" "${BEFORE}"

# --- What the release writes ---

echo "== release.sh: version files, changelog, notes, branch and pull request"
use_fixture tagged
commit "feat: add a reminder setting"
commit "fix: stop the timeline flickering (#42)"
commit "chore: bump a dependency"
commit "Update the README by hand"
commit "feat!: drop the old catalog format"
# GitHub reads a skip marker anywhere in the head commit message of a push, so quoting such a subject in
# the notes would skip the release build.
commit "chore: record the catalog source texts as of 2026-09-23 [skip ci]"
expect_release && {
    check "VERSION carries the new version" "$(released_version)" "0.2.0"

    NOTES="${REPO}/release-notes-v0.2.0.md"
    if [[ -f "${NOTES}" ]]; then
        pass "the release notes file is written"
    else
        fail "the release notes file is missing"
    fi

    section_of() { # item -> heading it landed under
        python3 - "${NOTES}" "$1" <<'EOF'
import sys

heading = ""
with open(sys.argv[1]) as f:
    for line in f:
        line = line.rstrip("\n")
        if line.startswith("### "):
            heading = line[4:]
        elif sys.argv[2] in line:
            print(heading, end="")
            break
    else:
        print("<missing>", end="")
EOF
    }

    check "a feature lands under Features" \
        "$(section_of 'add a reminder setting')" "Features"
    check "a fix lands under Bug fixes" \
        "$(section_of 'stop the timeline flickering')" "Bug fixes"
    check "a chore lands under Other" "$(section_of 'bump a dependency')" "Other"
    check "a non-conforming subject is not lost" \
        "$(section_of 'Update the README by hand')" "Other"
    check "an exclamation mark makes a breaking change" \
        "$(section_of 'drop the old catalog format')" "Breaking changes"
    check "breaking changes come first" \
        "$(grep -m1 '^### ' "${NOTES}")" "### Breaking changes"
    check "issue references are kept" \
        "$(grep -c 'stop the timeline flickering (#42)' "${NOTES}")" "1"

    check "a quoted skip-ci marker does not reach the notes" \
        "$(grep -c 'skip ci' "${NOTES}")" "0"
    check "the commit that carried it is still listed" \
        "$(section_of 'record the catalog source texts')" "Other"
    check "no skip-ci marker reaches the release commit" \
        "$(cd "${REPO}" && git log -1 --format='%B' | grep -ci 'skip ci')" "0"

    check "the changelog carries the released version and date" \
        "$(grep -m1 "^## \[0" "${REPO}/CHANGELOG.md")" \
        "## [0.2.0] - $(date -u +%Y-%m-%d)"
    check "an empty Unreleased section is kept for the next round" \
        "$(grep -c '^## \[Unreleased\]$' "${REPO}/CHANGELOG.md")" "1"
    check "the changelog header is not repeated" \
        "$(grep -c '^# Changelog$' "${REPO}/CHANGELOG.md")" "1"

    check "the chart's version moves with VERSION" \
        "$(sed -n 's/^version: //p' "${REPO}/deploy/charts/rotor/Chart.yaml")" "0.2.0"
    check "the chart's appVersion moves with VERSION" \
        "$(sed -n 's/^appVersion: //p' "${REPO}/deploy/charts/rotor/Chart.yaml")" '"0.2.0"'
    check "a package.json that carries a version moves with it" \
        "$(grep -c '"version": "0.2.0"' "${REPO}/web/package.json")" "1"
    check "the runner's project version moves with it" \
        "$(sed -n '/^\[project\]/,/^\[/p' "${REPO}/runner/pyproject.toml" | grep -c '^version = "0.2.0"$')" "1"
    check "another table's version in pyproject.toml is left alone" \
        "$(grep -c '^version = "9.9.9"$' "${REPO}/runner/pyproject.toml")" "1"
    check "the runner's entry in uv.lock moves with it" \
        "$(grep -A1 '^name = "rotor-runner"$' "${REPO}/runner/uv.lock" | grep -c '^version = "0.2.0"$')" "1"
    check "a dependency's version in uv.lock is left alone" \
        "$(grep -A1 '^name = "langgraph"$' "${REPO}/runner/uv.lock" | grep -c '^version = "1.0.0"$')" "1"
    check "the version files agree afterwards" \
        "$("${REPO}/scripts/version-files.sh" check 0.2.0 2>&1 && echo agree)" "agree"

    check "the release travels on a branch of its own" \
        "$(git -C "${REPO}" branch --show-current)" "release/v0.2.0"
    check "the release commit subject stays one short line" \
        "$(git -C "${REPO}" log -1 --format=%s)" "release: v0.2.0"
    check "only the release files are committed" \
        "$(git -C "${REPO}" show --name-only --format= HEAD | LC_ALL=C sort | tr '\n' ' ')" \
        "${COMMITTED}"
    check "the working tree is left clean" "$(tree_state)" ""

    check "release.sh sets no tag; the merge commit gets it" \
        "$(git -C "${REPO}" tag -l 'v0.2.*')$(git -C "${REPO}.remote" tag -l 'v0.2.*')" ""
    check "the release branch is pushed" \
        "$(git -C "${REPO}.remote" rev-parse --verify -q release/v0.2.0)" "$(git -C "${REPO}" rev-parse HEAD)"
    check "main on the remote is left alone" \
        "$(git -C "${REPO}.remote" rev-parse main)" "$(git -C "${REPO}" rev-parse origin/main)"
    check "the release commit sits on top of main" \
        "$(git -C "${REPO}" rev-list --count 'origin/main..HEAD')" "1"

    check "a pull request into main is opened" \
        "$(cut -d' ' -f1-9 "${GH_LOG}" 2>/dev/null)" \
        "pr create --base main --head release/v0.2.0 --title release: v0.2.0"
    check "the pull request carries the notes" \
        "$(grep -c 'add a reminder setting' "${GH_LOG}.body" 2>/dev/null)" "1"
    check "the notes are printed for pasting" \
        "$(grep -c 'add a reminder setting' "${W}/out.log")" "1"

    if [[ -d "${REPO}/dist" || -d "${REPO}/build" ]]; then
        fail "release.sh built an artifact"
    else
        pass "release.sh builds no artifact"
    fi
}

# What the release workflow does once the pull request is merged; the merge
# is done here by hand, as GitHub would.
echo "== release-tag.sh: the merge commit becomes the release"
RELEASE_COMMIT="$(git -C "${REPO}" rev-parse HEAD)"
git -C "${REPO}" switch -q main
check "the release commit is not on main before the merge" \
    "$(cd "${REPO}" && scripts/release-tag.sh release/v0.2.0 "${RELEASE_COMMIT}" >/dev/null 2>&1 && echo tagged || echo refused)" "refused"
git -C "${REPO}" merge -q --no-ff -m "Merge pull request #999 from Cloudster1/release/v0.2.0" release/v0.2.0
git -C "${REPO}" push -q origin main
MERGE_COMMIT="$(git -C "${REPO}" rev-parse HEAD)"
if (cd "${REPO}" && scripts/release-tag.sh release/v0.2.0 "${MERGE_COMMIT}") > "${W}/tag.log" 2>&1; then
    pass "the merge commit is tagged"
else
    fail "the merge commit was not tagged"
    sed 's/^/      /' "${W}/tag.log"
fi
check "the tag on the remote points at the merge commit" \
    "$(git -C "${REPO}.remote" rev-parse -q --verify 'v0.2.0^{commit}')" "${MERGE_COMMIT}"
check "the tag is annotated" "$(git -C "${REPO}.remote" cat-file -t v0.2.0)" "tag"
check "the tag message is the release's changelog section" \
    "$(git -C "${REPO}.remote" tag -l --format='%(contents)' v0.2.0 | grep -c 'add a reminder setting')" "1"
check "the tag message carries no other release" \
    "$(git -C "${REPO}.remote" tag -l --format='%(contents)' v0.2.0 | grep -c '^## ')" "0"
if (cd "${REPO}" && scripts/release-tag.sh release/v0.2.0 "${MERGE_COMMIT}") > "${W}/tag.log" 2>&1; then
    pass "tagging the same merge again is a no-op, so the workflow can be re-run"
else
    fail "tagging the same merge again failed"
    sed 's/^/      /' "${W}/tag.log"
fi
check "a non-release branch is refused" \
    "$(cd "${REPO}" && scripts/release-tag.sh fix/12-something HEAD >/dev/null 2>&1 && echo tagged || echo refused)" "refused"
check "a tag is never moved to another commit" \
    "$(cd "${REPO}" && git commit -q --allow-empty -m 'chore: later' && git push -q origin main && scripts/release-tag.sh release/v0.2.0 HEAD >/dev/null 2>&1 && echo moved || echo refused)" "refused"
git -C "${REPO}" reset -q --hard "${MERGE_COMMIT}"
git -C "${REPO}" push -q -f origin main
check "a merge whose files say another version is refused" \
    "$(cd "${REPO}" && scripts/release-tag.sh release/v0.3.0 HEAD 2>&1 | grep -c 'disagree with 0.3.0')" "1"

echo "== release.sh: a second release prepends to CHANGELOG.md"
commit "fix: repair the second thing"
expect_release && {
    check "the earlier section is still there" \
        "$(grep -c '^## \[0.2.0\]' "${REPO}/CHANGELOG.md")" "1"
    check "the newest section is on top" \
        "$(grep -m1 '^## \[0' "${REPO}/CHANGELOG.md")" \
        "## [0.2.1] - $(date -u +%Y-%m-%d)"
    check "the header is not repeated" \
        "$(grep -c '^# Changelog$' "${REPO}/CHANGELOG.md")" "1"
    check "the previous tag bounds the history" \
        "$(grep -c 'add a reminder setting' "${REPO}/release-notes-v0.2.1.md")" "0"
    check "the version keeps climbing" "$(released_version)" "0.2.1"
}

echo "== release.sh: a hand-written Unreleased section becomes the release"
use_fixture tagged
commit "feat: add something the changelog already describes"
printf '\n### Features\n\n- A sentence written by hand\n' >> "${REPO}/CHANGELOG.md"
git -C "${REPO}" commit -qam "docs: fill in the changelog"
git -C "${REPO}" push -q origin main
expect_release && {
    check "the hand-written entry is released" \
        "$(grep -c 'A sentence written by hand' "${REPO}/release-notes-v0.2.0.md")" "1"
    check "the commit subjects do not duplicate it" \
        "$(grep -c 'add something the changelog already describes' "${REPO}/release-notes-v0.2.0.md")" "0"
    check "the Unreleased section is emptied" \
        "$(sed -n '/^## \[Unreleased\]$/,/^## \[0/p' "${REPO}/CHANGELOG.md" | grep -c '^- ')" "0"
}

echo "== release.sh: a changelog argument overrides everything generated"
use_fixture tagged
commit "feat: add something generated"
expect_release 0.2.0 "Handwritten note" && {
    check "the manual changelog reaches the notes" \
        "$(cat "${REPO}/release-notes-v0.2.0.md")" "Handwritten note"
    check "the generated notes are not used" \
        "$(grep -c 'add something generated' "${REPO}/CHANGELOG.md")" "0"
}

finish release.sh
