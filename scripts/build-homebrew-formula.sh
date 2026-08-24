#!/bin/bash
# Generate the publishable Formula from VERSION and the built release tarballs.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

VERSION=$(tr -d ' \t\n\r' < VERSION)
ARM_TARBALL="release/bob-mdict-$VERSION-darwin-arm64.tar.gz"
AMD_TARBALL="release/bob-mdict-$VERSION-darwin-amd64.tar.gz"

for archive in "$ARM_TARBALL" "$AMD_TARBALL"; do
    if [ ! -f "$archive" ]; then
        echo "错误: 缺少 $archive；请先运行 ./scripts/build-server.sh"
        exit 1
    fi
done

ARM_SHA=$(shasum -a 256 "$ARM_TARBALL" | awk '{print $1}')
AMD_SHA=$(shasum -a 256 "$AMD_TARBALL" | awk '{print $1}')
sed -e "s|REPLACE_WITH_ARM64_SHA256|$ARM_SHA|" \
    -e "s|REPLACE_WITH_AMD64_SHA256|$AMD_SHA|" \
    -e "s|version \"[0-9][^\"]*\"|version \"$VERSION\"|" \
    packaging/homebrew/bob-mdict.rb > release/bob-mdict.rb

echo "已生成 release/bob-mdict.rb (v$VERSION)"
printf 'arm64 sha256: %s\n' "$ARM_SHA"
printf 'amd64 sha256: %s\n' "$AMD_SHA"
