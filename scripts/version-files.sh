#!/usr/bin/env bash
# The one list of the files that carry the release version, so release.sh, the release workflow and the tests
# agree on it. Usage: version-files.sh show | check [X.Y.Z] | set X.Y.Z | files
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
# The chart is versioned with the application, so chart X.Y.Z installs Rotor X.Y.Z; a checkout without it skips it.
CHART="deploy/charts/rotor/Chart.yaml"
# The runner is a Python project of its own, and uv.lock repeats its version, so the two move together.
RUNNER="runner/pyproject.toml"
RUNNER_LOCK="runner/uv.lock"
RUNNER_NAME="rotor-runner"
# A package.json carries a version only if somebody gave it one; once it does, it has to move with the others.
PACKAGES=(web/package.json)
SEMVER_RE='^[0-9]+\.[0-9]+\.[0-9]+$'

has() { [[ -f "$root/$1" ]]; }
has_version() { has "$1" && grep -q '^  "version":' "$root/$1"; }
chart_field() { sed -n "s/^$1: *\"\{0,1\}\([^\"]*\)\"\{0,1\} *\$/\1/p" "$root/$CHART"; }
package_version() { sed -n 's/^  "version": *"\([^"]*\)",\{0,1\}$/\1/p' "$root/$1"; }
# The first version line of [project] in pyproject.toml, and the one after the runner's name in uv.lock.
runner_version() { sed -n '/^\[project\]/,/^\[/{s/^version = "\([^"]*\)"$/\1/p}' "$root/$RUNNER" | head -n1; }
lock_version() { sed -n "/^name = \"$RUNNER_NAME\"\$/{n;s/^version = \"\([^\"]*\)\"\$/\1/p}" "$root/$RUNNER_LOCK"; }

show() {
    printf 'VERSION\t%s\n' "$(tr -d '[:space:]' <"$root/VERSION")"
    if has "$CHART"; then
        printf '%s version\t%s\n' "$CHART" "$(chart_field version)"
        printf '%s appVersion\t%s\n' "$CHART" "$(chart_field appVersion)"
    fi
    if has "$RUNNER"; then printf '%s\t%s\n' "$RUNNER" "$(runner_version)"; fi
    if has "$RUNNER_LOCK"; then printf '%s %s\t%s\n' "$RUNNER_LOCK" "$RUNNER_NAME" "$(lock_version)"; fi
    local p
    for p in "${PACKAGES[@]}"; do
        if has_version "$p"; then printf '%s\t%s\n' "$p" "$(package_version "$p")"; fi
    done
    return 0
}

case "${1:-}" in
show)
    show
    ;;
check)
    want="${2:-}"
    [[ -n "$want" ]] || want=$(tr -d '[:space:]' <"$root/VERSION")
    bad=$(show | awk -F'\t' -v want="$want" '$2 != want { printf "   %s says %s\n", $1, ($2 == "" ? "nothing" : $2) }')
    if [[ -n "$bad" ]]; then
        echo "The release version files disagree with $want:" >&2
        echo "$bad" >&2
        echo "Run scripts/version-files.sh set $want, or release.sh, which writes them all." >&2
        exit 1
    fi
    ;;
set)
    want="${2:-}"
    [[ "$want" =~ $SEMVER_RE ]] || { echo "Give the version as X.Y.Z, digits only, not '$want'." >&2; exit 2; }
    printf '%s\n' "$want" >"$root/VERSION"
    if has "$CHART"; then
        sed -i -E "s/^version: .*/version: $want/; s/^appVersion: .*/appVersion: \"$want\"/" "$root/$CHART"
    fi
    if has "$RUNNER"; then
        sed -i -E "/^\[project\]/,/^\[/{s/^version = \"[^\"]*\"\$/version = \"$want\"/}" "$root/$RUNNER"
    fi
    if has "$RUNNER_LOCK"; then
        sed -i -E "/^name = \"$RUNNER_NAME\"\$/{n;s/^version = \"[^\"]*\"\$/version = \"$want\"/}" "$root/$RUNNER_LOCK"
    fi
    for p in "${PACKAGES[@]}"; do
        if has "$p"; then sed -i -E "s/^(  \"version\": *)\"[^\"]*\"/\1\"$want\"/" "$root/$p"; fi
    done
    "$0" check "$want"
    ;;
files)
    echo VERSION
    if has "$CHART"; then echo "$CHART"; fi
    if has "$RUNNER"; then echo "$RUNNER"; fi
    if has "$RUNNER_LOCK"; then echo "$RUNNER_LOCK"; fi
    for p in "${PACKAGES[@]}"; do
        if has_version "$p"; then echo "$p"; fi
    done
    ;;
*)
    echo "Say what to do: $0 show, $0 check [X.Y.Z], $0 set X.Y.Z or $0 files." >&2
    exit 2
    ;;
esac
