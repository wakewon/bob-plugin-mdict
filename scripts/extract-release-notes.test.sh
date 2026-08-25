#!/bin/bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELPER="$REPO_ROOT/scripts/extract-release-notes.sh"
TEMP=$(mktemp -d)
trap 'rm -rf "$TEMP"' EXIT

FIXTURE="$TEMP/CHANGELOG.md"
cat > "$FIXTURE" <<'EOF'
# Changelog

## [Unreleased]

- work in progress

## [1.0.1] - 2026-09-01

Only 1.0.1.

- security hardening

## [1.0.0] - 2026-08-25

Only 1.0.0.

- stable release

[1.0.1]: https://example.invalid/1.0.1
[1.0.0]: https://example.invalid/1.0.0
EOF

expected_101=$(printf 'Only 1.0.1.\n\n- security hardening')
expected_100=$(printf 'Only 1.0.0.\n\n- stable release')
[ "$($HELPER 1.0.1 "$FIXTURE")" = "$expected_101" ]
[ "$($HELPER 1.0.0 "$FIXTURE")" = "$expected_100" ]
if "$HELPER" 9.9.9 "$FIXTURE" > /dev/null 2>&1; then
    echo "error: missing release section unexpectedly succeeded" >&2
    exit 1
fi
if "$HELPER" 1.0.1 "$FIXTURE" | grep -q 'Only 1.0.0'; then
    echo "error: extracted notes swallowed the next version" >&2
    exit 1
fi
if "$HELPER" 1.0.0 "$FIXTURE" | grep -q 'example.invalid'; then
    echo "error: extracted notes included global link definitions" >&2
    exit 1
fi
printf 'release notes extraction tests passed\n'
