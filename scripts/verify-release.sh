#!/bin/bash
# Verify the complete local release rehearsal and its public-asset boundary.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
source scripts/lib-release.sh

VERSION=$(release_version)
COMMIT=$(release_build_commit)
OUT_DIR="${RELEASE_DIR:-release}"

scripts/verify-plugin.sh "$OUT_DIR/MDict-v$VERSION.bobplugin" "$COMMIT"
scripts/verify-installer.sh "$OUT_DIR/bob-mdict-$VERSION-macos-installer.tar.gz"

for arch in arm64 amd64; do
    tar -tzf "$OUT_DIR/bob-mdict-$VERSION-darwin-$arch.tar.gz" | grep -qx 'bob-mdict'
done

HOST_ARCH=$(uname -m | sed 's/x86_64/amd64/')
IDENTITY=$("$OUT_DIR/bob-mdict-darwin-$HOST_ARCH" --version)
printf '%s' "$IDENTITY" | grep -Fq "bob-mdict $VERSION ($COMMIT) api=v2"

jq -e --arg version "$VERSION" --arg commit "$COMMIT" \
    '.version==$version and .apiVersion=="v2" and .buildCommit==$commit and .tag==("v"+$version)' \
    "$OUT_DIR/RELEASE_MANIFEST.json" >/dev/null
jq -r '.artifacts[] | [.filename,.sha256] | @tsv' "$OUT_DIR/RELEASE_MANIFEST.json" | \
    while IFS=$'\t' read -r name expected_sha; do
        actual_sha=$(shasum -a 256 "$OUT_DIR/$name" | awk '{print $1}')
        [ "$actual_sha" = "$expected_sha" ] || { echo "error: manifest SHA mismatch for $name" >&2; exit 1; }
    done

(cd "$OUT_DIR" && shasum -a 256 -c SHA256SUMS)
EXPECTED=$(release_public_assets "$VERSION" | sort)
ACTUAL=$(awk '{print $2}' "$OUT_DIR/SHA256SUMS" | sort)
[ "$EXPECTED" = "$ACTUAL" ] || {
    echo "error: SHA256SUMS does not list exactly the public assets" >&2
    diff <(printf '%s\n' "$EXPECTED") <(printf '%s\n' "$ACTUAL") || true
    exit 1
}

if grep -qE '@(VERSION|ARM64_SHA256|AMD64_SHA256)@' "$OUT_DIR/bob-mdict.rb"; then
    echo "error: Homebrew formula contains an unresolved placeholder" >&2
    exit 1
fi
# Homebrew's global RuboCop process also loads installed tap formulae. When the
# same released formula is already tapped locally, its DuplicateMethods cop
# reports the two class copies as if one file defined methods twice. Check the
# current file's method names directly, then skip only that environment-global
# cop while retaining every other Homebrew style rule.
DUPLICATE_METHODS=$(sed -nE 's/^  def ([^ (]+).*/\1/p' "$OUT_DIR/bob-mdict.rb" | sort | uniq -d)
[ -z "$DUPLICATE_METHODS" ] || {
    printf 'error: duplicate formula methods:\n%s\n' "$DUPLICATE_METHODS" >&2
    exit 1
}
brew style --except-cops Lint/DuplicateMethods "$OUT_DIR/bob-mdict.rb"
scripts/http-smoke.sh "$OUT_DIR/bob-mdict-darwin-$HOST_ARCH"
scripts/security-check.sh
printf 'release rehearsal verified for %s (%s)\n' "$VERSION" "$COMMIT"
