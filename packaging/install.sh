#!/bin/bash
#
# 手动安装 bob-mdict（不使用 Homebrew 时）。
#
# 安装二进制、创建词典目录、注册 LaunchAgent 开机自启。
# 用户不需要自己写 plist，也不需要保持终端窗口开着。
set -euo pipefail

LABEL="com.github.wakewon.bob-mdict"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
BINARY="$BIN_DIR/bob-mdict"
DICT_DIR="$HOME/Library/Application Support/bob-mdict/dictionaries"
LOG_DIR="$HOME/Library/Logs"
AGENT_DIR="$HOME/Library/LaunchAgents"
AGENT="$AGENT_DIR/$LABEL.plist"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> 安装 bob-mdict"

# 优先使用 release/ 中的通用二进制，其次按当前架构挑选。
find_binary() {
    local candidates=(
        "$SCRIPT_DIR/../release/bob-mdict-darwin-universal"
        "$SCRIPT_DIR/../release/bob-mdict-darwin-$(uname -m | sed 's/x86_64/amd64/')"
        "$SCRIPT_DIR/bob-mdict"
    )
    for candidate in "${candidates[@]}"; do
        if [ -f "$candidate" ]; then
            echo "$candidate"
            return 0
        fi
    done
    return 1
}

SOURCE_BINARY=$(find_binary) || {
    echo "错误: 找不到 bob-mdict 二进制。"
    echo "      请先运行 ./scripts/build-server.sh，或从 GitHub Release 下载后放到 release/ 目录。"
    exit 1
}

mkdir -p "$BIN_DIR" "$DICT_DIR" "$LOG_DIR" "$AGENT_DIR"

# 升级场景：先停旧服务，避免二进制被占用。
#
# bootout 是异步的：命令返回时服务往往还没真正退出，此时立刻 bootstrap
# 会失败，结果就是升级完一个服务都没跑起来。所以必须等它真的消失。
if launchctl list 2>/dev/null | grep -q "$LABEL"; then
    echo "==> 停止正在运行的服务"
    launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || launchctl unload "$AGENT" 2>/dev/null || true
    for _ in $(seq 1 50); do
        launchctl list 2>/dev/null | grep -q "$LABEL" || break
        sleep 0.2
    done
    if launchctl list 2>/dev/null | grep -q "$LABEL"; then
        echo "错误: 旧服务没有退出，请稍后重试，或手动执行:"
        echo "      launchctl bootout gui/$(id -u)/$LABEL"
        exit 1
    fi
fi

install -m 0755 "$SOURCE_BINARY" "$BINARY"
echo "==> 已安装: $BINARY"

# macOS 会给从网络下载的二进制打隔离标记，先清掉，否则首次运行会被拦截。
xattr -d com.apple.quarantine "$BINARY" 2>/dev/null || true

sed -e "s|__BINARY_PATH__|$BINARY|g" \
    -e "s|__LOG_DIR__|$LOG_DIR|g" \
    "$SCRIPT_DIR/launchd/$LABEL.plist" > "$AGENT"
echo "==> 已写入 LaunchAgent: $AGENT"

# Release verification uses an isolated HOME/PREFIX to prove the complete
# bundle without registering a real user LaunchAgent.
if [ "${BOB_MDICT_INSTALL_SMOKE:-0}" = "1" ]; then
    grep -q "$BINARY" "$AGENT"
    grep -q "$LOG_DIR" "$AGENT"
    echo "==> installer smoke mode complete"
    exit 0
fi

# 不吞掉 bootstrap 的错误：装完却没跑起来是最糟的失败方式。
if ! launchctl bootstrap "gui/$(id -u)" "$AGENT" 2>/dev/null; then
    launchctl load "$AGENT"
fi

# 确认服务真的起来了，而不是只写了个 plist 就宣布成功。
STARTED=0
for _ in $(seq 1 60); do
    if curl -sf --max-time 1 "http://127.0.0.1:15321/v2/status" > /dev/null 2>&1; then
        STARTED=1
        break
    fi
    sleep 0.5
done

if [ "$STARTED" -eq 1 ]; then
    echo "==> 服务已启动，并会随登录自动运行"
else
    echo "警告: 服务已注册，但还没有响应。"
    echo "      词典较多时首次建立索引需要一点时间，可稍后运行 $BINARY --check 确认。"
    echo "      日志: $LOG_DIR/bob-mdict.log"
fi

if ! command -v speexdec &> /dev/null && ! command -v ffmpeg &> /dev/null; then
    echo
    echo "提示: 未检测到 Speex 解码器。"
    echo "      少数词典（如 LDOCE5++）的部分发音是 Ogg-Speex 格式，macOS 无法直接播放。"
    echo "      安装解码器后这些发音才会显示：brew install speex"
fi

echo
echo "==> 完成"
echo
echo "词典目录："
echo "  $DICT_DIR"
echo
echo "把词典文件夹（含 .mdx 与配套 .mdd）复制进去，然后运行："
echo "  $BINARY --rescan"
echo
echo "自检："
echo "  $BINARY --check"
