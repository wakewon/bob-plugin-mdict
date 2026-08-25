#!/bin/bash
# Build release binaries and architecture tarballs without touching source.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
release_require_semver "$VERSION"
COMMIT=$(release_build_commit)
OUT_DIR="${RELEASE_DIR:-release}"
mkdir -p "$OUT_DIR"

LDFLAGS="-s -w -X github.com/wakewon/bob-plugin-mdict/internal/version.Version=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/wakewon/bob-plugin-mdict/internal/version.Commit=$COMMIT"

build_binary() {
    local arch="$1"
    CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" \
        -o "$OUT_DIR/bob-mdict-darwin-$arch" ./cmd/bob-mdict
}

build_binary arm64
build_binary amd64
if command -v lipo >/dev/null 2>&1; then
    lipo -create "$OUT_DIR/bob-mdict-darwin-arm64" "$OUT_DIR/bob-mdict-darwin-amd64" \
        -output "$OUT_DIR/bob-mdict-darwin-universal"
fi

for arch in arm64 amd64; do
    stage=$(mktemp -d)
    cp "$OUT_DIR/bob-mdict-darwin-$arch" "$stage/bob-mdict"
    chmod 0755 "$stage/bob-mdict"
    tar -czf "$OUT_DIR/bob-mdict-$VERSION-darwin-$arch.tar.gz" -C "$stage" bob-mdict
    rm -rf "$stage"
done

printf 'built bob-mdict %s (%s) for darwin/arm64 and darwin/amd64\n' "$VERSION" "$COMMIT"
