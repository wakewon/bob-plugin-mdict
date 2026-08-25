#!/bin/bash
# Apply post-release distribution metadata in a fresh main checkout.
set -euo pipefail

VERSION="${1:?version}"
PLUGIN_SHA="${2:?plugin sha256}"
BUILD_COMMIT="${3:?tag commit}"
TIMESTAMP="${4:?milliseconds timestamp}"
URL="https://github.com/wakewon/bob-plugin-mdict/releases/download/v$VERSION/MDict-v$VERSION.bobplugin"

jq --arg version "$VERSION" --arg sha256 "$PLUGIN_SHA" --arg commit "$BUILD_COMMIT" \
    --arg url "$URL" --argjson timestamp "$TIMESTAMP" '
      .versions = ([{
        version:$version,
        desc:("MDict for Bob " + $version),
        sha256:$sha256,
        url:$url,
        minBobVersion:"1.20.0",
        buildCommit:$commit,
        timestamp:$timestamp
      }] + [.versions[] | select(.version != $version)])
    ' appcast.json > appcast.json.next
mv appcast.json.next appcast.json
scripts/validate-appcast.sh
