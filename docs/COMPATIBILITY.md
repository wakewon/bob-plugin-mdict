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
| Phrases / idioms | — | ✅ | ✅ | ✅ |
| Cross-references | ✅ | ✅ | ✅ | ✅ |
| Images in MDD | 1 733 | 15 | 1 305 | — |
| Audio files in MDD | 101 727 | 183 907 | 100 211 | — |
| Parser | `collins-cobuild-overhaul` | `ldoce5pp` | `oald8` | `ode-living-online` |

Entry counts: 141 091 / 283 110 / 109 476 / 464 360 headwords — 998 037 total.

Regenerate against your own library:

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries \
BOB_MDICT_MATRIX_OUT=/tmp/out \
  go test ./internal/service -run TestCompatibilityMatrix -v
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
  independently and shown in one Bob card with superscript record ordinals.

## Performance

Apple M4, measured against the four dictionaries above: 998 037 headwords across
~2.7 GB of source files.

| | |
|---|---|
| Cold start (discover + index everything) | 0.62 s |
| Lookup, one dictionary, uncached | 5.3 ms |
| Lookup, all four dictionaries, uncached | 30 ms |
| Lookup, repeated (entry cache) | 0.23 ms |
| MDD audio resolution | 27 µs |
| Ogg-Speex transcode | once per file, then cached on disk |

Resident memory, measured on the running process rather than reported by the
runtime:

| Library | Headwords | RSS |
|---|---|---|
| One medium dictionary (OALD8) | 109 476 | 50 MB |
| One large dictionary (LDOCE5++, 1.3 GB MDD) | 283 110 | 105 MB |
| All four, at rest | 998 037 | 253 MB |
| All four, after sustained lookups | 998 037 | 313 MB (plateaus) |

Indexes are held in memory so that lookups are instant without a database. The
cost scales with headword count, so a typical user with one or two dictionaries
sits well under 100 MB.

Two things keep that number down: the service builds only the indexes it needs
rather than the three the bundled engine would build, and GC is tuned for a
process whose live set is large and static while its garbage is short-lived.
Both are described in [ARCHITECTURE.md](ARCHITECTURE.md).

Reproduce with:

```bash
go test ./internal/service -bench . -run XXX
```
