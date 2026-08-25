# Release engineering

`scripts/release.sh` is the only human-facing release entry point. Build
correctness lives in repository scripts and GitHub Actions, not in personal
notes or unpublished tooling.

## State machine

```text
development
  → prepare VERSION
  → review/commit/push main
  → main CI success
  → annotated tag
  → read-only validate-build job creates official candidate artifacts once
  → verified draft GitHub Release
  → public GitHub-enforced immutable Release
  → appcast metadata commit on main
  → Homebrew tap formula commit
  → remote verification
```

The tag commit is the source of every plugin and server artifact. The later
appcast commit is distribution metadata and is intentionally not the tag.

## Version policy

`VERSION` is the authoritative product version. `plugin/info.json`, the server
fallback version, and explicit documentation examples must agree with it.
Product versions and the local HTTP API version are independent. A 1.x product
continues to use API v2 until the wire contract has a genuine breaking change.

## Commands

```bash
./scripts/release.sh doctor
./scripts/release.sh bootstrap
./scripts/release.sh dev
./scripts/release.sh build
./scripts/release.sh check
./scripts/release.sh prepare 1.0.1
./scripts/release.sh publish
./scripts/release.sh status
```

- `doctor` is read-only and checks local tools plus GitHub CLI/SSH identity.
- `bootstrap` idempotently creates the public `wakewon/homebrew-tap` when
  absent and configures its dedicated write deploy key. `--rotate` deletes only
  that named key and replaces the paired Actions secret.
- `dev` runs focused tests, builds a server, safely deploys the standalone
  development LaunchAgent, builds the plugin, and verifies runtime identity.
  Dirty builds are labelled `<commit>-dirty`.
- `build` cleans ignored `release/`, builds every artifact, and verifies that
  tracked working-tree state is exactly unchanged.
- `check` is the strict clean-main, origin equality, tests, race, deterministic
  security guard, workflow policy, full-history Gitleaks, installer, Homebrew,
  HTTP, and build gate.
- `prepare` is the only command allowed to modify tracked product-version
  metadata. It never tags.
- `publish` reruns `check`, requires successful CI for the exact HEAD, creates
  and pushes one annotated tag, and waits for the Release workflow. `--yes`
  supplies explicit non-interactive confirmation.
- `status` reports version/API, branch/tree, local/remote commits, tag, CI,
  Release, appcast, installed runtime, and tap state without secrets.

## Credential and permission model

The workflow denies permissions by default and splits release duties so build
and test processes never receive a repository write credential:

| Job | Repository permission | Credential exposure |
| --- | --- | --- |
| `validate-build` | `contents: read` | no `GH_TOKEN`, no deploy key; checkout credentials are not persisted |
| `publish-release` | `contents: write` | `GH_TOKEN` only in the Release mutation step |
| `update-appcast` | `contents: write` | `GH_TOKEN` only in the bounded Git push step |
| `publish-homebrew` | `contents: read` | tap deploy key only in the formula-push step; no main-repository write token |
| `verify-distribution` | `contents: read` | no write credential and no secret |

Ordinary CI also uses `contents: read`, never receives release secrets, and
sets `persist-credentials: false` on every checkout. The release build job does
the same. GitHub publication jobs never rebuild the product; they consume the
single candidate transferred by GitHub Actions artifacts.

Homebrew publication uses the Actions secret:

```text
HOMEBREW_TAP_DEPLOY_KEY
```

It is an ED25519 deploy key attached only to `wakewon/homebrew-tap`, with write
access to that repository and no account-wide capability. No classic or
fine-grained PAT is required. The workflow writes it to a mode-0600 file under
`$RUNNER_TEMP`, disables shell tracing, never prints it, and removes it after
use. A missing half of the key/secret pair is not guessed or recovered; use:

```bash
./scripts/release.sh bootstrap --rotate
```

Bootstrap and Actions SSH commands use `scripts/github-known-hosts`,
`StrictHostKeyChecking=yes`, `IdentitiesOnly=yes`, and an isolated SSH config.
The committed RSA, ECDSA, and Ed25519 host keys come from GitHub's official SSH
fingerprint documentation and Meta API. Neither bootstrap nor the workflow
reads or modifies a user's `~/.ssh/known_hosts`.

## Preflight, tag, and official build

Prepare the version and release notes, review the diff, then commit and push:

