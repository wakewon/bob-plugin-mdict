#!/bin/bash
# Render the Homebrew formula template from release tarball checksums.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
OUT_DIR="${RELEASE_DIR:-release}"
ARM="$OUT_DIR/bob-mdict-$VERSION-darwin-arm64.tar.gz"
AMD="$OUT_DIR/bob-mdict-$VERSION-darwin-amd64.tar.gz"
for archive in "$ARM" "$AMD"; do
    [ -f "$archive" ] || { echo "error: missing $archive" >&2; exit 1; }
done

ARM_SHA=$(shasum -a 256 "$ARM" | awk '{print $1}')
AMD_SHA=$(shasum -a 256 "$AMD" | awk '{print $1}')
sed -e "s|@VERSION@|$VERSION|g" \
    -e "s|@ARM64_SHA256@|$ARM_SHA|g" \
    -e "s|@AMD64_SHA256@|$AMD_SHA|g" \
    packaging/homebrew/bob-mdict.rb.tmpl > "$OUT_DIR/bob-mdict.rb"
printf 'built %s/bob-mdict.rb\n' "$OUT_DIR"
