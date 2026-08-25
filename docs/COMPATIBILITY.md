# Compatibility

## Formats

| | Status |
|---|---|
| MDX v1.x / v2.x | Supported |
| MDD, and multi-volume `.mdd` / `.1.mdd` / `.2.mdd` | Supported, discovered by filename |
| zlib-compressed blocks | Supported |
| LZO1X-compressed blocks | Supported |
| UTF-8 and UTF-16 encodings | Supported |
| Encrypted key blocks (RIPEMD-128) | Supported by the underlying engine |
| `@@@LINK=` redirects | Followed, with loop detection |
| `sound://`, `snd://`, `file://`, bare paths | Resolved against the MDD index |
| `entry://` cross-references | Surfaced through Bob's structured related words |
| MP3 / WAV / OGG audio | Served directly |
| Ogg-Speex (`.spx`) audio | Transcoded to WAV, cached on disk (needs `speexdec`) |
| Images and CSS in MDD | Addressable through the resource endpoint |
| Missing MDD | Degrades gracefully — definitions keep working, no audio is offered |
| Corrupt dictionary | Isolated and marked unavailable; others keep working |

## Verified dictionaries

Measured by `TestCompatibilityMatrix` against real dictionaries. The table
records **capabilities only** — no dictionary content is reproduced here or
anywhere in this repository.

| Capability | Collins COBUILD overhaul V2.30 | LDOCE5++ En-Cn V2.15 | 牛津高阶英汉双解词典（第8版） | Oxford Living Dictionaries (ODE) |
|---|---|---|---|---|
| MDX open | ✅ | ✅ | ✅ | ✅ |
| Metadata | ✅ | ✅ | ✅ | ✅ |
| Exact lookup | ✅ | ✅ | ✅ | ✅ |
| Normalized lookup | ✅ | ✅ | ✅ | ✅ |
| HTML decode | ✅ | ✅ | ✅ | ✅ |
| `@@@LINK` redirects | — | ✅ | — | — |
| MDD detected | ✅ | ✅ | ✅ | — |
| Audio found | ✅ | ✅ | ✅ | — |
| UK/US resolved to different files | ✅ | ✅ | ✅ | — |
| IPA | ✅ | ✅ | ✅ | ✅ |
| Part of speech | ✅ | ✅ | ✅ | ✅ |
| Definitions | ✅ | ✅ | ✅ | ✅ |
| Translations | ✅ | ✅ | ✅ | — |
| Examples | ✅ | ✅ | ✅ | ✅ |
| Inflected forms | ✅ | — | ✅ | — |
| Phrases / idioms | ✅ | ✅ | ✅ | ✅ |
| Cross-references | ✅ | ✅ | ✅ | ✅ |
| Images in MDD | 1 733 | 15 | 1 305 | — |
| Audio files in MDD | 101 727 | 183 907 | 100 211 | — |
| Parser | `collins-cobuild-overhaul` | `ldoce5pp` | `oald8` | `ode-living-online` |

A fifth profile, `oxford-xml-learner`, covers the Oxford learner's builds that
ship the publisher's own XML element names (`sn-g`, `def`, `chn`, `pron-g`)
rather than CSS classes. It is a *family* profile: two unrelated repacks in the
survey corpus share that template exactly, and both go from no recoverable
structure to full sense, translation, example, label, idiom and UK/US
pronunciation extraction under it.

Entry counts: 141 091 / 283 110 / 109 476 / 464 360 headwords — 998 037 total.

Regenerate against your own library:

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries \
BOB_MDICT_MATRIX_OUT=/tmp/out \
  go test ./internal/service -run TestCompatibilityMatrix -v
