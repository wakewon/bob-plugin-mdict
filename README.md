# MDict for Bob

English | [简体中文](README_CN.md)

Look up your own local MDict dictionaries in [Bob](https://bobtranslate.com/),
fully offline and with the real human recordings supplied by your dictionaries.

> This project is a reader. It ships no dictionary data. You provide and use
> your own `.mdx` and optional `.mdd` files lawfully.

## Highlights

- MDict v1.x/v2.x, recursive discovery, multiple dictionaries and multi-volume
  MDD (`.mdd`, `.1.mdd`, `.2.mdd`, …).
- Completely local: no dictionary API, cloud service, telemetry or outbound
  request. The companion service listens on loopback only.
- Structured entries: POS groups, senses and subsenses, bilingual definitions,
  examples, forms, phrases, idioms, phrasal verbs, cross-references and notes.
- Real MDD pronunciation only. UK, US, shared and unlabelled provenance is
  preserved. There is no text-to-speech fallback anywhere in the project.
- Simple Bob setup: leave Dictionary ID empty for the first dictionary that
  contains the word, or set one ID to pin that service instance.

## How it works

```text
Bob plugin → http://127.0.0.1:15321 → MDX/MDD → semantic parser
                                              → Entry IR → Bob toDict
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

Without Homebrew, download the matching archive from the
[latest release](https://github.com/wakewon/bob-plugin-mdict/releases), extract
it, and run:

```bash
./packaging/install.sh
```

The standalone installer places the binary in `~/.local/bin`, installs a
LaunchAgent and creates the default dictionary directory. To remove the service
later, run `./packaging/uninstall.sh`; it keeps your dictionaries.

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

## Plugin settings

| Setting | Default | Meaning |
|---|---|---|
| Service URL | `http://127.0.0.1:15321` | Change only when the daemon uses another port. |
| Dictionary ID | empty | Empty uses the first match; a value pins one dictionary. Query `/list` to discover IDs. |
| Show examples | on | Show examples and bilingual translations. |
| Show extras | on | Show phrases, idioms, phrasal verbs, cross-references, forms and notes. |
| Max examples per POS | `3` | Limit long corpus-example sections. |

`pluginValidate` checks service identity and API version, the presence of a
healthy dictionary, and a configured Dictionary ID before the first lookup.

## Command line

```bash
bob-mdict --version
bob-mdict --check
bob-mdict --list-dictionaries
bob-mdict --rescan
bob-mdict --debug-lookup WORD
```

The local HTTP API is documented in [docs/API.md](docs/API.md).

## Troubleshooting

### Cannot connect to the local service

```bash
brew services start bob-mdict
curl http://127.0.0.1:15321/v1/status
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
./scripts/build-plugin.sh
./scripts/build-server.sh
./scripts/dev-deploy.sh
```

`build-server.sh` only creates artifacts. `dev-deploy.sh` safely updates the
standalone development LaunchAgent, waits for the actual listener, and refuses
to replace a Homebrew- or otherwise-managed daemon. It then verifies that the
runtime version and commit equal `VERSION` and repository HEAD.

Real-dictionary integration tests never write entry content into tracked
snapshots. Point them at a lawful local library:

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries go test ./internal/service -v
```

More detail: [Architecture](docs/ARCHITECTURE.md) · [Parser](docs/PARSER.md) ·
[HTTP API](docs/API.md)
