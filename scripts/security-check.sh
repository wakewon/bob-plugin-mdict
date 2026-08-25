#!/bin/bash
# Guard tracked source and release artifacts against private data and secrets.
set -euo pipefail

if git ls-files | grep -iE '\.(mdx|mdd)$'; then
    echo "error: tracked dictionary data" >&2
    exit 1
fi
if git ls-files | grep -q '^local_assets/'; then
    echo "error: tracked local_assets content" >&2
    exit 1
fi
if git ls-files --others --exclude-standard | grep -qE '(^|/)local_assets/'; then
    echo "error: untracked local_assets content exists outside the ignored private root" >&2
    exit 1
fi
if git grep -nE 'BEGIN (OPENSSH|RSA|EC|DSA) PRIVATE KEY|github_pat_[A-Za-z0-9_]+|ghp_[A-Za-z0-9]+' -- . ':!scripts/security-check.sh'; then
    echo "error: credential marker found in tracked source" >&2
    exit 1
fi
if [ -d release ]; then
    if find release -type f \( -iname '*.mdx' -o -iname '*.mdd' \) | grep -q .; then
        echo "error: dictionary data found in release/" >&2
        exit 1
    fi
    if grep -RIlE 'BEGIN (OPENSSH|RSA|EC|DSA) PRIVATE KEY|github_pat_|ghp_' release >/dev/null 2>&1; then
        echo "error: credential marker found in release/" >&2
        exit 1
    fi
fi
printf 'security and dictionary-content guards passed\n'
