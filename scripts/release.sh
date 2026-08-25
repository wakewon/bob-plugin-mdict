#!/bin/bash
# The only human-facing entry point for development and releases.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

usage() {
    cat <<'USAGE'
Usage: ./scripts/release.sh <command> [arguments]

  doctor                 read-only local tool/auth checks
  bootstrap [--rotate]   idempotently create the Homebrew tap and deploy key
  dev                    test, build, deploy and verify a development build
  build                  clean release/, build and verify all release artifacts
  check                  strict release preflight
  prepare <version>      update tracked product-version metadata only
  publish [--yes]        check, tag, push and wait for the release workflow
  status                 show local and remote release state
USAGE
}

doctor() {
    local failed=0
    for name in git gh ssh go node jq zip unzip tar shasum curl brew lipo gitleaks; do
        if command -v "$name" >/dev/null 2>&1; then
            printf 'ok  %-8s %s\n' "$name" "$(command -v "$name")"
        else
            printf 'missing %s\n' "$name"
            failed=1
        fi
    done
    gh auth status || failed=1
    local ssh_output ssh_status
    set +e
    ssh_output=$(ssh -T git@github.com 2>&1)
    ssh_status=$?
    set -e
    if printf '%s' "$ssh_output" | grep -q 'successfully authenticated'; then
        printf 'ok  github ssh authentication (no shell access, as expected)\n'
    else
        printf '%s\n' "$ssh_output" >&2
        [ "$ssh_status" -eq 0 ] || failed=1
    fi
    [ "$failed" -eq 0 ]
}

bootstrap() {
    local rotate=0
    [ "${1:-}" = "--rotate" ] && rotate=1
    doctor
    local tap="wakewon/homebrew-tap"
    local created=0
    if ! gh repo view "$tap" --json nameWithOwner >/dev/null 2>&1; then
        gh repo create "$tap" --public --description "Homebrew formulae for wakewon projects" --add-readme
        created=1
    fi
    gh repo view "$tap" --json nameWithOwner,visibility --jq \
        'select(.nameWithOwner=="wakewon/homebrew-tap" and .visibility=="PUBLIC")' >/dev/null

    local temp
    temp=$(mktemp -d)
    BOOTSTRAP_TEMP="$temp"
    trap 'if [ -n "${BOOTSTRAP_TEMP:-}" ]; then rm -rf "$BOOTSTRAP_TEMP"; fi' EXIT
    gh repo clone "$tap" "$temp/tap" -- --quiet
    if [ "$created" -eq 1 ] || [ ! -d "$temp/tap/Formula" ]; then
        mkdir -p "$temp/tap/Formula"
        touch "$temp/tap/Formula/.gitkeep"
        (
            cd "$temp/tap"
            git add Formula/.gitkeep
            if ! git diff --cached --quiet; then
                git -c user.name='wakewon release bootstrap' -c user.email='actions@users.noreply.github.com' \
                    commit -m 'Initialize Formula directory' >/dev/null
                git push origin HEAD:main
            fi
        )
    fi

    local title="bob-mdict-homebrew-tap-release"
    local key_ids secret_present
    key_ids=$(gh api "repos/$tap/keys" --jq ".[] | select(.title==\"$title\") | .id")
    secret_present=$(gh secret list --repo wakewon/bob-plugin-mdict --json name --jq \
        '.[] | select(.name=="HOMEBREW_TAP_DEPLOY_KEY") | .name')

    if [ "$rotate" -eq 1 ]; then
        if [ -n "$key_ids" ]; then
            printf '%s\n' "$key_ids" | while IFS= read -r id; do
                gh api --method DELETE "repos/$tap/keys/$id"
            done
        fi
        key_ids=""
        secret_present=""
    fi

    if { [ -n "$key_ids" ] && [ -z "$secret_present" ]; } || \
       { [ -z "$key_ids" ] && [ -n "$secret_present" ]; }; then
        rm -rf "$temp"
        echo "error: deploy key/secret state is incomplete; run bootstrap --rotate" >&2
        return 1
    fi

    if [ -z "$key_ids" ]; then
        umask 077
        ssh-keygen -q -t ed25519 -N '' -C "$title" -f "$temp/deploy-key"
        gh secret set HOMEBREW_TAP_DEPLOY_KEY --repo wakewon/bob-plugin-mdict < "$temp/deploy-key"
        local public_key
        public_key=$(awk '{print $1 " " $2 " " $3}' "$temp/deploy-key.pub")
        gh api --method POST "repos/$tap/keys" -f title="$title" -f key="$public_key" -F read_only=false >/dev/null
        [ -f "$REPO_ROOT/scripts/github-known-hosts" ] || {
            echo 'error: missing repository-controlled GitHub host keys' >&2
            return 1
        }
        GIT_SSH_COMMAND="ssh -F /dev/null -i $temp/deploy-key -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=$REPO_ROOT/scripts/github-known-hosts -o GlobalKnownHostsFile=/dev/null" \
            git ls-remote git@github.com:wakewon/homebrew-tap.git HEAD >/dev/null
    fi

    gh api "repos/$tap/keys" --jq ".[] | select(.title==\"$title\" and .read_only==false) | .title" | grep -qx "$title"
    gh secret list --repo wakewon/bob-plugin-mdict --json name --jq '.[].name' | grep -qx HOMEBREW_TAP_DEPLOY_KEY
    rm -rf "$temp"
    BOOTSTRAP_TEMP=""
    trap - EXIT
    printf 'bootstrap ready: public %s, repository-scoped write deploy key, Actions secret HOMEBREW_TAP_DEPLOY_KEY\n' "$tap"
}

