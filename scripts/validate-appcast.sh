#!/bin/bash
# Validate tracked appcast schema without requiring unpublished versions.
set -euo pipefail

jq -e '
  .identifier == "com.github.wakewon.mdict" and
  (.versions | type == "array") and
  ([.versions[].version] | length == (unique | length)) and
  ([.versions[] |
    (.version | test("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")) and
    (.url == ("https://github.com/wakewon/bob-plugin-mdict/releases/download/v" + .version + "/MDict-v" + .version + ".bobplugin")) and
    (.sha256 | test("^[0-9a-f]{64}$")) and
    (.buildCommit | test("^[0-9a-f]{7,40}$")) and
    (.minBobVersion == "1.20.0") and
    (.timestamp | type == "number")
  ] | all)
' appcast.json >/dev/null

VERSIONS=$(jq -r '.versions[].version' appcast.json)
if [ -n "$VERSIONS" ]; then
    SORTED=$(printf '%s\n' "$VERSIONS" | sort -Vr)
    [ "$VERSIONS" = "$SORTED" ] || { echo "error: appcast versions are not newest-first" >&2; exit 1; }
fi
printf 'appcast schema passed\n'