```

## Unknown dictionaries

The table above measures dictionaries that have a profile. The question a new
user actually has is different: what happens to a dictionary nobody has ever
looked at?

To answer it, the generic parser was surveyed against a private local corpus of
**99 MDX files** — English, Chinese, Japanese, Korean, German, French, Italian,
Arabic and Malay; monolingual, bilingual, learner, unabridged, encyclopedic,
thesaurus and terminology titles — using `bob-mdict --diagnose-all`. The corpus
is the developer's own and is never committed; only these aggregate numbers are.

| | Before numbering | After numbering | After validation |
|---|---:|---:|---:|
| Dictionaries analysed | 99 | 99 | 99 |
| Opened and indexed | 96 | 96 | 96 |
| Mean structural coverage | 22.0 % | 56.1 % | 54.2 % |
| Mean fallback rate | 77.9 % | 43.8 % | 45.7 % |
| Produced no structure at all | 73 | 24 | 25 |
| Yielded definitions | 22 | 72 | 71 |
| Yielded parts of speech | 13 | 43 | 42 |
| Yielded examples | 11 | 27 | **44** |
| Yielded translations | 1 | 15 | **26** |
| Yielded IPA | 73 | 73 | **45** |
| Yielded cross-references | 0 | 12 | 12 |
| Flagged for manual inspection | 91 | 58 | 51 |

**Coverage is not accuracy, and the last column is the proof.** Structural
coverage went slightly *down* between the second and third columns while the
parse got substantially better, because the third column is the first one
measured after the output was actually read.

Two of those movements are worth stating outright:

- **IPA falls from 73 dictionaries to 45.** The IPA character class contained
  `y`, which is the close front rounded vowel and also the twenty-fifth letter
  of the English alphabet. Every heading, label and section title ending in one
  was being reported as a transcription. Forty-five is the honest number.
- **Examples rise from 27 to 44 and translations from 15 to 26.** Both come
  from reading real records: many dictionaries keep the definition in one
  element and its examples in the elements after it, and many fuse an English
  definition and its Chinese gloss into a single text node.

Coverage is not the measure the third column was tuned against. That was
**content retention** — how much of a record the parse accounts for — which is
reported separately below.

## Validating the output

A second survey runs the real service and the real Bob adapter over 1 132
records from the same corpus and measures what happens to them. Dictionaries
are weighted by what this project is for: Chinese on either side first, English
monolingual second.

| tier | dictionaries | records | retention | duplication |
|---|---:|---:|---:|---:|
| A · Chinese | 40 | 625 | 82 % | 3.3 % |
| B · English monolingual | 26 | 312 | 73 % | 2.7 % |
| C · English ↔ other | 15 | 120 | 68 % | 6.4 % |
| D · other lexical | 10 | 60 | 95 % | 3.8 % |
| E · reference / non-lexical | 5 | 15 | 78 % | 1.0 % |

Mean content retention across the corpus rose from **68 % to 79 %**, and on
Chinese-related dictionaries from **66 % to 82 %**, against a baseline measured
with the same harness before any of this round's parser changes. Of 1 132
records, 202 were classified as likely improvements and 9 as possible
regressions; every one of the nine is a case where confidently wrong structure
was replaced by a fallback that keeps far more of the record.

Every backend invariant held on every record: the EntrySet is keyed by the key
the MDX matched, record ordinals stay consecutive, duplicate records stay
distinguishable, sense order survives into the Bob result, every semantic field
reaches both presentations, and no token appears in the Markdown rendering that
is absent from the IR.

**Retention and duplication are not accuracy either.** They detect the shapes a
wrong parse takes — text that vanished, text emitted twice — not whether the
text that survived was understood. The hundred-record review queue those
numbers rank is where a human reads the entries themselves.

## What is still weak, and what is correct

Three of the 99 files could not be opened at all. Their key-block info is not
zlib-compressed, which is what a newer container revision or an unsupported
key-block encryption looks like from here. They are isolated and reported as
unavailable; the other 96 are unaffected. MDict v3 is not implemented.

**A high fallback rate is often the right answer.** Roughly a quarter of the
corpus is terminology banks, name lists, etymology dictionaries and
encyclopedias whose records are one prose body under a headword: there is no
sense structure to recover, and reporting the entry honestly as unparsed
content beats inventing divisions in it. An article-style reference work is
deliberately sampled three times rather than sixteen, and is kept out of the
review queue, so its fallbacks cannot drown the dictionaries this project
exists for.

Known imprecisions that remain, all visible in the review snapshots:

- A collocations or thesaurus dictionary's lists are recovered and attached to
  the right sense, but filed as **examples**. There is no generic evidence that
  distinguishes a collocation list from a citation list, and losing them was
  worse than labelling them approximately.
- Dictionaries that print a **sense menu** at the top of a long entry and then
  the entry itself yield both, so the short menu entries appear alongside the
  full senses.
- Material printed **after** the last sense is attached only when the dictionary
  has already shown that shape of element to be an example. Otherwise it is left
  where it is rather than swallowed, which loses it from a structured parse.

Reproduce against your own library:

```bash
bob-mdict --dictionary-dir /path/to/mdxs --diagnose-all --diagnose-out /tmp/corpus
bob-mdict --dictionary-dir /path/to/mdxs --validate-all --validate-out /private/review
```

## Known boundaries

- **ODE has no MDD**, so it has no audio. This is correct behaviour, not a
  failure: without a recording, nothing is offered.
- **LDOCE5++ carries a single RP transcription.** Its profile assigns that
  transcription to UK and leaves US with audio only, rather than printing a
  British transcription under an American flag.
- **Collins labels its phrase senses as a part of speech** (`PHRASE`), so they
  appear as a part rather than under Idioms. The content is present either way.
- **Images are addressable but not displayed.** Bob's `toDict` has no image
  slot; the resource endpoint serves them for any future use.
- **No full-text search.** Lookup is exact, normalized, case-insensitive, with
  optional prefix suggestions. Morphological fallback (`ran` → `run`) is not
  implemented; most dictionaries already carry inflected headwords as redirects.
- **Case-sensitive headwords are preserved.** An exactly cased key wins;
  case-insensitive matching is used only after an exact miss. Canonically
  equivalent NFC/NFD input shares identity without folding case. Bob displays
  and navigates with the actual selected MDX key, so fallback input casing does
  not fabricate a second headword.
- **Duplicate exact keys preserve record boundaries.** Resolved byte-identical
  records are safely deduplicated; every remaining non-empty record is parsed
  independently. Separate mode shows one record with sibling navigation;
  Combined mode keeps all ordinal-labelled records in one Bob card.

## Performance

Apple M4, measured on 2026-08-25 against the four dictionaries above: 998 037
headwords across ~2.7 GB of source files. Values are medians or representative
steady readings from the final 1.0.0 code; no dictionary content was captured.

| | |
|---|---|
| Cold start (discover + index everything) | 0.60 s benchmark; 0.77 s CLI rescan including process setup/cleanup |
| Lookup, one dictionary | 3.5 ms |
| Lookup, all four dictionaries, uncached | 29.6 ms |
| Lookup, repeated (entry cache) | 54 µs |
| MDD audio resolution | 7.2 µs |
| Ogg-Speex transcode | once per file, then cached on disk |

Resident memory, measured on the running process rather than reported by the
runtime:

| Library | Headwords | RSS |
|---|---|---|
| All four, after rescan | 998 037 | 260 MB |
| All four, after representative queries | 998 037 | 325 MB |

Indexes are held in memory so that lookups are instant without a database. The
cost scales with headword count. The table deliberately reports only the final
four-dictionary configuration remeasured for 1.0.0 rather than retaining older
per-dictionary values from a different build.

Two things keep that number down: the service builds only the indexes it needs
rather than the three the bundled engine would build, and GC is tuned for a
process whose live set is large and static while its garbage is short-lived.
Both are described in [ARCHITECTURE.md](ARCHITECTURE.md).

Reproduce with:

```bash
go test ./internal/service -run '^$' -bench . -benchmem
```
