# MDict for Bob

Current product version: **1.3.0** · local API: **v2**.

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
- Four presentations: Dictionary card, Plain Text, Markdown (structured) and
  Markdown (web layout), which shows the dictionary's own page with its own
  stylesheet. See [Presentation modes](#presentation-modes-at-a-glance).
- Clickable lookups in the web layout, with a path back to the previous word.
- MDD-backed pronunciation only. UK, US, shared and unlabelled provenance is
  preserved. In both Markdown views 🔊 plays in the background with adjustable
  volume, speed and loudness matching. There is no text-to-speech fallback
  anywhere in the project.
- Simple Bob setup: leave Dictionary ID empty for the first dictionary that
  contains the word, or set one ID to pin that service instance.

## How it works

```text
Bob plugin → http://127.0.0.1:15321 → MDX/MDD → semantic parser
                                              → EntrySet IR → Bob toDict
                                                            → Plain Text
                                                            → Markdown
                          record HTML + dictionary CSS → web-layout Markdown
```

`bob-mdict` is a native Go service that owns the indexes, parsing, MDD
resources, HTML-to-Markdown conversion and audio playback. The Bob plugin is a
small JavaScript client; it does not parse MDX, HTML or audio. The two
components are installed and updated separately and agree on a versioned local
API. Clickable lookups and background playback go through a small helper app
(`MDict Lookup.app`) that the service builds on your Mac.

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

## Updating

The service and the plugin are two separate components with separate update
paths, and **new features usually need both**. Update them together.

1. **Update the service.** With Homebrew:

   ```bash
   brew update
   brew upgrade bob-mdict
   brew services restart bob-mdict
   ```

   Restarting matters: upgrading installs a new binary, but the process already
   running keeps serving the old version until it is restarted. With the
   standalone installer, download the new `bob-mdict-X.Y.Z-macos-installer.tar.gz`,
   extract it and run `./install.sh` again; it replaces the binary and restarts
   the LaunchAgent.
2. **Update the plugin.** Download the new `MDict-vX.Y.Z.bobplugin` from the
   [latest release](https://github.com/wakewon/bob-plugin-mdict/releases) and
   double-click it. Bob may also offer the update itself.
3. **Check both versions.** `bob-mdict --version` shows the installed binary;
   `curl http://127.0.0.1:15321/v2/status` shows the running process
   (`serviceVersion`). If they differ, the service was not restarted.

What happens when only one side is updated:

| Combination | Result |
|---|---|
| New plugin, old service | The settings appear, but the old service ignores them. Markdown (web layout) falls back to structured Markdown, and volume, speed and loudness matching have no effect. |
| Old plugin, new service | Works; the newer options are simply not shown in the plugin settings. |
| Different API version (currently `v2`) | The plugin refuses to query and says which side to update. |

After a service update the helper app may be rebuilt. macOS then asks once more
whether **MDict Lookup** may control Bob; allow it. Dictionary IDs, your
dictionaries and your plugin settings are kept.

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

## Presentation modes at a glance

Choose the mode in the plugin's **Display** setting. Every mode reads the same
dictionary; they differ in how the entry is shown and what can be done with it.

| | Dictionary card | Plain Text | Markdown (structured) | Markdown (web layout) |
|---|---|---|---|---|
| Shows | Bob's native card, built from the parsed entry | Parsed entry as plain text | Parsed entry as Markdown | The dictionary's own page, converted from its HTML and CSS |
| Minimum Bob | 1.20.0 | 1.20.0 | 1.21.0 (macOS 13+) | 1.21.0 (macOS 13+) |
| Pronunciation | Bob's own buttons | Listed as text, not playable | 🔊 plays in the background | 🔊 plays in the background |
| Volume, speed, loudness matching | ❌ Bob plays the audio | ❌ no audio | ✅ | ✅ |
| Clickable lookups | Bob's related words | ❌ copy selectors | ❌ copy selectors | ✅ dictionary links and `Other entries`, with a way back |
| Show examples / grammar / extras, max examples | ✅ | ✅ | ✅ | ❌ shown as published |
| Combined / Separate records | ✅ | ✅ | ✅ | ✅ |
| Needs dictionary stylesheets | no | no | no | recommended |
| Needs the link helper | no | no | for 🔊 playback | for links and 🔊 playback |

Volume, speed and loudness matching are done by the service, so they apply only
where the service plays the audio: the 🔊 in the two Markdown views. In the
dictionary card the pronunciation buttons are played by Bob, which offers no
such controls. Plain Text carries no audio at all.

Dictionary card also switches to Plain Text on its own for a free-form record
with no useful structure; see below.

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

**Markdown** presentation answers the same setting with what Markdown has.
Combined renders every record in order, each under a `Record n of total`
heading and divided from the next by a `---` thematic break. Separate renders
one record and closes with an `Other entries` list of the other records'
selectors. A typed record selector still selects exactly that record in either
mode.

Bob renders Markdown links as plain clickable URLs and documents no lookup
action for them, so those selectors — and cross references and related entries
— are written as copyable query text in a code span rather than as links that
would not work:

```markdown
## Other entries

- `wound²`
- `wound³`
```

This is presentation only. The navigation target stays in the parsed entry, so
if Bob later publishes a real Markdown lookup mechanism, only this rendering
changes.

Markdown rendering needs Bob 1.21.0 or later on macOS 13 or later. The plugin
still installs on Bob 1.20.0, where the Markdown option shows the document's raw
source; Dictionary card and Plain Text are unaffected.

**Markdown (web layout)** shows the dictionary's own page instead of the
parsed entry. The service reads the record's HTML together with the
dictionary's stylesheets — beside the MDX, then inside the MDD — works out from
them which parts start a new line, are hidden, bold or italic, and converts the
result with [html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown).
Pronunciation links become 🔊 links to the MDD audio, MDD illustrations are
shown, and nothing is fetched from the network. It keeps the same record
boundaries and `Other entries` selectors as the structured Markdown, but the
example, grammar and extras options do not apply: it shows the page as
published. If `/list` shows `缺少样式表` for a dictionary, copy those `.css`
files next to its `.mdx` and rescan; the view is much closer to the original
with them.

#### Clickable lookups and background pronunciation

In the web-layout view, a word the dictionary links to, and each `Other
entries` selector, can be clicked to look it up in Bob; in both Markdown views
a 🔊 plays the recording without opening a browser, labelled UK or US when the
recording says which. Bob hands a clicked link to macOS, and only an app can
receive one, so the service keeps a small helper for it:
`~/Library/Application Support/bob-mdict/MDict Lookup.app`. It is an
AppleScript applet that the service compiles on your Mac with the system's own
`osacompile` and signs locally, so it needs no developer certificate and
nothing extra is downloaded. It passes a word to Bob through Bob's documented
AppleScript interface, and plays recordings, prepared by the service, in an
audio engine it keeps running between clicks — so the next word plays at once
and a Bluetooth headset is not woken again; when the engine had stopped, a
short silence precedes the word so a waking headset does not swallow it. If
the helper cannot be set up, links stay plain text and 🔊 opens the recording
as before.

The helper records each click, and anything that went wrong, in
`~/Library/Logs/bob-mdict-helper.log`.

A page reached this way ends with a **← word** link back to the page you came
from, and following links further builds a path you can walk back step by
step. A word you look up yourself starts no path.

The first lookup click asks whether **MDict Lookup** may control Bob; allow
it. Bob itself asks "Open this link?" for any link that is not http, https or
mailto. To click without that prompt, set **Opening Links in Translation
Results** to **Never Ask** in Bob's settings — this applies to links from every
service in Bob.

Pronunciation volume, speed (without a pitch change) and loudness matching
are plugin settings. Matching measures each recording's loudness to ITU-R
BS.1770 with macOS's own audio tools and brings it to -16 LUFS, the level of
Apple's Sound Check, so dictionaries recorded at different levels sound alike;
gain is always limited so the peak stays below -1 dB.

**Plain Text** is rendered directly from the same EntrySet, using headings,
paragraphs, indentation and blank lines rather than Markdown syntax. Combined
mode uses a textual record separator; Separate mode lists copyable sibling
selectors. A Dictionary card request automatically uses Plain Text only when
the selected record is an untyped free-form fallback with no useful senses,
phrases, forms or navigation. Long structured entries remain Bob cards.

Examples are grouped directly by their displayed sense, such as
`Examples · v. 1` and `Examples · v. 2`. See also references are exposed
through Bob's structured `relatedWordParts` representation when possible;
phrases, idioms, phrasal verbs and collocations receive independent compact Bob
parts, while prose notes remain additions.

## Plugin settings

Open **Bob → Preferences → Translation → Services → MDict** to change these.

| Setting | Default | What it does |
|---|---|---|
| Service URL | `http://127.0.0.1:15321` | Where the plugin finds the local service. Change it only if you started the service on another port. |
| Dictionary ID | empty | Empty looks the word up in the first dictionary that contains it. Enter an ID to search only that dictionary. Query `/list` to see the IDs. |
| Display | Dictionary card | How a result is shown; see [Presentation modes](#presentation-modes-at-a-glance). **Dictionary card** and **Plain Text** work on every supported Bob. The two **Markdown** options need Bob 1.21.0 or later on macOS 13 or later; on Bob 1.20.0 they show the raw Markdown source. **Markdown (web layout)** ignores the example, grammar and extras settings below. |
| Duplicate entry display | Separate | For a headword with several records: **Separate** shows the first record and lists the others as `Other entries` (type `word²` to open one); **Combined** shows every record in one result. |
| Pronunciation volume | 100% | How loud 🔊 plays, 50% to 200%. Applied after loudness matching, and the sound is peak-limited so a high value does not distort. Markdown views only. |
| Pronunciation speed | 1.0× | How fast 🔊 plays, 0.5× to 1.5×. The pitch does not change. Markdown views only. |
| Pronunciation loudness matching | on | Evens out recordings so that dictionaries recorded at different levels sound alike (-16 LUFS, the level of Apple's Sound Check). Turn it off to hear recordings at their original level. Markdown views only. |
| Show examples | on | Show example sentences and their translations. Not used by the web layout. |
| Show grammar | on | Show grammatical notes such as `[with object]`. Parts of speech, labels and patterns are always shown. Not used by the web layout. |
| Show extras | on | Show phrases, idioms, phrasal verbs, cross-references, word forms and usage notes. Not used by the web layout. |
| Max examples per sense | `3` | The most examples shown under each meaning. Not used by the web layout. |

Clicking a link in the web layout may make Bob ask "Open this link?" every time.
To stop that, set **Opening Links in Translation Results** to **Never Ask** in
Bob's settings; it applies to every service in Bob.

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
it repeats, checks the invariants between parser, service and the
Bob/Plain/Markdown renderers, and writes a ranked set of Markdown review
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

Update both, as described in [Updating](#updating):

```bash
brew upgrade bob-mdict
brew services restart bob-mdict
```

Then install the latest plugin from the release page.

### A new setting or presentation has no effect

The running service is probably still the old version. Compare
`bob-mdict --version` with `serviceVersion` from
`curl http://127.0.0.1:15321/v2/status`, and restart the service if they differ.

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
go test -short -race ./...
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

`go test ./...` runs everything, including the real-dictionary integration
tests. The race suite uses `-short`, which skips those: they are corpus-scale by
nature — a fresh service parsed over your whole library, once per test — and
under the race detector a large library puts the package beyond any sensible
timeout. What race detection actually needs is contention, and that comes from
the synthetic fixtures in `internal/service/concurrency_test.go`, which drive
concurrent lookups, a rescan racing lookups in flight, and concurrent resource
resolution. `-short -race` covers every package in seconds.

Real-dictionary integration tests never write entry content into tracked
snapshots. Point them at a lawful local library:

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries go test ./internal/service -v
```

More detail: [Architecture](docs/ARCHITECTURE.md) · [Parser](docs/PARSER.md) ·
[HTTP API](docs/API.md) · [Known issues and limitations](docs/KNOWN_ISSUES.md)
