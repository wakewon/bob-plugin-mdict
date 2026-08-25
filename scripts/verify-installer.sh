#!/bin/bash
# Structural smoke test for the self-contained manual installer bundle.
set -euo pipefail

ARCHIVE="${1:?usage: verify-installer.sh <installer.tar.gz>}"
[ -f "$ARCHIVE" ] || { echo "error: missing $ARCHIVE" >&2; exit 1; }
TEMP=$(mktemp -d)
trap 'rm -rf "$TEMP"' EXIT
tar -xzf "$ARCHIVE" -C "$TEMP"
ROOT=$(find "$TEMP" -mindepth 1 -maxdepth 1 -type d | head -n 1)
[ -n "$ROOT" ] || { echo "error: installer has no root directory" >&2; exit 1; }

for path in bob-mdict install.sh uninstall.sh \
    launchd/com.github.wakewon.bob-mdict.plist LICENSE THIRD_PARTY_NOTICES.md INSTALL.md; do
    [ -f "$ROOT/$path" ] || { echo "error: installer missing $path" >&2; exit 1; }
done
[ -x "$ROOT/bob-mdict" ] && [ -x "$ROOT/install.sh" ] && [ -x "$ROOT/uninstall.sh" ]
bash -n "$ROOT/install.sh" "$ROOT/uninstall.sh"
grep -q '__BINARY_PATH__' "$ROOT/launchd/com.github.wakewon.bob-mdict.plist"
grep -q '__LOG_DIR__' "$ROOT/launchd/com.github.wakewon.bob-mdict.plist"

RENDERED="$TEMP/rendered.plist"
sed -e 's|__BINARY_PATH__|/tmp/bob-mdict-smoke/bin/bob-mdict|g' \
    -e 's|__LOG_DIR__|/tmp/bob-mdict-smoke/logs|g' \
    "$ROOT/launchd/com.github.wakewon.bob-mdict.plist" > "$RENDERED"
if grep -q '__[A-Z_]*__' "$RENDERED"; then
    echo "error: rendered plist retains placeholders" >&2
    exit 1
fi
"$ROOT/bob-mdict" --version | grep -q 'api=v2'

SMOKE_HOME="$TEMP/home"
SMOKE_PREFIX="$TEMP/prefix"
HOME="$SMOKE_HOME" PREFIX="$SMOKE_PREFIX" BOB_MDICT_INSTALL_SMOKE=1 "$ROOT/install.sh" >/dev/null
[ -x "$SMOKE_PREFIX/bin/bob-mdict" ]
SMOKE_AGENT="$SMOKE_HOME/Library/LaunchAgents/com.github.wakewon.bob-mdict.plist"
[ -f "$SMOKE_AGENT" ]
grep -q "$SMOKE_PREFIX/bin/bob-mdict" "$SMOKE_AGENT"
if grep -q '__[A-Z_]*__' "$SMOKE_AGENT"; then
    echo "error: installed smoke plist retains placeholders" >&2
    exit 1
fi
HOME="$SMOKE_HOME" PREFIX="$SMOKE_PREFIX" BOB_MDICT_INSTALL_SMOKE=1 "$ROOT/uninstall.sh" >/dev/null
[ ! -e "$SMOKE_PREFIX/bin/bob-mdict" ]
[ ! -e "$SMOKE_AGENT" ]
printf 'installer smoke passed: %s\n' "$ARCHIVE"
