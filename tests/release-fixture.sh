#!/usr/bin/env bash
# What tests/test-release*.sh share: a throwaway repository with every file that carries Rotor's version, a bare
# remote, a stand-in gh and the helpers that run release.sh in it. Sourced, never run. Adapted from Exciter's.
set -u

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@example.com
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@example.com

# Background auto maintenance once still wrote into .git/objects while the next case removed the repository, and that
# case then ran in something that was no repository at all.
export GIT_CONFIG_COUNT=2
export GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=false
export GIT_CONFIG_KEY_1=gc.auto GIT_CONFIG_VALUE_1=0

# A broken fixture makes every later case fail for the wrong reason, so the script stops right after the command that
# broke it has printed its error.
fixture_failed() {
    echo "The test fixture broke while $1. The error above is the cause; fix it and run the script again." >&2
    exit 1
}

W="$(mktemp -d)" || fixture_failed "creating the temporary directory"
trap 'rm -rf "${W}"' EXIT

FAILED=0
pass() { echo "   ok    $1"; }
fail() { echo "   FAIL  $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

REPO="${W}/repo"
CHART="deploy/charts/rotor/Chart.yaml"
# Every file release.sh writes, in the order version-files.sh names them, and sorted as git lists them.
# shellcheck disable=SC2034 # read by the tests that source this
CARRIERS="VERSION ${CHART} runner/pyproject.toml runner/uv.lock web/package.json"
# shellcheck disable=SC2034
COMMITTED="CHANGELOG.md VERSION ${CHART} runner/pyproject.toml runner/uv.lock web/package.json "

# The runner's files hold other versions too, a tool's setting and a dependency's, which must stay as they are.
write_runner() { # dir
    cat > "$1/runner/pyproject.toml" <<'EOF' || fixture_failed "writing runner/pyproject.toml"
[project]
name = "rotor-runner"
version = "0.1.0"
dependencies = ["langgraph==1.0.0"]

[tool.example]
version = "9.9.9"
EOF
    cat > "$1/runner/uv.lock" <<'EOF' || fixture_failed "writing runner/uv.lock"
version = 1

[[package]]
name = "langgraph"
version = "1.0.0"

[[package]]
name = "rotor-runner"
version = "0.1.0"
source = { editable = "." }
EOF
}

# One prepared repository is copied per case, so each case can state the version it expects instead of tracking what
# its predecessors released.
build_pristine() { # target tagged
    local dir="$1" tagged="$2"
    rm -rf "${dir}" "${dir}.remote" || fixture_failed "removing ${dir}"
    mkdir -p "${dir}/scripts" "${dir}/deploy/charts/rotor" "${dir}/web" "${dir}/runner" || fixture_failed "creating ${dir}"
    cp "${SRC}/release.sh" "${dir}/release.sh" || fixture_failed "copying release.sh"
    cp "${SRC}/scripts/version-files.sh" "${SRC}/scripts/release-notes.sh" "${SRC}/scripts/release-tag.sh" "${dir}/scripts/" \
        || fixture_failed "copying the release scripts"
    printf '0.1.0\n' > "${dir}/VERSION" || fixture_failed "writing VERSION"
    printf 'apiVersion: v2\nname: rotor\nversion: 0.1.0\nappVersion: "0.1.0"\n' > "${dir}/${CHART}" \
        || fixture_failed "writing Chart.yaml"
    # The real web/package.json carries no version; this one does, so the test shows one that does is kept in step.
    printf '{\n  "name": "rotor-web",\n  "version": "0.1.0",\n  "private": true\n}\n' > "${dir}/web/package.json" \
        || fixture_failed "writing web/package.json"
    write_runner "${dir}"
    cat > "${dir}/CHANGELOG.md" <<'EOF' || fixture_failed "writing CHANGELOG.md"
# Changelog

All notable changes to this project are documented here.

## [Unreleased]
EOF
    printf 'release-notes-*.md\n' > "${dir}/.gitignore" || fixture_failed "writing .gitignore"
    git -C "${dir}" init -q -b main || fixture_failed "initialising ${dir}"
    git -C "${dir}" add -A || fixture_failed "staging the files in ${dir}"
    git -C "${dir}" commit -qm "feat: the first feature" || fixture_failed "committing in ${dir}"
    git init -q --bare "${dir}.remote" || fixture_failed "initialising ${dir}.remote"
    git -C "${dir}" remote add origin "${dir}.remote" || fixture_failed "adding the remote to ${dir}"
    git -C "${dir}" push -q -u origin main || fixture_failed "pushing ${dir} to its remote"
    git -C "${dir}" remote set-head origin main || fixture_failed "setting origin/HEAD in ${dir}"
    if [[ "${tagged}" == tagged ]]; then
        git -C "${dir}" tag -a v0.1.0 -m "v0.1.0" || fixture_failed "tagging v0.1.0 in ${dir}"
    fi
}

use_fixture() { # tagged|untagged
    rm -rf "${REPO}" "${REPO}.remote" || fixture_failed "removing the previous case's repository"
    cp -a "${W}/pristine-$1" "${REPO}" || fixture_failed "copying the $1 fixture"
    cp -a "${W}/pristine-$1.remote" "${REPO}.remote" || fixture_failed "copying the $1 fixture's remote"
    git -C "${REPO}" remote set-url origin "${REPO}.remote" || fixture_failed "pointing the copy at its remote"
}

commit() { # subject [body]
    local args=(-m "$1")
    (( $# > 1 )) && args+=(-m "$2")
    git -C "${REPO}" commit -q --allow-empty "${args[@]}"
    git -C "${REPO}" push -q origin main
}

# GitHub is the one thing these tests cannot reach, so gh is a stand-in that records what it was asked and keeps the
# body of the pull request.
mkdir -p "${W}/bin" || fixture_failed "creating the directory for the gh stand-in"
cat > "${W}/bin/gh" <<'EOF' || fixture_failed "writing the gh stand-in"
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${GH_LOG}"
[[ -n "${GH_FAIL:-}" ]] && { echo "gh: not signed in" >&2; exit 1; }
while (( $# )); do
    [[ "$1" == --body-file ]] && cp "$2" "${GH_LOG}.body"
    shift
done
echo "https://github.com/Cloudster1/Rotor/pull/999"
EOF
chmod +x "${W}/bin/gh" || fixture_failed "making the gh stand-in executable"
export GH_LOG="${W}/gh.log"

run_release() {
    rm -f "${GH_LOG}" "${GH_LOG}.body"
    (cd "${REPO}" && PATH="${W}/bin:${PATH}" ./release.sh "$@") > "${W}/out.log" 2>&1
}

released_version() { cat "${REPO}/VERSION"; }
tree_state() { git -C "${REPO}" status --porcelain; }

expect_release() { # args...
    if ! run_release "$@"; then
        fail "release.sh exited non-zero"
        sed 's/^/      /' "${W}/out.log"
        return 1
    fi
}

finish() { # name
    if (( FAILED )); then
        echo "$1 tests failed"
        exit 1
    fi
    echo "$1 tests passed"
}

build_pristine "${W}/pristine-tagged" tagged
build_pristine "${W}/pristine-untagged" untagged
