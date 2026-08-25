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

| | Before this round | After |
|---|---:|---:|
| Dictionaries analysed | 99 | 99 |
| Opened and indexed | 96 | 96 |
| Mean structural coverage | 22.0 % | 56.1 % |
| Mean fallback rate | 77.9 % | 43.8 % |
| Produced no structure at all | 73 | 24 |
| Yielded definitions | 22 | 72 |
| Yielded parts of speech | 13 | 43 |
| Yielded examples | 11 | 27 |
| Yielded translations | 1 | 15 |
| Yielded cross-references | 0 | 12 |
| Flagged for manual inspection | 91 | 58 |

Fifty dictionaries improved and none regressed. The four profiled dictionaries
above produce a byte-identical capability matrix before and after.

Three of the 99 files could not be opened at all. Their key-block info is not
zlib-compressed, which is what a newer container revision or an unsupported
key-block encryption looks like from here. They are isolated and reported as
unavailable; the other 96 are unaffected. MDict v3 is not implemented.

**Coverage is not accuracy.** These are counts of samples that produced
structure of a given kind, not of samples that produced *correct* structure.
Nothing here has been compared against a human reading of an entry.

A high fallback rate is often the right answer. Roughly a quarter of the corpus
is terminology banks, name lists, etymology dictionaries and encyclopedias whose
records are one prose body under a headword: there is no sense structure to
recover, and reporting the entry honestly as unparsed content beats inventing
divisions in it.

Reproduce against your own library:

```bash
bob-mdict --dictionary-dir /path/to/mdxs --diagnose-all --diagnose-out /tmp/corpus
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
