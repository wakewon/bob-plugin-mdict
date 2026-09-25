# Changelog

All notable product changes are recorded here. Product versions and the local
HTTP API version are independent; MDict for Bob 1.2.0 continues to use API v2.

## [Unreleased]

- Add a **Markdown (web layout)** presentation that shows the dictionary's own
  page instead of the parsed entry. The service reads the record's HTML with
  the dictionary's stylesheets (beside the MDX, then in the MDD), applies what
  decides what a reader sees — display, visibility, emphasis, list markers,
  horizontal spacing and `::before`/`::after` text — and converts the result
  with `html-to-markdown`. Layout tables are flattened, icon-font glyphs are
  dropped, and nothing is fetched from the network. Record boundaries and
  `Other entries` selectors match the structured Markdown. The API adds
  `markdownSource: "html"` to `format: "markdown"`; older v2 services ignore it.
- Report stylesheets a dictionary links to but that are not installed, as
  `missingStylesheets` in `/v2/dictionaries` and `缺少样式表` in `/list`.
  Profiles may declare `presentationCSS` to hide interface chrome in the web
  layout; the built-in profiles hide LDOCE's menu bar and collapsed-panel
  labels, Collins' usage-trend chart and ODE's toggle buttons.
- Make dictionary links and `Other entries` in the web layout clickable
  lookups in Bob, and end a page reached that way with a **← word** link back,
  along a path that can be walked back step by step.
- Play 🔊 in both Markdown views in the background instead of opening a
  browser, labelled UK or US in the web layout when the recording's markup
  says which. New settings: pronunciation volume, speed from 0.5× to 1.5×
  with pitch preserved, and loudness matching (ITU-R BS.1770 to -16 LUFS,
  peak-limited to -1 dB).
- Links and playback go through a small helper app the service builds on the
  Mac with `osacompile` and signs locally, so no developer certificate is
  involved. It receives `bobmdict://` links, asks Bob to look words up through
  Bob's AppleScript interface, and plays recordings prepared by the new
  loopback `POST /v2/audio/{token}` in an audio engine it keeps running between
  clicks. Steps through links reach the service by `POST /v2/navigation`.
  Without the helper, links stay text. Bob asks before opening such links
  unless **Opening Links in Translation Results** is set to **Never Ask**.
- Fix recordings and entries at key-block boundaries running into the records
  after them. The MDict engine left the last entry of each key block without
  an end, so OALD8's UK "greeting" played fourteen seconds of other words and
  a headword's HTML could include the entries after it; about 2,100 entries in
  the four development dictionaries were affected.
- Resource tokens are now lowercase base32, which stays intact inside Markdown
  links. Tokens remain opaque; clients that treated them so are unaffected.
- Fix the installer and uninstaller mistaking another launchd job whose name
  contains the service's label for the service.
- Document known issues and limitations in `docs/KNOWN_ISSUES.md`.

## [1.2.0] - 2026-09-25

- Adapt Markdown presentation to Bob 1.21.0's native Markdown rendering. The
  plugin now returns `content: { format: "markdown", text }`; Plain Text and the
  automatic free-form fallback return `format: "plain"`. The same text is still
  sent as one `toParagraphs` element, which older Bob versions read, so the
  plugin keeps `minBobVersion` 1.20.0 and Markdown shows as raw source there.
  Markdown rendering requires Bob 1.21.0+ on macOS 13+.
- Declare the `/list` result as `plain` so Bob 1.21+ does not read its several
  `toParagraphs` elements as `lines` mapped onto the one-line query.
- Update the option description and documentation, which said Bob did not
  render Markdown.
- Keep numbers in a bilingual translation. Dictionaries such as Collins leave
  the digits of a translated sentence outside the gloss elements, so
  `每小时 7元。` used to lose its `7` and read `每小时 元。` in every
  presentation. A number-only text node directly beside a gloss element, with a
  gloss element or the edge of the parent on its other side, is now part of the
  translation. Numbers next to English prose, and a numeral that is itself the
  definition (`14 十四`), are left where they were.

## [1.1.0] - 2026-08-26


- Present a dictionary entry as Plain Text as an alternative to the Bob dictionary card and Markdown. A requested Bob card automatically falls back to Plain Text when the selected view consists solely of free-form entry blocks without typed structure, preserving paragraph, list, and heading boundaries.
- Add `showGrammar` plugin option to hide detailed grammatical qualifiers from presentation without hiding parts of speech, labels, or patterns.
- Compact Bob POS presentation: keep grammar out of Bob's narrow POS column, and recursively flatten senses and subsenses into independent top-level Bob Parts, because Bob has no nested Part schema. Plain Text and Markdown retain the IR's real hierarchical nesting.
- Generic parsing now conservatively recognizes meaningful secondary semantic headings (such as PHRASES, IDIOMS, PHRASAL VERBS, COLLOCATIONS, USAGE, GRAMMAR, SEE ALSO, RELATED) without claiming every heading is lexical structure.
- Add `oxford-collocations` family profile for the tested Oxford Collocations Dictionary / 牛津英语搭配词典 template.

- Present a dictionary entry as Markdown as an alternative to the Bob
  dictionary card, rendered by the service from the same canonical EntrySet the
  card is rendered from. The plugin returns the document as a single
  `toParagraphs` element, which is Bob's documented array-of-strings contract;
  Bob does not currently document Markdown rendering of that content, and this
  release claims no such behaviour.
- Give `重复词条显示方式` meaning in Markdown as well as in the dictionary card.
  Combined renders every record in source order, divided by a `---` thematic
  break; separate renders one record and lists the other records' selectors.
- Render dictionary navigation targets — sibling record selectors, cross
  references, related entries — as copyable query text rather than as links,
  because Bob publishes no Markdown lookup-action contract. The target stays in
  the IR, so a future Bob mechanism replaces the presentation and nothing else.
- Show an inline MDD illustration at its original position in the prose, over
  an opaque loopback resource URL. External, `data:` and filesystem image
  references remain refused.
- Deduplicate a repeated illustration only when the two occurrences are
  adjacent, which is what a publisher's two language views of one figure look
  like once CSS is gone. An illustration the dictionary places twice with
  content between the two is no longer deleted.
- Use a table's own `<th>` cells as its Markdown header instead of promoting
  whichever row came first.
- Test the service under concurrency at all. Bob's requests each arrive on their
  own goroutine and a rescan can land among them, but no test had ever run two
  goroutines at once, so the race detector was inspecting single-threaded code.
  Synthetic fixtures now drive concurrent lookups, a rescan racing lookups in
  flight, and concurrent resource resolution; removing the cache mutex makes
  them report 140 races, so they have teeth. The race suite runs `-short` and
  finishes in seconds instead of exceeding an hour on a large local library.

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
  and written out as ranked Markdown snapshots. Mean content retention across 96 healthy dictionaries (from a 99-dictionary survey) reached approximately 84% over 1,132 validation records, with zero semantic-field preservation failures across the three renderers.
- Render the canonical EntrySet as Markdown in `internal/mdrender`, a sibling
  of the Bob adapter rather than a second conversion path. It backs both the
  `format:"markdown"` presentation above and the validation review snapshots.
- Recover senses from visible numbering, ordered and definition lists, and
  repeated definition blocks when a dictionary's class names say nothing. A survey of 99 unknown MDX dictionaries yielded greatly improved structural recovery.
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

[1.1.0]: https://github.com/wakewon/bob-plugin-mdict/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/wakewon/bob-plugin-mdict/releases/tag/v1.0.0