clean_release_dir() {
    mkdir -p release
    find release -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
}

build() {
    local before after
    before=$(git status --porcelain --untracked-files=normal)
    clean_release_dir
    scripts/build-server.sh
    scripts/build-plugin.sh
    scripts/build-installer.sh
    scripts/build-homebrew-formula.sh
    scripts/build-manifest.sh
    scripts/verify-release.sh
    after=$(git status --porcelain --untracked-files=normal)
    if [ "$before" != "$after" ]; then
        echo "error: build changed tracked source" >&2
        diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") || true
        return 1
    fi
    printf 'pure build completed; tracked working-tree state is unchanged\n'
}

check_versions() {
    local version
    version=$(release_version)
    release_require_semver "$version"
    [ "$(jq -r '.version' plugin/info.json)" = "$version" ]
    grep -Fq "var Version = \"$version\"" internal/version/version.go
    grep -Fq "Current product version: **$version**" README.md
    grep -Fq "当前产品版本：**$version**" README_CN.md
    grep -Fq 'const APIVersion = "v2"' internal/version/version.go
    grep -Fq "var REQUIRED_API_VERSION = 'v2'" plugin/main.js
    scripts/validate-appcast.sh
}

check() {
    [ -z "$(git status --porcelain --untracked-files=normal)" ] || { echo 'error: working tree is not clean' >&2; return 1; }
    [ "$(git branch --show-current)" = main ] || { echo 'error: release branch must be main' >&2; return 1; }
    git fetch origin main --quiet
    [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || { echo 'error: HEAD != origin/main' >&2; return 1; }
    check_versions
    local unformatted
    unformatted=$(gofmt -l . | grep -v '^local_assets/' || true)
    [ -z "$unformatted" ] || { printf 'error: unformatted Go files:\n%s\n' "$unformatted" >&2; return 1; }
    go vet ./...
    go mod tidy
    git diff --exit-code -- go.mod go.sum
    go test ./... -count=1
    go test ./... -race -count=1
    node --test plugin/main.test.js
    scripts/security-check.sh
    scripts/verify-workflow-security.sh
    scripts/extract-release-notes.test.sh
    scripts/gitleaks-scan.sh history .
    build
    printf 'strict release check passed\n'
}

prepare() {
    local version="${1:-}"
    [ -n "$version" ] || { echo 'error: prepare requires a version' >&2; return 1; }
    release_require_semver "$version"
    [ "$(git branch --show-current)" = main ] || { echo 'error: prepare requires main' >&2; return 1; }
    grep -Fq "## [$version]" CHANGELOG.md || { echo "error: CHANGELOG.md must contain ## [$version]" >&2; return 1; }
    printf '%s\n' "$version" > VERSION
    jq --arg version "$version" '.version=$version' plugin/info.json > plugin/info.json.next
    mv plugin/info.json.next plugin/info.json
    sed -i '' -E "s/var Version = \"[^\"]+\"/var Version = \"$version\"/" internal/version/version.go
    sed -i '' -E "s/Current product version: \*\*[^*]+\*\*/Current product version: **$version**/" README.md
    sed -i '' -E "s/当前产品版本：\*\*[^*]+\*\*/当前产品版本：**$version**/" README_CN.md
    sed -i '' -E "s/\"serviceVersion\": \"[^\"]+\"/\"serviceVersion\": \"$version\"/" docs/API.md
    sed -i '' -E "s/'[0-9]+\.[0-9]+\.[0-9]+-test'/'$version-test'/" plugin/main.test.js
    printf 'prepared product version %s; review, test, commit and push before publish\n' "$version"
}

latest_successful_ci_for_head() {
    local head="$1"
    gh run list --workflow CI --branch main --commit "$head" --limit 20 \
        --json status,conclusion,headSha,url --jq \
        ".[] | select(.headSha==\"$head\" and .status==\"completed\" and .conclusion==\"success\") | .url" | head -n 1
}

publish() {
    local yes=0
    [ "${1:-}" = "--yes" ] && yes=1
    check
    local version tag head ci_url
    version=$(release_version)
    tag="v$version"
    head=$(git rev-parse HEAD)
    ci_url=$(latest_successful_ci_for_head "$head")
    [ -n "$ci_url" ] || { echo "error: no successful main CI for $head" >&2; return 1; }
    ! git rev-parse "$tag" >/dev/null 2>&1 || { echo "error: tag $tag already exists" >&2; return 1; }
    ! gh release view "$tag" >/dev/null 2>&1 || { echo "error: release $tag already exists" >&2; return 1; }
    if [ "$yes" -ne 1 ]; then
        printf 'Publish immutable %s from %s? [y/N] ' "$tag" "$head"
        read -r answer
        [ "$answer" = y ] || [ "$answer" = Y ] || { echo 'cancelled'; return 1; }
    fi
    git tag -a "$tag" -m "MDict for Bob $version"
    git push origin "$tag"
    local run_id
    for _ in $(seq 1 60); do
        run_id=$(gh run list --workflow Release --limit 20 --json databaseId,headSha \
            --jq ".[] | select(.headSha==\"$head\") | .databaseId" | head -n 1)
        [ -n "$run_id" ] && break
        sleep 2
    done
    [ -n "${run_id:-}" ] || { echo 'error: release workflow did not start' >&2; return 1; }
    gh run watch "$run_id" --exit-status
    git fetch origin main --tags --quiet
    printf 'published %s; release workflow %s completed\n' "$tag" "$run_id"
}

status() {
    local version head origin branch tree tag tag_state latest_ci latest_release appcast runtime tap
    version=$(release_version)
    head=$(git rev-parse HEAD)
    origin=$(git rev-parse origin/main 2>/dev/null || printf unavailable)
    branch=$(git branch --show-current)
    tree=clean; [ -z "$(git status --porcelain --untracked-files=normal)" ] || tree=dirty
    tag="v$version"
    tag_state=absent
    if git rev-parse "$tag" >/dev/null 2>&1; then tag_state=present; fi
    latest_ci=$(gh run list --workflow CI --branch main --limit 1 --json status,conclusion,url --jq '.[0] | "\(.status)/\(.conclusion // "-") \(.url)"' 2>/dev/null || printf unavailable)
    latest_release=$(gh release list --repo wakewon/bob-plugin-mdict --limit 1 --json tagName,isDraft,isPrerelease,url --jq '.[0] // "none"' 2>/dev/null || printf none)
    appcast=$(jq -r '.versions[0].version // "empty"' appcast.json)
    runtime=$(curl -fsS --max-time 1 http://127.0.0.1:15321/v2/status 2>/dev/null | jq -r '"\(.serviceVersion) (\(.buildCommit)) api=\(.apiVersion)"' || printf unavailable)
    tap=$(gh repo view wakewon/homebrew-tap --json visibility,url --jq '"\(.visibility) \(.url)"' 2>/dev/null || printf absent)
    printf 'VERSION=%s API=v2\nbranch=%s tree=%s\nHEAD=%s\norigin/main=%s\ntag=%s (%s)\nlatest CI=%s\nlatest release=%s\nappcast latest=%s\nruntime=%s\nHomebrew tap=%s\n' \
        "$version" "$branch" "$tree" "$head" "$origin" "$tag" "$tag_state" "$latest_ci" "$latest_release" "$appcast" "$runtime" "$tap"
}

dev() {
    go test ./internal/mdict ./internal/parser ./internal/bobadapter ./internal/httpapi ./internal/service
    scripts/dev-deploy.sh
    scripts/build-plugin.sh
    scripts/verify-plugin.sh "release/MDict-v$(release_version).bobplugin" "$(release_build_commit)"
    printf 'development chain completed\n'
}

COMMAND="${1:-}"
shift || true
case "$COMMAND" in
    doctor) doctor "$@" ;;
    bootstrap) bootstrap "$@" ;;
    dev) dev "$@" ;;
    build) build "$@" ;;
    check) check "$@" ;;
    prepare) prepare "$@" ;;
    publish) publish "$@" ;;
    status) status "$@" ;;
    *) usage; exit 2 ;;
esac
