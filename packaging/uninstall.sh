#!/bin/bash
#
# 卸载 bob-mdict。
#
# 默认保留用户自己的词典，只删除本项目安装的东西。
set -euo pipefail

LABEL="com.github.wakewon.bob-mdict"
PREFIX="${PREFIX:-$HOME/.local}"
BINARY="$PREFIX/bin/bob-mdict"
AGENT="$HOME/Library/LaunchAgents/$LABEL.plist"
SUPPORT_DIR="$HOME/Library/Application Support/bob-mdict"
CACHE_DIR="$HOME/Library/Caches/bob-mdict"

echo "==> 卸载 bob-mdict"

if [ "${BOB_MDICT_INSTALL_SMOKE:-0}" != "1" ] && launchctl list | grep -q "$LABEL"; then
    launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || launchctl unload "$AGENT" 2>/dev/null || true
    echo "==> 服务已停止"
fi

rm -f "$AGENT" && echo "==> 已移除 LaunchAgent"
rm -f "$BINARY" && echo "==> 已移除二进制"

# 转码缓存是可再生的派生数据，删掉没有损失。
rm -rf "$CACHE_DIR" && echo "==> 已清除音频转码缓存"

if [ -d "$SUPPORT_DIR/dictionaries" ]; then
    echo
    echo "你的词典保留在："
    echo "  $SUPPORT_DIR/dictionaries"
    echo
    echo "如果确定不再需要，可手动删除："
    echo "  rm -rf \"$SUPPORT_DIR\""
fi

echo
echo "==> 完成。Bob 插件请在 Bob 的「服务」设置中单独卸载。"
