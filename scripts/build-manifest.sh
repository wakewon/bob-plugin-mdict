#!/bin/bash
# Generate release provenance and checksums for public assets only.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
COMMIT=$(release_build_commit)
TAG="${RELEASE_TAG:-v$VERSION}"
OUT_DIR="${RELEASE_DIR:-release}"
TMP=$(mktemp)
trap 'rm -f "$TMP" "$TMP.next"' EXIT

printf '[]' > "$TMP"
for name in "MDict-v$VERSION.bobplugin" \
    "bob-mdict-$VERSION-darwin-arm64.tar.gz" \
    "bob-mdict-$VERSION-darwin-amd64.tar.gz" \
    "bob-mdict-$VERSION-macos-installer.tar.gz" \
    "bob-mdict.rb"; do
    [ -f "$OUT_DIR/$name" ] || { echo "error: missing $OUT_DIR/$name" >&2; exit 1; }
    sha=$(shasum -a 256 "$OUT_DIR/$name" | awk '{print $1}')
    jq --arg filename "$name" --arg sha256 "$sha" '. + [{filename:$filename,sha256:$sha256}]' \
        "$TMP" > "$TMP.next"
    mv "$TMP.next" "$TMP"
done

jq -n --arg version "$VERSION" --arg apiVersion "v2" --arg buildCommit "$COMMIT" \
    --arg tag "$TAG" --slurpfile artifacts "$TMP" \
    '{version:$version,apiVersion:$apiVersion,buildCommit:$buildCommit,tag:$tag,artifacts:$artifacts[0]}' \
    > "$OUT_DIR/RELEASE_MANIFEST.json"

: > "$OUT_DIR/SHA256SUMS"
release_public_assets "$VERSION" | while IFS= read -r name; do
    [ -f "$OUT_DIR/$name" ] || { echo "error: missing $OUT_DIR/$name" >&2; exit 1; }
    (cd "$OUT_DIR" && shasum -a 256 "$name") >> "$OUT_DIR/SHA256SUMS"
done
printf 'built release manifest and SHA256SUMS\n'
