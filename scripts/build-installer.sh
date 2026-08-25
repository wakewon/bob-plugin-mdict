#!/bin/bash
# Build a self-contained manual installer using the universal macOS binary.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
OUT_DIR="${RELEASE_DIR:-release}"
BINARY="$OUT_DIR/bob-mdict-darwin-universal"
[ -x "$BINARY" ] || { echo "error: missing universal binary; run build-server.sh on macOS" >&2; exit 1; }

STAGE_ROOT=$(mktemp -d)
trap 'rm -rf "$STAGE_ROOT"' EXIT
BUNDLE="$STAGE_ROOT/bob-mdict-$VERSION-macos-installer"
mkdir -p "$BUNDLE/launchd"
cp "$BINARY" "$BUNDLE/bob-mdict"
cp packaging/install.sh packaging/uninstall.sh "$BUNDLE/"
cp packaging/launchd/com.github.wakewon.bob-mdict.plist "$BUNDLE/launchd/"
cp LICENSE THIRD_PARTY_NOTICES.md "$BUNDLE/"
cp packaging/INSTALL.md "$BUNDLE/INSTALL.md"
chmod 0755 "$BUNDLE/bob-mdict" "$BUNDLE/install.sh" "$BUNDLE/uninstall.sh"
tar -czf "$OUT_DIR/bob-mdict-$VERSION-macos-installer.tar.gz" -C "$STAGE_ROOT" "$(basename "$BUNDLE")"
scripts/verify-installer.sh "$OUT_DIR/bob-mdict-$VERSION-macos-installer.tar.gz"
printf 'built %s/bob-mdict-%s-macos-installer.tar.gz\n' "$OUT_DIR" "$VERSION"
