#!/bin/bash
#
# 校验 .bobplugin 包的结构是否正确。
#
# 最常见的打包错误是把插件目录本身压进了 ZIP，导致 Bob 找不到 info.json。
# 这个脚本在发布前就把这类问题挡住。
set -euo pipefail

PACKAGE="${1:?用法: verify-plugin.sh <path-to-.bobplugin>}"

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
IDENTIFIER=$(printf '%s' "$INFO" | jq -r '.identifier')
VERSION=$(printf '%s' "$INFO" | jq -r '.version')
CATEGORY=$(printf '%s' "$INFO" | jq -r '.category')

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

# appcast 必须与包内实际内容对得上，否则 Bob 会拒绝更新。
if [ -f appcast.json ]; then
    APPCAST_ID=$(jq -r '.identifier' appcast.json)
    if [ "$APPCAST_ID" != "$IDENTIFIER" ]; then
        echo "❌ appcast.json identifier ($APPCAST_ID) 与 info.json ($IDENTIFIER) 不一致"
        FAILED=1
    fi
    ENTRY=$(jq -r --arg v "$VERSION" '.versions[] | select(.version == $v)' appcast.json)
    if [ -z "$ENTRY" ]; then
        echo "❌ appcast.json 中没有版本 $VERSION 的条目"
        FAILED=1
    else
        EXPECTED_SHA=$(shasum -a 256 "$PACKAGE" | awk '{print $1}')
        ACTUAL_SHA=$(printf '%s' "$ENTRY" | jq -r '.sha256')
        if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
            echo "❌ appcast.json 中的 sha256 与实际包不符"
            echo "     期望 $EXPECTED_SHA"
            echo "     实际 $ACTUAL_SHA"
            FAILED=1
        fi
        TS=$(printf '%s' "$ENTRY" | jq -r '.timestamp')
        # Bob 要求毫秒时间戳；秒级时间戳只有 10 位。
        if [ "${#TS}" -lt 13 ]; then
            echo "❌ appcast.json timestamp 应为毫秒 (13 位)，实际为 $TS"
            FAILED=1
        fi
    fi
fi

if [ "$FAILED" -eq 0 ]; then
    echo "✅ 插件包结构校验通过: $IDENTIFIER v$VERSION"
else
    exit 1
fi