```bash
./scripts/release.sh prepare 1.0.1
git commit -am "Prepare v1.0.1"
git push origin main
```

Wait for CI success for that exact commit. Then:

```bash
./scripts/release.sh check
./scripts/release.sh publish
```

The tag workflow validates `vX.Y.Z == VERSION`. Its read-only `validate-build`
job runs full tests and is the only official artifact producer. Publication
jobs consume those exact bytes, create and verify a draft Release, publish it,
update appcast with a bounded non-force push, publish the formula using only the
tap deploy key, and perform a clean Homebrew install test.

`scripts/extract-release-notes.sh` deterministically extracts only the current
`## [X.Y.Z]` CHANGELOG section. Release creation never submits the entire
CHANGELOG, and extraction tests cover current, synthetic future, missing, and
adjacent-version cases.

`SHA256SUMS` lists only public assets and not itself. `RELEASE_MANIFEST.json`
contains version, API, exact build commit, tag, filenames, and SHA-256 values;
it contains no local paths or user information.

## Appcast and Homebrew

Unpublished versions never enter appcast. After a successful Release, the
workflow adds the real version, official plugin SHA, tag build commit, official
asset URL, Bob minimum version, and release timestamp to `main`. It never
force-pushes. The Homebrew formula is rendered from
`packaging/homebrew/bob-mdict.rb.tmpl`; builds never modify the template.

The tap workflow changes only `Formula/bob-mdict.rb` (and removes the initial
`.gitkeep`). Remote verification installs `wakewon/tap/bob-mdict`, checks
version/API/build identity, and runs `--check` with an empty dictionary folder.

## Failures and recovery

- Before tag push: fix the problem and rerun; no public state exists.
- After tag push but before any public Release: diagnose infrastructure first.
  A workflow rerun removes only its unpublished draft and rebuilds it. A failed
  tag may be deleted only after positively confirming no Release was ever
  public and nobody depends on it.
- After a GitHub Release is public: never delete, replace, or reuse that
  version. A workflow rerun may resume idempotent appcast/tap verification using
  the already-public bytes, but product fixes must move forward with `1.0.1` or
  the next appropriate version.
- If appcast push races with main, the workflow rebases and retries a bounded
  number of times. It never force-pushes main.
- If a public Release is complete but a later distribution step fails, fix the
  workflow on `main` and resume it with
  `gh workflow run Release --ref main -f release_tag=vX.Y.Z`. The recovery run
  checks out the existing tag and reuses the public Release bytes; it never
  replaces published assets.
- If tap authentication is incomplete, rotate only the dedicated deploy key;
  do not create a broad PAT.

Recovery dispatch accepts only a semver-formatted existing tag whose commit and
`VERSION` match. It refuses to create a Release. When the Release is already
public it downloads and verifies those public bytes; newly rebuilt verification
bytes are never uploaded over them.

## Supply-chain checks

`scripts/security-check.sh` is the fast deterministic guard for dictionary
data, private-key markers, GitHub token prefixes, and AWS access-key prefixes.
Gitleaks 8.30 or newer supplies generic detection. Ordinary CI scans the
tracked/non-ignored working tree; `release.sh check` and `validate-build` scan
the complete Git history with `--redact`. The single allowlist is constrained
to one generic-api-key rule, one test file, and one synthetic path-traversal
fixture. Release operators must also scan the complete `homebrew-tap` history.

Only official `actions/*` Actions are used. Each `uses:` reference is pinned to
a full, GitHub-verified commit SHA with its semantic release in a comment:

| Action | Version | Commit |
| --- | --- | --- |
| `actions/checkout` | `v7.0.1` | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | `v7.0.0` | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
| `actions/upload-artifact` | `v7.0.1` | `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` |
| `actions/download-artifact` | `v8.0.1` | `3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c` |

Before updating a pin, resolve the desired official release tag through the
`actions/<name>` GitHub repository/API, confirm the full commit and verified
signature, update the semantic-version comment, then run
`scripts/verify-workflow-security.sh` and `actionlint`.

## Release immutability

Repository-level GitHub-enforced immutable releases were enabled on
2026-08-25. GitHub applies this setting only to releases published afterward:
their tags and assets are platform-locked when a verified draft is published.
The earlier `v1.0.0` API record remains `immutable: false`; it follows the
project policy that its public tag and assets are never deleted, moved, or
replaced. Rollback always means publishing a corrected new version.
