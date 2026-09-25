# HTTP API

All endpoints are served on `http://127.0.0.1:<port>` (default `15321`) and on
`[::1]` when IPv6 loopback is available. Requests from any other address are
refused, as are requests carrying a non-loopback `Origin`.

The contract is versioned by `apiVersion`. The plugin refuses to talk to a
service advertising a different one, so a server upgrade cannot silently break
an older plugin.

---

## `GET /v2/status`

```json
{
  "service": "bob-mdict",
  "serviceVersion": "1.3.0",
  "buildCommit": "abcdef1",
  "apiVersion": "v2",
  "platform": "darwin",
  "architecture": "arm64",
  "dictionaryDirectory": "/Users/you/Library/Application Support/bob-mdict/dictionaries",
  "dictionaryCount": 4,
  "healthyDictionaryCount": 4,
  "audioAvailable": true,
  "speexAvailable": true,
  "speexDecoder": "speexdec",
  "lookupLinks": true,
  "uptimeSeconds": 128.4
}
```

Used by `pluginValidate` to tell apart "service missing", "no dictionaries" and
"incompatible version" — three problems with three different fixes.
`buildCommit` is diagnostic identity for the process actually listening on the
port; it is not required to equal the independently packaged plugin commit.

---

## `GET /v2/dictionaries`

```json
{
  "directory": "...",
  "dictionaries": [
    {
      "id": "2f4c6a8e10b3d597",
      "title": "My Local Dictionary",
      "entryCount": 120000,
      "encoding": "UTF-8",
      "version": "2.000000",
      "createdAt": "2026-1-1",
      "hasMDD": true,
      "mddVolumes": 1,
      "profile": "generic",
      "health": "ok",
      "diagnostics": [],
      "missingStylesheets": ["switch.css"]
    }
  ]
}
```

`missingStylesheets` (omitted when empty) lists local stylesheets that the
dictionary's sampled records link to but that are neither beside the MDX nor in
an MDD. It is measured at rescan from the same records that choose the parser
profile. Only `markdownSource: "html"` uses stylesheets, so a dictionary with
missing ones is still fully usable; copying the named files next to the MDX and
rescanning improves that view.

`id` is a 16-character, path-independent fingerprint built from MDX file size,
the header and three spread-out content samples. Moving or renaming the folder
or MDX file keeps the ID; changing dictionary editions normally changes it. No
MDD volume is hashed. Development builds before this scheme used path-based
IDs, so those users should obtain the replacement once through `/list` or this
endpoint.

A dictionary that fails to open still appears with `health: "unavailable"` and
a reason in `diagnostics`; the others stay usable.

---

## `POST /v2/lookup`

```json
{
  "query": "abandon",
  "format": "bob",
  "mode": "exact",
  "dictionaries": ["2f4c6a8e10b3d597"],
  "limit": 1,
  "maxExamples": 3,
  "includeExamples": true,
  "includeExtras": true,
  "multiRecordMode": "separate",
  "recordOrdinal": 2,
  "debug": false
}
```

| Field | Meaning |
|---|---|
| `query` | Required. |
| `format` | `ir` (default) returns the duplicate-aware EntrySet IR. `bob`, `plain`, and `markdown` add the requested presentation from the first dictionary match; the IR remains in `matches`. A `bob` request may conservatively return `plain` instead for a free-form fallback entry. `effectiveFormat` identifies the populated presentation field. |
| `mode` | `exact` (default) prefers an exactly cased headword, then tries Unicode-normalized and case-insensitive fallback matches. `smart` also returns prefix suggestions on a miss. |
| `dictionaries` | Restrict and order the search. Empty means all, in registry order. |
| `limit` | Stop after this many dictionaries answer. |
| `maxExamples` | Cap parsed and displayed examples independently per sense or subsense. |
| `includeExamples` / `includeExtras` | Trim all rendered presentation formats consistently. |
| `audioVolume` / `audioRate` / `audioNormalize` | Pronunciation playback settings (percentages, default 100; matching default true). They are carried by 🔊 links that play in the background and change nothing else. |
| `markdownSource` | With `format: "markdown"`: `entry` (default) renders the parsed EntrySet; `html` converts the first match's own record HTML and stylesheets (see below). `includeExamples`, `includeExtras`, `includeGrammar` and `maxExamples` do not apply to `html`. Older v2 services ignore the field and return `entry`. |
| `multiRecordMode` | Presentation only. `separate` shows one record plus navigation to the others; `combined` shows every record with explicit boundaries. Bob uses ordinal labels/related words, Plain uses a textual separator/copyable selectors, and Markdown uses headings/`---`/code-spanned selectors. |
| `recordOrdinal` | One-based ordinal in the visible EntrySet after resolved-byte dedupe and parser-empty filtering—not a raw MDX record index. It selects that record in every presentation and overrides `multiRecordMode`. |
| `debug` | Attach parser provenance notes to each entry. |

