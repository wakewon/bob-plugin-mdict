#!/bin/bash
#
# 校验 .bobplugin 包的结构是否正确。
#
# 最常见的打包错误是把插件目录本身压进了 ZIP，导致 Bob 找不到 info.json。
# 这个脚本在发布前就把这类问题挡住。
set -euo pipefail

PACKAGE="${1:?用法: verify-plugin.sh <path-to-.bobplugin> [expected-build-commit]}"
EXPECTED_COMMIT="${2:-}"

if [ ! -f "$PACKAGE" ]; then
    echo "❌ 找不到文件: $PACKAGE"
    exit 1
fi

ENTRIES=$(unzip -Z1 "$PACKAGE")
FAILED=0

check_top_level() {
    local name="$1"
    if ! printf '%s\n' "$ENTRIES" | grep -qx "$name"; then
        echo "❌ 包内缺少顶层文件: $name"
        FAILED=1
    fi
}

check_top_level "info.json"
check_top_level "main.js"
check_top_level "icon.png"

# 任何带目录前缀的条目都说明压缩层级错了。
if printf '%s\n' "$ENTRIES" | grep -q '/'; then
    echo "❌ 包内存在目录层级，Bob 期望 info.json 位于压缩包根部:"
    printf '%s\n' "$ENTRIES" | grep '/' | sed 's/^/     /'
    FAILED=1
fi

INFO=$(unzip -p "$PACKAGE" info.json)
PACKAGED_MAIN=$(unzip -p "$PACKAGE" main.js)
IDENTIFIER=$(printf '%s' "$INFO" | jq -r '.identifier')
VERSION=$(printf '%s' "$INFO" | jq -r '.version')
CATEGORY=$(printf '%s' "$INFO" | jq -r '.category')
MIN_BOB_VERSION=$(printf '%s' "$INFO" | jq -r '.minBobVersion')
SOURCE_VERSION=$(tr -d ' \t\n\r' < VERSION)

if [ "$CATEGORY" != "translate" ]; then
    echo "❌ category 应为 translate，实际为 $CATEGORY"
    FAILED=1
fi
if ! printf '%s' "$IDENTIFIER" | grep -qE '^[0-9a-z.]+$'; then
    echo "❌ identifier 只能由数字、小写字母和 . 组成: $IDENTIFIER"
    FAILED=1
fi
if ! printf '%s' "$VERSION" | grep -qE '^[0-9a-z.]+$'; then
    echo "❌ version 只能由数字、小写字母和 . 组成: $VERSION"
    FAILED=1
fi
if [ "$VERSION" != "$SOURCE_VERSION" ]; then
    echo "❌ 包版本 $VERSION 与 VERSION 文件 $SOURCE_VERSION 不一致"
    FAILED=1
fi
if [ "$MIN_BOB_VERSION" != "1.20.0" ]; then
    echo "❌ /list 依赖 query.originalText，minBobVersion 必须为 1.20.0，实际为 $MIN_BOB_VERSION"
    FAILED=1
fi
if printf '%s' "$PACKAGED_MAIN" | grep -q '__BOB_MDICT_PLUGIN_'; then
    echo "❌ main.js 仍包含未替换的 build identity marker"
    FAILED=1
fi
if ! printf '%s' "$PACKAGED_MAIN" | grep -Fq "var PLUGIN_VERSION = '$VERSION';"; then
    echo "❌ packaged main.js 未注入 plugin version $VERSION"
    FAILED=1
fi
if ! printf '%s' "$PACKAGED_MAIN" | grep -Fq "var REQUIRED_API_VERSION = 'v2';"; then
    echo "❌ packaged main.js 不要求 API v2"
    FAILED=1
fi
if [ -n "$EXPECTED_COMMIT" ] && ! printf '%s' "$PACKAGED_MAIN" | grep -Fq "var PLUGIN_BUILD_COMMIT = '$EXPECTED_COMMIT';"; then
    echo "❌ packaged main.js build commit 不等于 $EXPECTED_COMMIT"
    FAILED=1
fi

if [ "$FAILED" -eq 0 ]; then
    echo "✅ 插件包结构校验通过: $IDENTIFIER v$VERSION"
else
    exit 1
fi
