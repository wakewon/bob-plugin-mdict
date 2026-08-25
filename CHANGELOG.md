# Changelog

All notable product changes are recorded here. Product versions and the local
HTTP API version are independent; MDict for Bob 1.0.0 continues to use API v2.

## [Unreleased]

- Attach the material between one sense and the next to the sense that opens
  it. Many dictionaries keep the definition in one element and its examples in
  the elements after it, where no sense contains them and nothing read them.
  Examples now come out of 44 of 96 surveyed dictionaries rather than 27.
- Separate a bilingual gloss fused into a definition's own text, where there is
  no element boundary to lift it out of. Translations now come out of 26 of 96
  surveyed dictionaries rather than 15, and the signal for a bilingual entry
  with no translation fell from 15 dictionaries to 4.
- Stop reading ordinary words as phonetic transcription. The IPA character set
  included `y`, which is both the close front rounded vowel and the
  twenty-fifth letter of the English alphabet, so every heading and label
  ending in one was reported as IPA.
- Decline a numbered list that is a table of contents rather than a sense list:
  blocks made entirely of link text, and numbering that accounts for almost
  none of the record.
- Decline a "sense list" whose members contain one another, which is what
  unclosed tags parse into, and which emitted the whole entry once per sense.
- Keep a sense that has exactly one subsense instead of discarding it into its
  parent's definition, and keep a block too large to be a meaning as untyped
  content instead of presenting it as a definition.
- Check a headword claimed by a `headword` or `entry_title` class against the
  key the record was found under, so a page banner reading "Definition of
  'below'" is no longer the entry's name.
- Fall back to script evidence when a profile's translation selector matches
  nothing in a record, which is what a repack of a profiled dictionary looks
  like once it has renamed that one class.
- Add `--validate` and `--validate-all`: an end-to-end review of what the
  parser produced, measured through the real service and the real Bob adapter
  and written out as ranked Markdown snapshots. Content retention across the
  surveyed corpus rose from 68 % to 79 %, and on Chinese-related dictionaries
  from 66 % to 82 %.
- Add an experimental Markdown renderer for the canonical EntrySet, a sibling
  of the Bob adapter rather than a second conversion path. It is a development
  and validation surface in this release and is not part of API v2.
- Recover senses from visible numbering, ordered and definition lists, and
  repeated definition blocks when a dictionary's class names say nothing. A
  survey of 99 unknown MDX dictionaries went from 22 % mean structural coverage
  to 54 %, and from 73 dictionaries yielding no structure at all to 25.
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
