#!/bin/bash
# Build, install and prove the identity of the standalone development daemon.
# This script deliberately refuses to replace Homebrew or an unknown listener.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

LABEL="com.github.wakewon.bob-mdict"
PORT=15321
DEV_PREFIX="${BOB_MDICT_DEV_PREFIX:-${HOME}/.local}"
EXPECTED_BINARY="$DEV_PREFIX/bin/bob-mdict"

if [ "$(uname -s)" != "Darwin" ]; then
    echo "错误: dev-deploy.sh 只管理 macOS LaunchAgent。"
    exit 1
fi

if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
    echo "错误: 工作区不干净。请先提交变更，让 buildCommit 能准确标识构建源码。"
    exit 1
fi

VERSION=$(tr -d ' \t\n\r' < VERSION)
COMMIT=$(git rev-parse --short HEAD)

listener_pid() {
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null | sort -u | head -n 1
}

executable_for_pid() {
    lsof -a -p "$1" -d txt -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1
}

if command -v brew >/dev/null 2>&1 && brew list --versions bob-mdict >/dev/null 2>&1; then
    echo "错误: 检测到 Homebrew 管理的 bob-mdict。"
    echo "      请用 brew upgrade/reinstall 和 brew services restart 更新它；dev-deploy 不会覆盖 Homebrew。"
    exit 1
fi

BEFORE_PID=$(listener_pid || true)
if [ -n "$BEFORE_PID" ]; then
    BEFORE_BINARY=$(executable_for_pid "$BEFORE_PID" || true)
    if [ "$BEFORE_BINARY" != "$EXPECTED_BINARY" ]; then
        echo "错误: 端口 $PORT 已由非 development daemon 占用。"
        echo "      PID: $BEFORE_PID"
        echo "      executable: ${BEFORE_BINARY:-unknown}"
        echo "      expected: $EXPECTED_BINARY"
        exit 1
    fi
fi

echo "==> build: bob-mdict $VERSION ($COMMIT)"
./scripts/build-server.sh

BUILT_IDENTITY=$(./release/bob-mdict-darwin-"$(uname -m | sed 's/x86_64/amd64/')" --version)
case "$BUILT_IDENTITY" in
    *"$VERSION ($COMMIT)"*"api=v1"*) ;;
    *)
        echo "错误: 构建产物 identity 不符合预期: $BUILT_IDENTITY"
        exit 1
        ;;
esac

echo "==> install/deploy: $EXPECTED_BINARY"
PREFIX="$DEV_PREFIX" ./packaging/install.sh

STATUS_JSON=""
for _ in $(seq 1 60); do
    STATUS_JSON=$(curl -fsS --max-time 1 "http://127.0.0.1:$PORT/v1/status" 2>/dev/null || true)
    if [ -n "$STATUS_JSON" ]; then
        break
    fi
    sleep 0.5
done
if [ -z "$STATUS_JSON" ]; then
    echo "错误: deployment 后端口 $PORT 没有响应。"
    exit 1
fi

ACTUAL_VERSION=$(printf '%s' "$STATUS_JSON" | jq -r '.serviceVersion // ""')
ACTUAL_COMMIT=$(printf '%s' "$STATUS_JSON" | jq -r '.buildCommit // ""')
ACTUAL_API=$(printf '%s' "$STATUS_JSON" | jq -r '.apiVersion // ""')
if [ "$ACTUAL_VERSION" != "$VERSION" ] || [ "$ACTUAL_COMMIT" != "$COMMIT" ] || [ "$ACTUAL_API" != "v1" ]; then
    echo "错误: runtime identity 与 repository/build 不一致。"
    printf '      expected: version=%s commit=%s api=v1\n' "$VERSION" "$COMMIT"
    printf '      actual:   version=%s commit=%s api=%s\n' "$ACTUAL_VERSION" "$ACTUAL_COMMIT" "$ACTUAL_API"
    exit 1
fi

AFTER_PID=$(listener_pid || true)
AFTER_BINARY=$(executable_for_pid "$AFTER_PID" || true)
if [ -z "$AFTER_PID" ] || [ "$AFTER_BINARY" != "$EXPECTED_BINARY" ]; then
    echo "错误: 无法证明端口 $PORT 由安装后的 development binary 监听。"
    exit 1
fi

# Lightweight real HTTP smoke test; this reads registry metadata only and does
# not write dictionary data or touch the user's dictionary directory.
curl -fsS --max-time 5 "http://127.0.0.1:$PORT/v1/dictionaries" | jq -e '.dictionaries | type == "array"' >/dev/null

echo "==> runtime verified"
printf 'Repository HEAD: %s\n' "$(git rev-parse HEAD)"
printf 'Built binary:    %s\n' "$BUILT_IDENTITY"
printf 'Installed path:  %s\n' "$AFTER_BINARY"
printf 'Listening PID:   %s\n' "$AFTER_PID"
printf 'Runtime status:  serviceVersion=%s buildCommit=%s apiVersion=%s\n' \
    "$ACTUAL_VERSION" "$ACTUAL_COMMIT" "$ACTUAL_API"
