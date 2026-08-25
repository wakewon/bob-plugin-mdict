#!/bin/bash
# Extract exactly one version section from CHANGELOG.md.
set -euo pipefail

VERSION="${1:?usage: extract-release-notes.sh <version> [changelog]}"
CHANGELOG="${2:-CHANGELOG.md}"
[ -f "$CHANGELOG" ] || { echo "error: missing changelog: $CHANGELOG" >&2; exit 1; }

awk -v version="$VERSION" '
BEGIN { header = "## [" version "]" }
index($0, header) == 1 && (length($0) == length(header) || substr($0, length(header) + 1, 1) == " ") {
    found = 1
    next
}
found && /^## \[/ { exit }
found && /^\[[^]]+\]:[[:space:]]/ { exit }
found { lines[++count] = $0 }
END {
    if (!found) {
        print "error: CHANGELOG section not found for " version > "/dev/stderr"
        exit 1
    }
    first = 1
    while (first <= count && lines[first] == "") first++
    last = count
    while (last >= first && lines[last] == "") last--
    for (i = first; i <= last; i++) print lines[i]
}
' "$CHANGELOG"
