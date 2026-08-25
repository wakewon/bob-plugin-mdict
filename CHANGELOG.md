# Changelog

All notable product changes are recorded here. Product versions and the local
HTTP API version are independent; MDict for Bob 1.0.0 continues to use API v2.

## [Unreleased]

- Recover senses from visible numbering, ordered and definition lists, and
  repeated definition blocks when a dictionary's class names say nothing. A
  survey of 99 unknown MDX dictionaries went from 22 % mean structural coverage
  to 56 %, with 50 dictionaries improved and none regressed.
- Extract bilingual glosses without a profile, from the scripts in play and the
  script the headword is written in.
- Detect an entry's own headword from headword-class evidence, and decline a
  heading that has nothing to do with the key the record was found under.
- Read a pronunciation block's own `BrE`/`NAmE` label in preference to a
  neighbour's, so the American half of a pair no longer inherits the British
  label printed above it.
- Fingerprint dictionaries from representative records strided across the key
  index instead of a fixed list of English probe words, so non-English
  dictionaries are recognised at all, and require several records to agree
  before a profile is applied.
- Add `oxford-xml-learner`, a reusable family profile for Oxford learner's
  builds that ship publisher XML element names rather than CSS classes.
- Add `--diagnose`, `--diagnose-all` and `--parser` for inspecting how well an
  unknown dictionary is understood. Reports carry structure and counts only,
  never dictionary text.
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
