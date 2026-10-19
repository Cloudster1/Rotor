#!/usr/bin/env bash
# Tests for release.sh (#44): the dry run, what stands in the way of a release, the version files that must agree
# and commit messages that must never run; tests/test-release.sh tests what a release writes.

# shellcheck source=tests/release-fixture.sh
. "$(dirname "$0")/release-fixture.sh"

# --- Refusals and the dry run ---

echo "== release.sh: --dry-run changes nothing"
use_fixture tagged
commit "feat: add a reminder setting"
BEFORE_TREE="$(tree_state)"
BEFORE_TAGS="$(git -C "${REPO}" tag -l)"
BEFORE_HEAD="$(git -C "${REPO}" rev-parse HEAD)"
expect_release --dry-run && {
    check "the dry run leaves the working tree clean" "$(tree_state)" "${BEFORE_TREE}"
    check "the dry run creates no tag" "$(git -C "${REPO}" tag -l)" "${BEFORE_TAGS}"
    check "the dry run creates no commit" \
        "$(git -C "${REPO}" rev-parse HEAD)" "${BEFORE_HEAD}"
    if compgen -G "${REPO}/release-notes-*.md" > /dev/null; then
        fail "the dry run wrote the notes file"
    else
        pass "the dry run wrote no notes file"
    fi
    check "the dry run prints the version it would release" \
        "$(grep -c 'Dry run: Rotor v0.2.0' "${W}/out.log")" "1"
    check "the dry run prints the notes it would write" \
        "$(grep -c 'add a reminder setting' "${W}/out.log")" "1"
    check "the dry run names the branch it would open" \
        "$(grep -c 'Branch:    release/v0.2.0' "${W}/out.log")" "1"
    check "the dry run names every file it would write" \
        "$(grep -cF "Files:     ${CARRIERS} CHANGELOG.md" "${W}/out.log")" "1"
    check "the dry run creates no branch" "$(git -C "${REPO}" branch --list 'release/*')" ""
    check "the dry run opens no pull request" "$(cat "${GH_LOG}" 2>/dev/null)" ""
}

echo "== release.sh: what stands in the way of a release is refused"
use_fixture tagged
commit "feat: add a reminder setting"
printf 'local edit\n' >> "${REPO}/CHANGELOG.md"
if run_release; then
    fail "a release swept an uncommitted edit along"
else
    pass "a working tree with uncommitted changes is refused"
fi
check "the dirty refusal created no branch" "$(git -C "${REPO}" branch --list 'release/*')" ""
git -C "${REPO}" checkout -q -- CHANGELOG.md

git -C "${REPO}" push -q origin main:release/v0.2.0
if run_release; then
    fail "a release reused a release branch that exists"
else
    pass "an existing release branch is refused"
fi
check "the refusal names the branch" "$(grep -c 'release/v0.2.0 already exists' "${W}/out.log")" "1"
git -C "${REPO}" push -q origin --delete release/v0.2.0

if run_release 0.1.0; then
    fail "a version that was already tagged was released again"
else
    pass "a version that was already tagged is refused"
fi
check "the refusal names the tag" "$(grep -c 'v0.1.0 already exists' "${W}/out.log")" "1"
check "the tag refusal changed nothing" "$(tree_state)$(git -C "${REPO}" branch --list 'release/*')" ""

echo "== release.sh: without gh the release still lands on its branch"
use_fixture tagged
commit "fix: something"
if GH_FAIL=1 run_release; then
    pass "a pull request that cannot be opened does not fail the release"
else
    fail "a pull request that cannot be opened failed the release"
    sed 's/^/      /' "${W}/out.log"
fi
check "the release branch is pushed anyway" \
    "$(git -C "${REPO}.remote" rev-parse --verify -q release/v0.1.1 >/dev/null && echo pushed)" "pushed"
check "the command to open it by hand is printed" \
    "$(grep -c 'gh pr create --base main --head release/v0.1.1' "${W}/out.log")" "1"

echo "== version-files.sh: the files that carry the version must agree"
check "this repository's version files agree" \
    "$("${SRC}/scripts/version-files.sh" check 2>&1 && echo agree)" "agree"
