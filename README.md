# MDict for Bob

Current product version: **1.3.0** · local API: **v2**.

English | [简体中文](README_CN.md)

Look up your own local MDict dictionaries in [Bob](https://bobtranslate.com/),
fully offline. Pronunciation comes straight from the dictionary's own audio
files; there is no text-to-speech.

> This project is a reader. It ships no dictionary data. You provide and use
> your own `.mdx` and optional `.mdd` files lawfully.

## Highlights

- Works with MDict v1.x/v2.x dictionaries, several at once, including
  multi-volume `.mdd` files.
- Completely offline: no cloud service, telemetry or network requests.
- Four ways to show an entry: Dictionary card, Plain Text, Markdown, and
  Markdown (web layout), which shows the dictionary's own page as published.
- Clickable words in the web layout, with a link back to the previous word.
- Pronunciation played in the background, with adjustable volume and speed.

## How it works

```text
Bob plugin → local service (127.0.0.1:15321) → your MDX/MDD files
```

`bob-mdict` is a small service that runs on your Mac, reads your dictionaries
and answers the Bob plugin. The plugin only displays the results. They are
installed and updated separately, so see [Updating](#updating).

## Install

Requires **Bob 1.20.0 or later**.

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

To remove it later, run `./uninstall.sh`; your dictionaries are kept.

### 2. Add dictionaries

Put each dictionary in its own folder under:

```text
~/Library/Application Support/bob-mdict/dictionaries/
```

```text
dictionaries/
├── My Dictionary/
│   ├── My Dictionary.mdx
│   └── My Dictionary.mdd
└── Another Dictionary/
    └── Another Dictionary.mdx
```

The `.mdd` file (optional) holds pronunciation and images. Then rescan and
check:

```bash
bob-mdict --rescan
bob-mdict --check
```

### 3. Install the Bob plugin

Download `MDict-vX.Y.Z.bobplugin` from the latest release and double-click it.
In Bob, open **Preferences → Translation → Services**, select **Text
Translation**, click `+`, choose **MDict**, enable it and save.

## Updating

The service and the plugin are updated separately, and new features usually
need both, so update them together.

1. **Service.** With Homebrew, then restart it (an upgrade does not take effect
   until the service restarts):

   ```bash
   brew update
   brew upgrade bob-mdict
   brew services restart bob-mdict
   ```

   With the standalone installer, download the new package and run
   `./install.sh` again.
2. **Plugin.** Check for updates in Bob, or download the new `.bobplugin` from
   the release page and double-click it.

If a new option seems to do nothing, the service is probably still the old
version: compare `bob-mdict --version` with the `serviceVersion` shown by
`curl http://127.0.0.1:15321/v2/status`, and restart the service if they differ.
A new plugin with an old service simply ignores the new features. Your
dictionaries and settings are kept.

## Presentation modes

Choose one in the plugin's **Display** setting.

| | Dictionary card | Plain Text | Markdown | Markdown (web layout) |
|---|---|---|---|---|
| Shows | Bob's native card | Plain text | Structured Markdown | The dictionary's own page |
| Bob version | 1.20.0+ | 1.20.0+ | 1.21.0+ (macOS 13+) | 1.21.0+ (macOS 13+) |
| Pronunciation | Bob's own buttons | Not playable | 🔊 | 🔊 |
| Volume, speed, loudness matching | ❌ | ❌ | ✅ | ✅ |
| Clickable words | Related words | ❌ | ❌ | ✅ |
| Examples / grammar / extras settings | ✅ | ✅ | ✅ | ❌ shown as published |
| Combined / Separate records | ✅ | ✅ | ✅ | ✅ |

Volume, speed and loudness matching only work where 🔊 is played by this
project, that is, in the two Markdown modes. In the dictionary card, Bob plays
the audio itself and offers no such controls.

**Markdown (web layout)** shows the page the way the dictionary's publisher
designed it, so it looks closest to the original dictionary. If `/list` marks a
dictionary as missing stylesheets (`缺少样式表`), copy its `.css` files next to
the `.mdx` and rescan for a better result.

### Clicking words and playing audio

In the web layout you can click a word to look it up in Bob, and a page opened
that way ends with a **← word** link back. 🔊 in both Markdown modes plays right
away without opening a browser.

Bob cannot receive these clicks directly, so the service builds a small helper
app on your Mac, `MDict Lookup.app` (in
`~/Library/Application Support/bob-mdict/`). It is made locally with macOS's own
tools, needs no developer certificate and downloads nothing. What to expect:

- The first click asks whether **MDict Lookup** may control Bob. Allow it. After
  a service update it may ask again.
- Bob may ask "Open this link?" each time. To stop that, set **Opening Links in
  Translation Results** to **Never Ask** in Bob's settings (this applies to all
  Bob services).
- If the helper cannot be set up, links stay plain text and 🔊 opens the
  recording in your browser instead.
- Clicks and errors are logged in `~/Library/Logs/bob-mdict-helper.log`.

## Choosing a dictionary

Each MDict service in Bob shows one dictionary's result.

- **Dictionary ID empty** (default): the first dictionary containing the word.
- **Dictionary ID set**: only that dictionary.

To find IDs, look up `/list` with the MDict service in Bob. To see several
dictionaries at once, add the MDict service to Bob several times, each with a
different ID; Bob then shows them as separate cards.

Lookup direction follows the dictionary's own index. Many English-Chinese
dictionaries index only English headwords, so Chinese-to-English lookup needs a
dictionary that indexes Chinese entries.

## Multiple records for one word

Some dictionaries keep several entries under one word. With the default
**Separate** setting you see the first entry and a list of `Other entries` such
as `wound²` and `wound³`. Type `wound²` to open one, and `wound¹` to return.
In the web layout these are clickable; elsewhere, copy the text. **Combined**
shows every entry together instead.

## Plugin settings

Open **Bob → Preferences → Translation → Services → MDict**.

| Setting | Default | What it does |
|---|---|---|
| Service URL | `http://127.0.0.1:15321` | Change only if you run the service on another port. |
| Dictionary ID | empty | Empty uses the first dictionary that has the word; an ID uses only that one. |
| Display | Dictionary card | How results look; see [Presentation modes](#presentation-modes). The two Markdown options need Bob 1.21.0+ (macOS 13+); on Bob 1.20.0 they show raw Markdown. |
| Duplicate entry display | Separate | **Separate** shows one entry plus `Other entries`; **Combined** shows all entries together. |
| Pronunciation volume | 100% | How loud 🔊 plays, 50%–200%. Markdown modes only. |
| Pronunciation speed | 1.0× | How fast 🔊 plays, 0.5×–1.5×, pitch unchanged. Markdown modes only. |
| Pronunciation loudness matching | on | Makes recordings from different dictionaries about equally loud. Turn off to hear them at their original level. Markdown modes only. |
| Show examples | on | Show example sentences and translations. Not used by the web layout. |
| Show grammar | on | Show grammar notes such as `[with object]`. Not used by the web layout. |
| Show extras | on | Show phrases, idioms, word forms and usage notes. Not used by the web layout. |
| Max examples per sense | `3` | Most examples shown under each meaning. Not used by the web layout. |

## Troubleshooting

**Cannot connect to the local service.** Start it and check that the plugin's
Service URL matches its port:

```bash
brew services start bob-mdict
curl http://127.0.0.1:15321/v2/status
```

**No dictionaries were found.** Each dictionary needs an `.mdx` file:

```bash
open ~/Library/Application\ Support/bob-mdict/dictionaries/
bob-mdict --rescan
bob-mdict --list-dictionaries
```

**The Dictionary ID is invalid.** Look up `/list` in Bob, copy the current ID
and update that service. IDs can change when you replace a dictionary with
another edition; moving or renaming files does not change them.

**A word has no audio button.** Audio appears only when the dictionary has a
recording for it in its `.mdd`. Nothing is generated in its place.

**Some audio is missing.** Older dictionaries may use `.spx` recordings; install
a decoder with `brew install speex`.

**Plugin and service are incompatible, or a new setting has no effect.** Update
both as described in [Updating](#updating).

## Privacy and copyright

Everything stays on your Mac: no telemetry, no outbound requests, and the
service accepts connections from your own machine only.

This project contains no dictionary content; MDX/MDD files remain the property
of their publishers, and you are responsible for using them lawfully. Licensed
under GPL-3.0-or-later; see [LICENSE](LICENSE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## For developers

```bash
bob-mdict --debug-lookup WORD   # what the parser made of a word
bob-mdict --diagnose NAME       # how well a dictionary is understood
bob-mdict --validate NAME --validate-out DIR
```

```bash
gofmt -w .
go vet ./...
go test ./...
go test -short -race ./...
node --test plugin/main.test.js
./scripts/release.sh doctor
```

More detail: [Architecture](docs/ARCHITECTURE.md) · [Parser](docs/PARSER.md) ·
[HTTP API](docs/API.md) · [Releasing](docs/RELEASE.md) ·
[Known issues and limitations](docs/KNOWN_ISSUES.md)
