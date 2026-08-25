# Changelog

All notable product changes are recorded here. Product versions and the local
HTTP API version are independent; MDict for Bob 1.0.0 continues to use API v2.

## [Unreleased]

- Harden release credentials with isolated least-privilege jobs, non-persistent
  checkout authentication, commit-pinned official Actions, pinned GitHub SSH
  host keys, history-aware secret scanning, and deterministic release notes.
- Enable GitHub-enforced immutability for future Releases. The already-published
  v1.0.0 remains governed by the project's never-replace policy.

## [1.0.0] - 2026-08-25

First stable release.

- Reads locally supplied MDX v1/v2 dictionaries and optional multi-volume MDD
  resources without cloud dictionary APIs or telemetry.
- Supports recursive multi-dictionary discovery with stable content-based IDs.
- Preserves senses, subsenses, examples, phrases, cross-references, forms,
  notes, pronunciation provenance, and MDD-backed audio without TTS fallback.
- Preserves duplicate-key record boundaries with Separate sibling navigation
  and an optional Combined presentation.
- Prefers exact-case lookup, uses deterministic fallback, and displays the
  actual selected MDX key rather than fabricating input casing.
- Ships the loopback-only HTTP API v2, opaque MDD resource tokens, request
  limits, Origin checks, and no filesystem-path API.
- Adds reproducible release rehearsal, a self-contained macOS installer,
  official tag-built artifacts, never-replaced Release assets, Bob appcast
  updates, and a repository-scoped Homebrew tap publication path.

[Unreleased]: https://github.com/wakewon/bob-plugin-mdict/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/wakewon/bob-plugin-mdict/releases/tag/v1.0.0