use_fixture tagged
sed -i 's/^appVersion: .*/appVersion: "0.1.1"/' "${REPO}/deploy/charts/rotor/Chart.yaml"
check "a chart appVersion that disagrees is refused" \
    "$("${REPO}/scripts/version-files.sh" check 2>&1 | grep -c 'Chart.yaml appVersion says 0.1.1')" "1"
use_fixture tagged
sed -i 's/"version": "0.1.0"/"version": "0.0.9"/' "${REPO}/web/package.json"
check "a package.json that disagrees is refused" \
    "$("${REPO}/scripts/version-files.sh" check 2>&1 | grep -c 'web/package.json says 0.0.9')" "1"
use_fixture tagged
sed -i '/^name = "rotor-runner"$/{n;s/0.1.0/0.0.8/}' "${REPO}/runner/uv.lock"
check "a runner lock entry that disagrees is refused" \
    "$("${REPO}/scripts/version-files.sh" check 2>&1 | grep -c 'runner/uv.lock rotor-runner says 0.0.8')" "1"
check "a version that is no semver is never written" \
    "$("${REPO}/scripts/version-files.sh" set 1.2 >/dev/null 2>&1 && echo written || echo refused)$(cat "${REPO}/VERSION")" "refused0.1.0"
use_fixture tagged
rm "${REPO}/deploy/charts/rotor/Chart.yaml"
check "without a chart the others still carry the version" \
    "$("${REPO}/scripts/version-files.sh" files | paste -sd' ')" "VERSION runner/pyproject.toml runner/uv.lock web/package.json"
check "and a release writes them" \
    "$("${REPO}/scripts/version-files.sh" set 0.1.1 >/dev/null 2>&1 && "${REPO}/scripts/version-files.sh" show | cut -f2 | paste -sd' ')" \
    "0.1.1 0.1.1 0.1.1 0.1.1"

echo "== release.sh: a checkout behind its remote is refused"
use_fixture tagged
commit "feat: a commit the remote has"
git -C "${REPO}" reset -q --hard HEAD~1
BEFORE="$(tree_state)$(git -C "${REPO}" tag -l)"
if run_release; then
    fail "a release from a stale checkout was cut"
else
    pass "a release from a stale checkout is refused"
fi
check "the refusal says how far behind the checkout is" \
    "$(grep -c 'commit(s) behind origin/main' "${W}/out.log")" "1"
check "the refused run changed nothing" "$(tree_state)$(git -C "${REPO}" tag -l)" "${BEFORE}"

echo "== release.sh: a release off the default branch is refused"
use_fixture tagged
commit "fix: something"
git -C "${REPO}" switch -q -c fix/12-something
if run_release; then
    fail "a release from a feature branch was cut"
else
    pass "a release from a feature branch is refused"
fi
check "the refusal names the branch releases come from" \
    "$(grep -c 'releases are cut from' "${W}/out.log")" "1"

# --- Untrusted input ---

# Commit subjects are attacker-shaped input: they reach the version parsing and the notes through the
# environment, never interpolated into a command line or a Python source string.
echo "== release.sh: a hostile commit message executes nothing"
use_fixture tagged
HOSTILE='feat: "quoted" and `backticks` and $(touch '"${W}"'/pwned-sub) and ${W}'
commit "${HOSTILE}" "'); import os; os.system('touch ${W}/pwned-py'); ('

a body line, then another"
commit "fix: a commit with a hostile trailer" \
    "BREAKING CHANGE: \"quoted\" and \$(touch ${W}/pwned-body)"
expect_release && {
    NOTES="${REPO}/release-notes-v0.2.0.md"
    if compgen -G "${W}/pwned-*" > /dev/null; then
        fail "a commit message was executed: $(echo "${W}"/pwned-*)"
    else
        pass "a commit message is never executed"
    fi
    check "the subject survives quoting intact" \
        "$(grep -cF '"quoted" and `backticks`' "${NOTES}")" "1"
    check "the trailer survives quoting intact" \
        "$(grep -cF '"quoted" and $(touch' "${NOTES}")" "1"
    check "the trailer still makes a breaking change" \
        "$(grep -m1 '^### ' "${NOTES}")" "### Breaking changes"
    check "the release still lands" "$(released_version)" "0.2.0"
}

finish "release.sh refusal"
