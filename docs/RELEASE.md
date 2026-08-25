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
  → tag workflow builds official artifacts
  → verified draft GitHub Release
  → public immutable Release
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
- `check` is the strict clean-main, origin equality, tests, race, security,
  real-dictionary-when-present, installer, Homebrew, HTTP, and build gate.
- `prepare` is the only command allowed to modify tracked product-version
  metadata. It never tags.
- `publish` reruns `check`, requires successful CI for the exact HEAD, creates
  and pushes one annotated tag, and waits for the Release workflow. `--yes`
  supplies explicit non-interactive confirmation.
- `status` reports version/API, branch/tree, local/remote commits, tag, CI,
  Release, appcast, installed runtime, and tap state without secrets.

## First bootstrap and permission model

The release workflow uses GitHub's repository token only for the main
repository, with workflow permission `contents: write`. It creates Releases and
pushes the post-release appcast commit. No issues, pull requests, packages,
OIDC, security-event, or Actions write permission is requested.

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

The tag workflow validates `vX.Y.Z == VERSION`, runs full tests, and is the only
official artifact producer. It creates a draft Release, uploads and verifies
the complete asset set, publishes it, verifies remote checksums, updates
appcast from a fresh main checkout with bounded non-force retries, publishes
the formula through the dedicated deploy key, and performs a clean Homebrew
install test.

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
- If tap authentication is incomplete, rotate only the dedicated deploy key;
  do not create a broad PAT.

Published artifacts are immutable. Rollback means publishing a corrected new
version, not silently replacing bytes under an existing tag.
