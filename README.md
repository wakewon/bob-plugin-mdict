# MDict for Bob

Look up **your own** local MDict dictionaries from [Bob](https://bobtranslate.com/) —
fully offline, with UK/US phonetics and the real human pronunciations that came
with your dictionaries.

> This project ships **no dictionary data**. You supply your own `.mdx`/`.mdd`
> files. It is a reader, not a dictionary.

---

## What you get

- **Your `.mdx`/`.mdd` dictionaries** — MDict v1.x and v2.x, multi-volume MDD
  (`.mdd`, `.1.mdd`, `.2.mdd`, …), several dictionaries at once.
- **Completely offline.** No dictionary API, no cloud, no telemetry. The service
  listens on loopback only and never makes an outbound request.
- **Real dictionary pronunciation.** UK and US audio streamed straight out of
  your MDD files.
- **No text-to-speech, ever.** If your dictionary has no recording, no audio
  button appears. Nothing is synthesized to fill the gap.
- **Structured entries, not stripped text.** Parts of speech, numbered senses
  and subsenses, definitions, translations, examples, labels, inflections,
  phrases, idioms, phrasal verbs, synonyms and etymology — mapped onto Bob's
  native dictionary display.

<!-- Add a screenshot of a Bob lookup here once you have one. -->

---

## How it works

```
Bob  ──►  MDict plugin  ──►  http://127.0.0.1:15321  ──►  bob-mdict
                                                            │
                                          ┌─────────────────┼─────────────────┐
                                          ▼                 ▼                 ▼
                                     .mdx lookup       .mdd audio      semantic parser
                                          │                 │                 │
                                          └────────► Entry IR ──► Bob toDict ─┘
```

Two pieces:

- **`bob-mdict`** — a small native service that reads your dictionaries. One
  binary, no Python, no Node, no Docker, no database.
- **The Bob plugin** — a thin JavaScript shim that asks the service for a word
  and hands Bob the result. It never parses MDX or dictionary HTML itself.

They version independently and agree on an API version, so upgrading one does
not silently break the other.

---

## Install

### 1. Install the service

```bash
brew install wakewon/tap/bob-mdict
brew services start bob-mdict
```

<details>
<summary>Without Homebrew</summary>

Download the archive for your Mac from the
[latest release](https://github.com/wakewon/bob-plugin-mdict/releases), then:

```bash
tar -xzf bob-mdict-*-darwin-arm64.tar.gz
./packaging/install.sh
```

The installer puts the binary in `~/.local/bin`, registers a LaunchAgent so the
service starts at login, and creates the dictionary folder. You never need to
write a `plist` or keep a terminal window open.

To remove it again: `./packaging/uninstall.sh` (your dictionaries are kept).
</details>

### 2. Add your dictionaries

Put each dictionary in its own folder inside:

```
~/Library/Application Support/bob-mdict/dictionaries/
```

For example:

```
dictionaries/
├── My English Dictionary/
│   ├── My English Dictionary.mdx      ← definitions
│   ├── My English Dictionary.mdd      ← audio, images
│   └── My English Dictionary.1.mdd    ← extra volumes are found automatically
└── Another Dictionary/
    └── Another Dictionary.mdx
```

Subfolders are discovered recursively, and `.mdd` volumes are matched to their
`.mdx` by filename. You never configure individual files.

Then pick them up:

```bash
bob-mdict --rescan
```

Check everything is working:

```bash
bob-mdict --check
```

### 3. Install the Bob plugin

Download `MDict-vX.Y.Z.bobplugin` from the
[latest release](https://github.com/wakewon/bob-plugin-mdict/releases) and
double-click it. Then in Bob: **Preferences → Services → Translate → +** and
choose **MDict**.

---

## Settings

| Setting | Default | What it does |
|---|---|---|
| Service address | `http://127.0.0.1:15321` | Only change this if you started the service on another port. |
| Dictionary selection | First match | `First match` keeps results focused on one dictionary. `All matches` shows every dictionary that has the word, clearly separated. `Specific only` restricts to IDs from `bob-mdict --list-dictionaries`. |
| Show examples | On | Example sentences, with translations when the dictionary is bilingual. |
| Show extras | On | Phrases, idioms, phrasal verbs, collocations, usage notes, synonyms, etymology. |
| Max examples per sense | 3 | Some dictionaries carry dozens of corpus sentences per sense. |

Sensible defaults mean most people change nothing.

---

## Command line

```bash
bob-mdict --version              # version and API version
bob-mdict --check                # full self-check with per-dictionary diagnostics
bob-mdict --list-dictionaries    # dictionary IDs, entry counts, detected parser
bob-mdict --rescan               # pick up newly added dictionaries
bob-mdict --debug-lookup WORD    # print the parsed entry as JSON
```

---

## Troubleshooting

<details>
<summary><b>“Cannot connect to the local MDict service”</b></summary>

The service is not running, or is on a different port.

```bash
brew services start bob-mdict   # or: ~/.local/bin/bob-mdict --check
curl http://127.0.0.1:15321/v1/status
```

If `--check` works but Bob does not, make sure the plugin's service address
matches the port the service is actually on.
</details>

<details>
<summary><b>“No dictionaries installed”</b></summary>

The service is running but the dictionary folder is empty, or the files are
nested somewhere it did not look.

```bash
open ~/Library/Application\ Support/bob-mdict/dictionaries/
bob-mdict --rescan
bob-mdict --list-dictionaries
```

Each dictionary needs at least one `.mdx`. A `.mdd` is optional and only
supplies audio and images.
</details>

<details>
<summary><b>A word is found but there is no audio button</b></summary>

Audio comes only from your `.mdd` files, and only when a real recording exists.
Check what your dictionary actually contains:

```bash
bob-mdict --check
```

If the dictionary shows `mdd=0`, it has no resource file — the `.mdd` was not
copied alongside the `.mdx`, or that dictionary simply has no audio.

This project never substitutes text-to-speech for a missing recording.
</details>

<details>
<summary><b>Some pronunciations are missing even though the MDD is present</b></summary>

A few older dictionaries store audio as Ogg-Speex (`.spx`), which macOS cannot
play. Install the decoder and those pronunciations appear:

```bash
brew install speex
```

Each file is decoded once and cached. `bob-mdict --check` reports whether a
decoder was found.
</details>

<details>
<summary><b>One dictionary is broken</b></summary>

A corrupt dictionary is isolated: it is marked unavailable and every other
dictionary keeps working. `bob-mdict --check` prints the reason for each one.
</details>

<details>
<summary><b>Port 15321 is already taken</b></summary>

Start the service on another port and set the same address in the plugin:

```bash
bob-mdict --port 15400
```

For the Homebrew service, set `BOB_MDICT_PORT` in the launchd plist, or run the
binary directly.
</details>

<details>
<summary><b>“Plugin and local service versions are incompatible”</b></summary>

The plugin and the service agree on an API version. Bring both up to date:

```bash
brew upgrade bob-mdict
```

and update the plugin from the releases page.
</details>

---

## Privacy

- Everything happens on your Mac. The service makes **no outbound network
  requests at all**.
- It binds to `127.0.0.1` (and `::1`) only — never to a LAN address — and
  rejects cross-origin requests, so a web page you have open cannot read your
  dictionary library.
- Your words are never sent anywhere. There is no analytics, no telemetry, no
  usage reporting.
- MDD resources are addressed by opaque, per-session tokens. No filesystem path
  ever leaves the service.

---

## Copyright

This project is a dictionary **reader**. It contains and distributes no
dictionary content. `.mdx`/`.mdd` files belong to their publishers, and it is
your responsibility to obtain and use them lawfully.

Licensed under **GPL-3.0-or-later** — see [LICENSE](LICENSE). The reasoning, and
the full audit of every dependency, is in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

---

## Development

```bash
go test ./...              # unit, golden and API tests
./scripts/build-plugin.sh  # build release/MDict-vX.Y.Z.bobplugin and update appcast.json
./scripts/build-server.sh  # build server binaries and checksums
./scripts/release.sh       # everything, plus the Homebrew formula
```

Tests that need real dictionaries skip cleanly when none are present. To run
them, point at a folder of your own:

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries go test ./internal/service -v
```

More detail: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) ·
[docs/API.md](docs/API.md) · [docs/PARSER.md](docs/PARSER.md)
