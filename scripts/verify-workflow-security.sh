#!/bin/bash
# Static least-privilege contract for GitHub Actions and release SSH usage.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CI="$REPO_ROOT/.github/workflows/ci.yml"
RELEASE="$REPO_ROOT/.github/workflows/release.yml"
KNOWN_HOSTS="$REPO_ROOT/scripts/github-known-hosts"

fail() { echo "error: workflow security contract: $*" >&2; exit 1; }
require_text() {
    local file="$1" pattern="$2" description="$3"
    grep -Eq "$pattern" "$file" || fail "$description"
}
reject_text() {
    local pattern="$1" description="$2"
    if grep -RInE "$pattern" "$CI" "$RELEASE" "$REPO_ROOT/scripts/release.sh" >/dev/null; then
        fail "$description"
    fi
}
job_block() {
    local job="$1"
    awk -v marker="  $job:" '
        $0 == marker { inside=1; print; next }
        inside && /^  [A-Za-z0-9_-]+:$/ { exit }
        inside { print }
    ' "$RELEASE"
}
require_job_permission() {
    local job="$1" permission="$2" block
    block=$(job_block "$job")
    [ -n "$block" ] || fail "missing release job $job"
    printf '%s\n' "$block" | grep -Eq '^    permissions:$' || fail "$job has no explicit permissions"
    printf '%s\n' "$block" | grep -Eq "^      contents: $permission$" || fail "$job must use contents: $permission"
}
verify_checkout_persistence() {
    local file="$1"
    awk '
        /uses: actions\/checkout@[0-9a-f]{40}/ { waiting=1; next }
        waiting && /persist-credentials: false/ { waiting=0; count++; next }
        waiting && /^      - / { exit 1 }
        END { if (waiting) exit 1 }
    ' "$file" || fail "every checkout in $file must set persist-credentials: false"
}

[ -f "$CI" ] && [ -f "$RELEASE" ] && [ -f "$KNOWN_HOSTS" ] || fail "required policy file is missing"

reject_text 'pull_request_target' 'pull_request_target is forbidden'
reject_text 'StrictHostKeyChecking[=[:space:]]+no' 'StrictHostKeyChecking=no is forbidden'
reject_text 'ssh-keyscan' 'network-fetched SSH host trust is forbidden'

while IFS= read -r use_line; do
    if ! printf '%s\n' "$use_line" | grep -Eq 'uses: actions/(checkout|setup-go|upload-artifact|download-artifact)@[0-9a-f]{40}[[:space:]]+# v[0-9]+\.[0-9]+\.[0-9]+'; then
        fail "Action is third-party, unpinned, or lacks a semantic-version comment: $use_line"
    fi
done < <(grep -RhE '^[[:space:]]*- uses:' "$CI" "$RELEASE")

require_text "$CI" '^permissions:$' 'CI must define workflow permissions'
require_text "$CI" '^  contents: read$' 'CI must use contents: read'
if grep -Eq 'secrets\.|GH_TOKEN|TAP_DEPLOY_KEY|contents: write' "$CI"; then
    fail "CI must not reference secrets or write permissions"
fi
verify_checkout_persistence "$CI"

require_text "$RELEASE" '^permissions: \{\}$' 'Release workflow must deny permissions by default'
require_text "$RELEASE" "^      - 'v\*'$" 'Release workflow must retain the trusted tag trigger'
require_text "$RELEASE" '^  workflow_dispatch:$' 'Release workflow must retain explicit recovery dispatch'
verify_checkout_persistence "$RELEASE"

require_job_permission validate-build read
require_job_permission publish-release write
require_job_permission update-appcast write
require_job_permission publish-homebrew read
require_job_permission verify-distribution read

validate_block=$(job_block validate-build)
homebrew_block=$(job_block publish-homebrew)
verify_block=$(job_block verify-distribution)
publish_block=$(job_block publish-release)
appcast_block=$(job_block update-appcast)

if printf '%s\n' "$validate_block" | grep -Eq 'GH_TOKEN|secrets\.|contents: write'; then
    fail "validate-build must not receive a write token or secret"
fi
if printf '%s\n' "$validate_block" | grep -Eq 'cp .*github-known-hosts|cp .*update-appcast'; then
    fail "build outputs must not supply executable publication logic or SSH trust roots"
fi
if printf '%s\n' "$homebrew_block" | grep -Eq 'GH_TOKEN|contents: write'; then
    fail "publish-homebrew must not receive the main-repository write token"
fi
if printf '%s\n' "$verify_block" | grep -Eq 'GH_TOKEN|secrets\.|contents: write'; then
    fail "verify-distribution must be read-only and secret-free"
fi
if printf '%s\n' "$publish_block$appcast_block" | grep -q 'TAP_DEPLOY_KEY'; then
    fail "GitHub publication jobs must not receive the Homebrew deploy key"
fi

token_count=$(grep -Ec '^          GH_TOKEN: \$\{\{ github\.token \}\}$' "$RELEASE" || true)
[ "$token_count" -eq 2 ] || fail "GH_TOKEN must appear only in the two mutation steps"
if grep -nE '^ {0,9}GH_TOKEN:' "$RELEASE" >/dev/null; then
    fail "GH_TOKEN must be step-scoped, never workflow- or job-scoped"
fi
tap_count=$(grep -Ec '^          TAP_DEPLOY_KEY: \$\{\{ secrets\.HOMEBREW_TAP_DEPLOY_KEY \}\}$' "$RELEASE" || true)
[ "$tap_count" -eq 1 ] || fail "TAP_DEPLOY_KEY must appear exactly once at step scope"
printf '%s\n' "$homebrew_block" | grep -q 'TAP_DEPLOY_KEY' || fail "tap key must be confined to publish-homebrew"
# Literal GitHub expression; shell expansion would make this assertion weaker.
# shellcheck disable=SC2016
printf '%s\n' "$homebrew_block" | grep -q 'ref: \${{ github.workflow_sha }}' || fail "Homebrew host keys must come from the trusted workflow commit"
printf '%s\n' "$appcast_block" | grep -q 'bash scripts/update-appcast.sh' || fail "appcast mutation logic must come from the independently cloned main branch"

require_text "$RELEASE" 'StrictHostKeyChecking=yes' 'Homebrew SSH must require strict host verification'
require_text "$RELEASE" 'UserKnownHostsFile=' 'Homebrew SSH must use the repository known_hosts file'
require_text "$REPO_ROOT/scripts/release.sh" 'StrictHostKeyChecking=yes' 'bootstrap must require strict host verification'

fingerprints=$(ssh-keygen -lf "$KNOWN_HOSTS" -E sha256 | awk '{print $2}' | sort)
expected=$(printf '%s\n' \
    'SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU' \
    'SHA256:p2QAMXNIC1TJYWeIOttrVc98/R1BUFWu3/LiyKgUfQM' \
    'SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s' | sort)
[ "$fingerprints" = "$expected" ] || fail "repository GitHub host keys do not match official fingerprints"


if printf '%s\n' "$appcast_block" | grep -q 'api.github.com/repos'; then
    fail "appcast preparation must not make anonymous REST API calls"
fi

if ! grep -q 'go test \./\.\.\. -short -race -count=1' "$REPO_ROOT/scripts/release.sh"; then
    fail "release.sh must use deterministic -short race gate"
fi

if ! grep -q 'go test \./\.\.\. -short -race -count=1' "$RELEASE"; then
    fail "Release workflow must use deterministic -short race gate"
fi

printf 'workflow least-privilege and supply-chain contract passed\n'
