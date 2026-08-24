#!/bin/bash
#
# Bob 插件一键打包脚本
#
# 沿用 AiTranslate 的发布流程：从版本号生成 .bobplugin、计算 SHA256、
# 用 jq 就地更新 appcast.json，产物统一放在 release/。
# 两个插件项目保持同一套构建命令和 release 布局。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PLUGIN_DIR="plugin"
OUT_DIR="release"
PLUGIN_NAME="MDict"
GITHUB_REPO="wakewon/bob-plugin-mdict"

if ! command -v jq &> /dev/null; then
    echo "错误: 未找到 jq 命令，请先安装 jq (例如: brew install jq)"
    exit 1
fi

if [ ! -f "$PLUGIN_DIR/info.json" ]; then
    echo "错误: $PLUGIN_DIR/info.json 不存在"
    exit 1
fi

# VERSION 是全仓库唯一的版本来源，先同步进 info.json，
# 保证 info.json / appcast.json / 服务二进制三者版本永远一致。
VERSION=$(tr -d ' \t\n\r' < VERSION)
if [ -z "$VERSION" ]; then
    echo "错误: VERSION 文件为空"
    exit 1
fi

CURRENT_VERSION=$(jq -r '.version' "$PLUGIN_DIR/info.json")
if [ "$CURRENT_VERSION" != "$VERSION" ]; then
    echo "同步 info.json 版本号: $CURRENT_VERSION -> $VERSION"
    jq --arg v "$VERSION" '.version = $v' "$PLUGIN_DIR/info.json" > "$PLUGIN_DIR/info.json.tmp"
    mv "$PLUGIN_DIR/info.json.tmp" "$PLUGIN_DIR/info.json"
fi

IDENTIFIER=$(jq -r '.identifier' "$PLUGIN_DIR/info.json")
MIN_BOB_VERSION=$(jq -r '.minBobVersion // "1.8.0"' "$PLUGIN_DIR/info.json")

echo "正在打包 $IDENTIFIER 版本: $VERSION..."

mkdir -p "$OUT_DIR"
PACKAGE_NAME="$OUT_DIR/$PLUGIN_NAME-v$VERSION.bobplugin"
rm -f "$PACKAGE_NAME"

# .bobplugin 本质是 ZIP。必须压缩插件根目录“内部”的文件，
# 而不是把插件目录本身作为额外的顶层目录。
(
    cd "$PLUGIN_DIR"
    zip -q -r "../$PACKAGE_NAME" info.json main.js icon.png -x ".*" -x "__MACOSX"
)

echo "========================================="
echo "✅ 打包成功!"
echo "📦 产物路径: $PACKAGE_NAME"
echo "========================================="

SHA256=$(shasum -a 256 "$PACKAGE_NAME" | awk '{print $1}')
echo "SHA256: $SHA256"

TIMESTAMP=$(($(date +%s) * 1000))

if [ -f "appcast.json" ]; then
    echo "正在更新 appcast.json..."
    jq --arg version "$VERSION" \
       --arg sha256 "$SHA256" \
       --arg minBob "$MIN_BOB_VERSION" \
       --arg repo "$GITHUB_REPO" \
       --arg plugin "$PLUGIN_NAME" \
       --argjson timestamp "$TIMESTAMP" '
        if (.versions | map(.version) | index($version)) then
            .versions |= map(
                if .version == $version then
                    .sha256 = $sha256 | .timestamp = $timestamp | .minBobVersion = $minBob
                else . end
            )
        else
            .versions = [{
                "version": $version,
                "desc": "MDict for Bob \($version)",
                "sha256": $sha256,
                "url": "https://github.com/\($repo)/releases/download/v\($version)/\($plugin)-v\($version).bobplugin",
                "minBobVersion": $minBob,
                "timestamp": $timestamp
            }] + .versions
        end
    ' appcast.json > appcast.json.tmp && mv appcast.json.tmp appcast.json
    echo "appcast.json 更新完成"
else
    echo "appcast.json 不存在，跳过更新。"
fi

# 打包完立即自检，避免把结构错误的包发出去。
"$REPO_ROOT/scripts/verify-plugin.sh" "$PACKAGE_NAME"