Responses: `200` with matches; a normal headword miss is `404` with an empty
match list. An invalid explicit ID returns `404 dictionaryNotFound`; an existing
but unhealthy ID returns `503 dictionaryUnavailable`; both include a `/list`
hint. An empty registry returns `503 noDictionaries` with the directory.
An ordinal beyond the visible EntrySet returns `404 recordNotFound`; it never
falls back to record 1 or reports the selector-shaped alias as a missing word.

`query` is the normalized user input. Each match carries the actual aggregate
MDX `lookupKey`, a parser-discovered display `headword`, and `records[]`. Every
record has a consecutive `recordOrdinal` plus an independently parsed `entry`:

```json
{
  "matches": [{
    "dictionaryId": "2f4c6a8e10b3d597",
    "dictionaryTitle": "My Local Dictionary",
    "lookupKey": "lead",
    "headword": "lead1",
    "records": [
      {"recordOrdinal": 1, "entry": {"headword": "lead1", "source": {"matchedKey": "lead"}}},
      {"recordOrdinal": 2, "entry": {"headword": "lead2", "source": {"matchedKey": "lead"}}}
    ]
  }]
}
```

These fields deliberately describe different facts:

- `query`: normalized request text, retained for the response and errors.
- `matches[].lookupKey`: actual MDX key chosen before duplicate expansion; this
  is the stable, re-lookupable base used by Bob `word` and sibling aliases.
- `matches[].headword` / `entry.headword`: parser-discovered display text.
- `entry.source.matchedKey`: actual semantic record target after redirect
  resolution, so it may differ from `lookupKey`.

For example, if only lowercase `china` exists, a `China` request keeps
`query: "China"` but returns `lookupKey: "china"` and Bob `word: "china"`.
For a redirect `foo → bar`, `lookupKey` and Bob `word` remain `foo`, while the
record's `source.matchedKey` is `bar`.

Duplicate expansion happens only after one exact spelling has been selected.
Resolved byte-identical records are removed, parser-empty records are omitted,
and the remaining records keep MDX source order. `format: "bob"` adds a
top-level `bob` object. In separate mode it is one ordinary record plus an
`Other entries` related-word group; an explicit selection uses a presentation
alias such as `lead²` while `lookupKey` remains `lead`; parsed headwords and
record provenance remain unchanged. Combined mode preserves the complete
ordinal-labelled card and uses the same `lookupKey` for Bob `word`.

The response also carries `effectiveFormat`. Normally it equals the requested
presentation. For a `bob` request whose selected EntrySet has no meaningful
typed structure or navigation and consists only of generic free-form Entry/
headword sections (or weak, untyped generic marker blocks), it is `plain`; the response contains
top-level `plain` and omits `bob`. Entry length and sense density never trigger
this fallback.

`format: "plain"` always adds one complete Plain Text document rendered
directly from the canonical EntrySet:

```json
{
  "query": "lead",
  "effectiveFormat": "plain",
  "matches": [{"lookupKey": "lead", "records": []}],
  "plain": "lead\n\nRecord 1 of 2\n\n...\n\n====================\n\nRecord 2 of 2\n\n...\n"
}
```

Plain uses paragraphs, indentation, blank lines and textual section headings;
it is not Markdown with syntax stripped. Navigation is ordinary labelled text
such as `See also: injure`. Separate mode ends with copyable `Other entries`
selectors without inventing URLs or private lookup schemes.

`format: "markdown"` leaves the same `matches` array intact and adds a
top-level Markdown string:

```json
{
  "query": "lead",
  "effectiveFormat": "markdown",
  "matches": [{"lookupKey": "lead", "records": []}],
  "markdown": "# lead\n\n## Record 1 of 2\n\n...\n\n---\n\n## Record 2 of 2\n\n...\n"
}
```

Markdown is a presentation option, not a second semantic contract. It is
rendered by `internal/mdrender` from the same canonical EntrySet the Bob card
is rendered from, and it is additive: `ir` and `bob` clients are unaffected and
the API stays at v2.

This is ordinary user presentation: parser rules, confidence, validation
warnings, and raw-source diagnostics are never included. Resolved MDD images
use `![alt](http://127.0.0.1:.../v2/resource/<opaque-token>)`; unresolved or
external image references emit no broken URL. Conventional tables are rendered
as Markdown tables with escaped cells and padded uneven rows; a table that
declares its own `<th>` header row uses it, and one that declares none keeps
its first row in the header position Markdown requires.

### Multiple records in Markdown

`combined` renders every record in canonical order. Each carries a
`## Record n of total` heading and consecutive records are divided by a `---`
thematic break — never before the first record, never after the last. No field
of one record is interleaved with another's.

```markdown
# wound

## Record 1 of 3

…complete first record…

---

## Record 2 of 3

…complete second record…
```

`separate` renders exactly one record — the first, or the one `recordOrdinal`
names — and closes with the other records' selectors:

```markdown
## Other entries

- `wound²`
- `wound³`
```

### Markdown from record HTML

