# MDict for Bob

Current product version: **1.0.0** · local API: **v2**.

English | [简体中文](README_CN.md)

Look up your own local MDict dictionaries in [Bob](https://bobtranslate.com/),
fully offline. Pronunciation audio comes directly from the dictionary's MDD;
this project never generates or uses TTS as a fallback.

> This project is a reader. It ships no dictionary data. You provide and use
> your own `.mdx` and optional `.mdd` files lawfully.

## Highlights

- MDict v1.x/v2.x, recursive discovery, multiple dictionaries and multi-volume
  MDD (`.mdd`, `.1.mdd`, `.2.mdd`, …).
- Completely local: no dictionary API, cloud service, telemetry or outbound
  request. The companion service listens on loopback only.
- Structured entries: POS groups, senses and subsenses, bilingual definitions,
  examples, forms, phrases, idioms, phrasal verbs, cross-references and notes.
- MDD-backed pronunciation only. UK, US, shared and unlabelled provenance is
  preserved. There is no text-to-speech fallback anywhere in the project.
- Simple Bob setup: leave Dictionary ID empty for the first dictionary that
  contains the word, or set one ID to pin that service instance.

## How it works

```text
Bob plugin → http://127.0.0.1:15321 → MDX/MDD → semantic parser
                                              → EntrySet IR → Bob toDict
```

`bob-mdict` is a native Go service that owns the indexes, parsing and MDD
resources. The Bob plugin is a small JavaScript client; it does not parse MDX,
HTML or audio. The two components advertise a versioned local API.

## Install

The plugin requires **Bob 1.20.0 or later**. Its `/list` control query relies on
Bob's `query.originalText`; ordinary lookups continue to use the preprocessed
`query.text`.

### 1. Install the service

With Homebrew:

```bash
brew install wakewon/tap/bob-mdict
brew services start bob-mdict
```

Without Homebrew, download `bob-mdict-X.Y.Z-macos-installer.tar.gz` from the
[latest release](https://github.com/wakewon/bob-plugin-mdict/releases), extract
it, and run inside the extracted directory:

```bash
./install.sh
```

The standalone installer places the binary in `~/.local/bin`, installs a
LaunchAgent and creates the default dictionary directory. To remove the service
later, run `./uninstall.sh`; it keeps your dictionaries.

### 2. Add dictionaries

Put each dictionary in its own folder under:

```text
~/Library/Application Support/bob-mdict/dictionaries/
```

For example:

```text
dictionaries/
├── My Dictionary/
│   ├── My Dictionary.mdx
│   ├── My Dictionary.mdd
│   └── My Dictionary.1.mdd
└── Another Dictionary/
    └── Another Dictionary.mdx
```

Subfolders are discovered recursively. MDD volumes are matched to the MDX by
filename. Rescan and verify:

```bash
bob-mdict --rescan
bob-mdict --check
```

### 3. Install and add the Bob plugin

Download `MDict-vX.Y.Z.bobplugin` from the latest release and double-click it.
In Bob, open **Preferences → Translation → Services**, select **Text
Translation**, click `+`, choose **MDict**, enable it and save.

## Dictionary selection

The plugin deliberately produces one dictionary result per Bob service card.

```text
Dictionary ID empty  → first dictionary containing the queried word
Dictionary ID set    → only that dictionary
```

Most users should leave the field empty. To find IDs, query exactly `/list`
with the MDict service in Bob. It returns every discovered dictionary, its ID,
and unavailable diagnostics. Whitespace around `/list` is accepted; ordinary
`list` remains a normal dictionary lookup.

To pin several dictionaries at once, add the MDict service to Bob several
times and give each instance a different Dictionary ID. Bob controls their
order and enabled state, so results remain separate and readable.

Dictionary IDs are 16-character fingerprints sampled from MDX content. Moving
or renaming a folder or MDX file does not change an ID. They normally change
when the dictionary edition changes. Earlier development builds used path-based
IDs; if such an ID stops working, query `/list` once and replace it.

Lookup direction comes from the installed MDX headword index. Many
English-Chinese dictionaries index only English headwords; their Chinese
translations are not reverse-search keys. For Chinese-to-English lookup,
install an MDX whose headword index contains Chinese entries.

Lookup prefers an exactly cased headword and uses case-insensitive matching
only after an exact miss. Result titles and multi-record aliases use the actual
selected MDX key: if only `china` exists, `China` and `CHINA` still display and
navigate as `china`; if both `China` and `china` exist, they remain distinct.

## Multiple records for one headword

Some MDict files store several independent records under the same headword.
The default **Separate** mode shows the first complete record and adds clickable
siblings under `Other entries`, for example `wound²` and `wound³`. You can also
type `wound²`, `wound^2` or `wound^{2}` directly; `wound¹` returns to the first
record. A sibling preview ends in `…` when that record contains further
meanings beyond the excerpt. A trailing superscript integer is reserved for
this navigation syntax.

Choose **Combined** in the plugin settings to keep all records in one card,
labelled with `¹`, `²`, `³`, and so on.

Examples are grouped directly by their displayed sense, such as
`Examples · verb 1` and `Examples · verb 2`. See also references are exposed
through Bob's structured `relatedWordParts` representation when possible;
phrases and other explanatory sections remain additions.

## Plugin settings

| Setting | Default | Meaning |
|---|---|---|
| Service URL | `http://127.0.0.1:15321` | Change only when the daemon uses another port. |
| Dictionary ID | empty | Empty uses the first match; a value pins one dictionary. Query `/list` to discover IDs. |
| Duplicate entry display | Separate | Show one complete record with clickable `Other entries`; Combined keeps every ordinal-labelled record in one card. |
| Show examples | on | Show examples and bilingual translations. |
| Show extras | on | Show phrases, idioms, phrasal verbs, structured cross-references, forms and notes. |
| Max examples per sense | `3` | Limit examples independently for each sense or subsense. |

`pluginValidate` checks service identity and API version, the presence of a
healthy dictionary, and a configured Dictionary ID before the first lookup.

## Command line

```bash
bob-mdict --version
bob-mdict --check
bob-mdict --list-dictionaries
bob-mdict --rescan
bob-mdict --debug-lookup WORD
bob-mdict --diagnose NAME      # one dictionary: structure and parser coverage
bob-mdict --diagnose-all       # every dictionary in the directory
bob-mdict --validate NAME --validate-out DIR   # end-to-end review snapshots
bob-mdict --validate-all --validate-out DIR
```

`--debug-lookup` answers "what did the parser make of this word?"; `--diagnose`
answers "how well is this dictionary understood at all?" — which parser was
chosen and on what evidence, what markup conventions it uses, and how much
semantic structure a sample of its own records yields. Both report structure
and counts, never dictionary text. See
[docs/PARSER.md](docs/PARSER.md#diagnosing-an-unknown-dictionary).

`--validate` answers the question after that: is the structure a fair reading
of the record, and does it survive the rest of the pipeline? It runs the real
service and the real Bob adapter over records the dictionary actually
contains, measures how much of each record the parse accounts for and how much
it repeats, checks the invariants between parser, service, adapter and the
experimental Markdown renderer, and writes a ranked set of Markdown review
files. Unlike the diagnostics, those files quote real entries, so they are
written only where you point them and belong somewhere private. See
[docs/PARSER.md](docs/PARSER.md#validating-what-the-parser-produced).

The local HTTP API is documented in [docs/API.md](docs/API.md).

## Troubleshooting

### Cannot connect to the local service

```bash
brew services start bob-mdict
curl http://127.0.0.1:15321/v2/status
```

Confirm that the plugin's Service URL uses the daemon's actual port. The status
response identifies the running process with `serviceVersion`, `buildCommit`
and `apiVersion`; building another binary does not update that process.

### No dictionaries were found

```bash
open ~/Library/Application\ Support/bob-mdict/dictionaries/
bob-mdict --rescan
bob-mdict --list-dictionaries
```

Every dictionary needs an MDX. MDD is optional and supplies resources such as
recordings.

### The configured ID is invalid or unavailable

Query `/list` in Bob, copy the current ID, and update that MDict service
instance. An unavailable entry includes its diagnostic; other dictionaries
continue to work.

### A word has no audio button

Audio appears only when the entry references a real recording that resolves in
the matching MDD. No MDD, missing resource, or absent recording means no audio
button. The project never substitutes TTS.

### Some MDD audio is missing

Older dictionaries may store Ogg-Speex (`.spx`) recordings. Install a decoder:

```bash
brew install speex
```

Decoded WAV files are cached locally. On startup, entries older than 30 days are
removed and the cache is capped at 256 MiB.

### Plugin and service versions are incompatible

```bash
brew upgrade bob-mdict
```

Then update the Bob plugin from the release page.

## Privacy and security

- Lookups and resources stay on your Mac; there is no analytics or telemetry.
- The service binds only to `127.0.0.1`/`::1` and rejects non-loopback origins.
- MDD resources use opaque, per-process tokens; no filesystem path is exposed.
- No endpoint accepts an arbitrary path and the service makes no outbound
  network request.

## Copyright

This repository, its binaries and its plugin packages contain no dictionary
content. MDX/MDD files remain the property of their publishers; users are
responsible for obtaining and using them lawfully.

Tracked parser fixtures are minimal synthetic documents built specifically for
tests. They contain invented words, definitions, translations, examples and
resource paths; only the few selectors/classes and DOM relationships required
for compatibility tests are retained.

The project is licensed under GPL-3.0-or-later. See [LICENSE](LICENSE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
node --test plugin/main.test.js
./scripts/release.sh doctor
./scripts/release.sh dev
./scripts/release.sh build
```

`release.sh build` is pure: it writes only ignored release artifacts and proves
that tracked source is unchanged. The development command labels dirty builds
explicitly, safely updates the standalone LaunchAgent, and refuses to replace a
Homebrew- or otherwise-managed daemon. Release operations are documented in
[docs/RELEASE.md](docs/RELEASE.md).

Real-dictionary integration tests never write entry content into tracked
snapshots. Point them at a lawful local library:

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries go test ./internal/service -v
```

More detail: [Architecture](docs/ARCHITECTURE.md) · [Parser](docs/PARSER.md) ·
[HTTP API](docs/API.md)
