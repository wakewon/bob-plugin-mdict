#!/bin/bash
# Build the Bob plugin without modifying tracked source or appcast metadata.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
release_require_semver "$VERSION"
SOURCE_VERSION=$(jq -r '.version' plugin/info.json)
if [ "$SOURCE_VERSION" != "$VERSION" ]; then
    echo "error: plugin/info.json version $SOURCE_VERSION != VERSION $VERSION" >&2
    exit 1
fi

COMMIT=$(release_build_commit)
OUT_DIR="${RELEASE_DIR:-release}"
PACKAGE="$OUT_DIR/MDict-v$VERSION.bobplugin"
STAGE_DIR=$(mktemp -d)
trap 'rm -rf "$STAGE_DIR"' EXIT
mkdir -p "$OUT_DIR"

cp plugin/info.json plugin/icon.png "$STAGE_DIR/"
sed -e "s/__BOB_MDICT_PLUGIN_VERSION__/$VERSION/g" \
    -e "s/__BOB_MDICT_PLUGIN_COMMIT__/$COMMIT/g" \
    plugin/main.js > "$STAGE_DIR/main.js"
(
    cd "$STAGE_DIR"
    zip -q -X "$REPO_ROOT/$PACKAGE" info.json main.js icon.png
)

scripts/verify-plugin.sh "$PACKAGE" "$COMMIT"
printf 'built %s (%s)\n' "$PACKAGE" "$COMMIT"
