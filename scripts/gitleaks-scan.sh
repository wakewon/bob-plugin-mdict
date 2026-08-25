#!/bin/bash
# Run Gitleaks with full redaction and report only safe finding metadata.
set -euo pipefail

MODE="${1:-history}"
TARGET="${2:-.}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${GITLEAKS_CONFIG_FILE:-$REPO_ROOT/.gitleaks.toml}"

command -v gitleaks >/dev/null 2>&1 || {
    echo "error: gitleaks is required (brew install gitleaks)" >&2
    exit 2
}
[ -f "$CONFIG" ] || { echo "error: missing Gitleaks config: $CONFIG" >&2; exit 2; }

TEMP=$(mktemp -d)
trap 'rm -rf "$TEMP"' EXIT
REPORT="$TEMP/report.json"
LOG="$TEMP/gitleaks.log"

set +e
case "$MODE" in
    history)
        gitleaks git --redact --no-banner --no-color --config "$CONFIG" \
            --report-format json --report-path "$REPORT" "$TARGET" >"$LOG" 2>&1
        ;;
    working-tree)
        SNAPSHOT="$TEMP/working-tree"
        mkdir -p "$SNAPSHOT"
        (
            cd "$TARGET"
            git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf -
        ) | tar -xf - -C "$SNAPSHOT"
        gitleaks dir --redact --no-banner --no-color --config "$CONFIG" \
            --report-format json --report-path "$REPORT" "$SNAPSHOT" >"$LOG" 2>&1
        ;;
    *)
        echo "error: usage: gitleaks-scan.sh [history|working-tree] [target]" >&2
        exit 2
        ;;
esac
STATUS=$?
set -e

if [ "$STATUS" -eq 0 ]; then
    printf 'Gitleaks %s scan passed: %s\n' "$MODE" "$TARGET"
    exit 0
fi

if [ "$STATUS" -eq 1 ] && [ -s "$REPORT" ]; then
    echo "error: Gitleaks found possible credentials (values fully redacted):" >&2
    jq -r '.[] | ["file=" + .File, "commit=" + (.Commit // "working-tree"), "rule=" + .RuleID] | join(" ")' "$REPORT" >&2
else
    echo "error: Gitleaks scan failed:" >&2
    sed -n '1,120p' "$LOG" >&2
fi
exit "$STATUS"
