#!/bin/bash
#
# 构建 bob-mdict 本地服务的发布产物。
#
# 输出与 Bob 插件相同的 release/ 目录，保持两个组件的发布布局一致。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

OUT_DIR="release"
BINARY="bob-mdict"
PKG="./cmd/bob-mdict"

VERSION=$(tr -d ' \t\n\r' < VERSION)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X github.com/wakewon/bob-plugin-mdict/internal/version.Version=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/wakewon/bob-plugin-mdict/internal/version.Commit=$COMMIT"

mkdir -p "$OUT_DIR"

build() {
    local goos="$1" goarch="$2"
    local output="$OUT_DIR/$BINARY-$goos-$goarch"
    echo "构建 $goos/$goarch ..."
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$output" "$PKG"
}

build darwin arm64
build darwin amd64

# 通用二进制让安装脚本和 Homebrew formula 不必关心用户的 CPU 架构。
if command -v lipo &> /dev/null; then
    echo "合并为 universal 二进制 ..."
    lipo -create \
        "$OUT_DIR/$BINARY-darwin-arm64" \
        "$OUT_DIR/$BINARY-darwin-amd64" \
        -output "$OUT_DIR/$BINARY-darwin-universal"
fi

# 打成 tar.gz 供 Homebrew 与手动安装使用。
for target in darwin-arm64 darwin-amd64; do
    tarball="$OUT_DIR/$BINARY-$VERSION-$target.tar.gz"
    rm -f "$tarball"
    tar -czf "$tarball" -C "$OUT_DIR" "$BINARY-$target" \
        --transform "s|$BINARY-$target|$BINARY|" 2>/dev/null \
        || (staging=$(mktemp -d) && cp "$OUT_DIR/$BINARY-$target" "$staging/$BINARY" \
            && tar -czf "$tarball" -C "$staging" "$BINARY" && rm -rf "$staging")
done

(
    cd "$OUT_DIR"
    artifacts=(
        "$BINARY-$VERSION-darwin-amd64.tar.gz"
        "$BINARY-$VERSION-darwin-arm64.tar.gz"
        "$BINARY-darwin-amd64"
        "$BINARY-darwin-arm64"
    )
    if [ -f "$BINARY-darwin-universal" ]; then
        artifacts+=("$BINARY-darwin-universal")
    fi
    # Only the current plugin belongs in this release manifest. Keeping older
    # local artifacts in release/ must not mix versions in SHA256SUMS.
    if [ -f "MDict-v$VERSION.bobplugin" ]; then
        artifacts+=("MDict-v$VERSION.bobplugin")
    fi
    shasum -a 256 "${artifacts[@]}" > SHA256SUMS
)

echo "========================================="
echo "✅ 服务构建完成 (v$VERSION, $COMMIT)"
ls -la "$OUT_DIR"
echo "========================================="
cat "$OUT_DIR/SHA256SUMS"
