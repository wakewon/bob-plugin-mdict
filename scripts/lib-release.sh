#!/bin/bash

# Shared release helpers. This file never mutates repository state by itself.

release_repo_root() {
    cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd
}

release_version() {
    tr -d ' \t\n\r' < VERSION
}

release_require_semver() {
    local value="$1"
    if ! printf '%s' "$value" | grep -qE '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'; then
        echo "error: invalid semantic version: $value" >&2
        return 1
    fi
}

release_build_commit() {
    if [ -n "${BUILD_COMMIT:-}" ]; then
        printf '%s\n' "$BUILD_COMMIT"
        return
    fi
    local commit
    commit=$(git rev-parse --short HEAD 2>/dev/null || printf 'unknown')
    if [ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]; then
        commit="$commit-dirty"
    fi
    printf '%s\n' "$commit"
}

release_public_assets() {
    local version="$1"
    printf '%s\n' \
        "MDict-v$version.bobplugin" \
        "bob-mdict-$version-darwin-arm64.tar.gz" \
        "bob-mdict-$version-darwin-amd64.tar.gz" \
        "bob-mdict-$version-macos-installer.tar.gz" \
        "bob-mdict.rb" \
        "RELEASE_MANIFEST.json"
}
