#!/bin/bash
#
# 一键生成一次完整发布所需的全部产物。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

VERSION=$(tr -d ' \t\n\r' < VERSION)
echo "==> 发布版本 v$VERSION"

echo "==> 运行测试"
go test ./...

echo "==> 打包 Bob 插件"
./scripts/build-plugin.sh

echo "==> 构建服务二进制"
./scripts/build-server.sh

# formula 里的 sha256 必须与刚构建出的 tar.gz 完全一致，
# 手工抄写极易出错，所以直接生成。
ARM_SHA=$(shasum -a 256 "release/bob-mdict-$VERSION-darwin-arm64.tar.gz" | awk '{print $1}')
AMD_SHA=$(shasum -a 256 "release/bob-mdict-$VERSION-darwin-amd64.tar.gz" | awk '{print $1}')
mkdir -p release
sed -e "s|REPLACE_WITH_ARM64_SHA256|$ARM_SHA|" \
    -e "s|REPLACE_WITH_AMD64_SHA256|$AMD_SHA|" \
    -e "s|version \"[0-9][^\"]*\"|version \"$VERSION\"|" \
    packaging/homebrew/bob-mdict.rb > release/bob-mdict.rb
echo "==> 已生成 release/bob-mdict.rb（复制到 homebrew tap 仓库）"

echo
echo "==> 产物"
ls -1 release
echo
echo "接下来："
echo "  1. git commit -am \"release v$VERSION\" && git tag v$VERSION && git push --tags"
echo "  2. gh release create v$VERSION release/MDict-v$VERSION.bobplugin \\"
echo "       release/bob-mdict-$VERSION-darwin-arm64.tar.gz \\"
echo "       release/bob-mdict-$VERSION-darwin-amd64.tar.gz \\"
echo "       release/SHA256SUMS"
echo "  3. 把 release/bob-mdict.rb 提交到 wakewon/homebrew-tap"
echo "  4. 确认 GitHub 仓库已添加 bobplugin topic"
