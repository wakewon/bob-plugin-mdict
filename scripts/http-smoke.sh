#!/bin/bash
# End-to-end HTTP v2 smoke test against an isolated empty dictionary library.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
COMMIT=$(release_build_commit)
ARCH=$(uname -m | sed 's/x86_64/amd64/')
BINARY="${1:-release/bob-mdict-darwin-$ARCH}"
TEMP=$(mktemp -d)
PORT=15399
PID=""
cleanup() {
    if [ -n "$PID" ]; then kill "$PID" 2>/dev/null || true; fi
    rm -rf "$TEMP"
}
trap cleanup EXIT

HOME="$TEMP/home" "$BINARY" --dictionary-dir "$TEMP/dictionaries" --port "$PORT" > "$TEMP/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 60); do
    STATUS=$(curl -fsS --max-time 1 "http://127.0.0.1:$PORT/v2/status" 2>/dev/null || true)
    [ -n "$STATUS" ] && break
    sleep 0.25
done
[ -n "${STATUS:-}" ] || { echo "error: isolated server did not start" >&2; exit 1; }
printf '%s' "$STATUS" | jq -e --arg version "$VERSION" --arg commit "$COMMIT" \
    '.serviceVersion==$version and .buildCommit==$commit and .apiVersion=="v2"' >/dev/null
curl -fsS "http://127.0.0.1:$PORT/v2/dictionaries" | jq -e '.dictionaries == []' >/dev/null
curl -sS -X POST "http://127.0.0.1:$PORT/v2/lookup" -H 'Content-Type: application/json' \
    -d '{"query":"hello"}' | jq -e '.error == "noDictionaries"' >/dev/null
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Origin: https://evil.example' \
    "http://127.0.0.1:$PORT/v2/status")
[ "$CODE" = 403 ] || { echo "error: cross-origin status was $CODE" >&2; exit 1; }
printf 'HTTP v2 smoke passed for %s (%s)\n' "$VERSION" "$COMMIT"