`markdownSource: "html"` returns the dictionary's own layout instead of the
parser's reading of it. The response has the same shape — `effectiveFormat:
"markdown"`, the IR still in `matches`, the document in `markdown` — and the
EntrySet still decides which records exist, so `recordOrdinal` and the record
selectors name the same record in both views.

Each shown record's HTML is converted by `internal/htmlmd`:

- The record's `<link>` stylesheets are read from the dictionary folder, then
  from the MDD, and inline `<style>` blocks apply too. Only `display`,
  `visibility`, `font-weight`, `font-style`, `list-style`, horizontal
  margins/padding and `::before`/`::after` string content are interpreted,
  with the normal cascade (importance, inline style, specificity, order).
  Conditional `@media` blocks and state selectors such as `:hover` are skipped.
  A parser profile may add a `presentationCSS` stylesheet after the
  dictionary's own, which hides interface chrome.
- A profile's `root` narrows the record as it does for parsing.
- `sound://` links and `<audio>` become `[🔊](loopback URL)` after the text
  they wrapped; `entry://`, `bword://`, in-page and script links are unwrapped
  to their text; MDD images use loopback URLs; remote and `data:` images and
  remote stylesheets are dropped, so rendering never touches the network.
- Tables without a header row that are not a full grid of simple cells are
  layout, and are flattened into lines.

When the link helper is ready (`lookupLinks: true` in `/v2/status`),
`entry://` and `bword://` links become `bobmdict://lookup?text=…&from=…&port=…`
links that look the target up in Bob, `from` naming the page they are on (with
its record selector when one was used), and sibling selectors become the same
kind of link. A page reached through such a link ends, after a `---`, with
`[← previous](bobmdict://lookup?text=previous&back=1&port=…)`; the service
keeps the path (at most 50 steps, forgotten after 30 idle minutes), so the way
back can be followed more than one step. The way back is offered only on the
page a step opens, within 30 seconds of it; the same word looked up later by
hand shows none, though a link on it still continues the path.
In both Markdown views, 🔊 becomes `bobmdict://play?port=…&token=…&volume=…&rate=…&normalize=…`
for recordings the system can play; the helper fetches them from
`/v2/audio/{token}`. In the web-layout view a 🔊 says UK or US
when the recording's own markup does. Without the helper, links stay text and
🔊 keeps its resource URL.

Record boundaries and sibling selectors are exactly those of the structured
Markdown below, without its `# key` title — the dictionary page prints its own
headword. If a record converts to nothing, the structured Markdown is returned
instead.

### Navigation targets

Bob publishes no Markdown lookup-action contract, so a link in this content
would be either an invalid external URL or a private scheme Bob does not
honour. Dictionary navigation targets are therefore written as copyable query
text in an inline code span: sibling record selectors, `crossReferences`, and
`related`.

```markdown
## See also

- `injure`
- `damage`
```

This is a presentation decision, not a parser one. The target itself stays in
the IR under its own field, so a future Bob-native lookup mechanism replaces
this rendering alone and needs no change to the parser, the IR, or the API.

Lists that name related vocabulary rather than a navigation target —
`synonyms`, `antonyms`, `collocations`, `wordFamily` — stay ordinary text, so
that a code span continues to mean "you can look this up".

Exact headword spelling is always preferred. Case-insensitive matching is used
only as a fallback when no exact key exists; NFC and NFD spellings share the
same canonical query identity without collapsing letter case.

---

## `POST /v2/rescan`

Rediscovers and reindexes. **Takes no arguments** — the directory it walks is
fixed by configuration, so this endpoint cannot be aimed at the filesystem.

```json
{ "dictionaryCount": 4, "healthyDictionaryCount": 4, "elapsedSeconds": 1.4 }
```

---

## `POST /v2/audio/{token}`

Returns one MDD recording as `audio/wav`, prepared for the link helper to play
in its own audio engine: decoded to 44.1 kHz mono float, matched in loudness
to -16 LUFS unless `normalize=0`, scaled by `volume` (percent, default 100,
10–200) and never louder than a -1 dB peak, and preceded by `leadin`
milliseconds of silence (default 0, at most 1000), shortened by `rate`
(percent, 50–200) because the helper stretches it along with the word. The
same guards as every route apply: a request carrying a non-loopback `Origin`
is refused, and a GET cannot reach it. Bad tokens get `400`, unknown
resources `404`, and formats the system cannot decode `415`.

## `POST /v2/navigation`

Records a step the reader took through a dictionary link. The link helper
sends it, as a form, just before it asks Bob to look the word up: `to` and
`from` for a link, `to` and `back=1` for the way back. Bob passes the plugin
only the text to look up, so this is how the page that opens learns where the
reader came from. Returns `204`; a step without `to`, or without either
`from` or `back=1`, gets `400`.

## `GET /v2/resource/{token}`

Streams one MDD resource. Tokens come from a lookup response and are AES-GCM
sealed with a per-process key.

- `Content-Type` reflects the served bytes — Ogg-Speex is transcoded, so it is
  reported as `audio/wav`; dictionary images retain their image MIME type.
- `Accept-Ranges`, `ETag` and `Cache-Control: immutable` are set; Range requests
  return `206`.
- Bad, forged, edited or expired tokens get `400`; unknown resources `404`; a
  Speex asset with no decoder installed gets `503` with `speexUnavailable`.
