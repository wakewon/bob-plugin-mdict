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
  "serviceVersion": "1.0.0",
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
      "diagnostics": []
    }
  ]
}
```

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
| `format` | `ir` (default) returns the duplicate-aware EntrySet IR. `bob` additionally returns one top-level `bob` `toDict`. `markdown` additionally returns top-level user-facing `markdown`. Both presentations use the first dictionary match; the IR remains in `matches`. |
| `mode` | `exact` (default) prefers an exactly cased headword, then tries Unicode-normalized and case-insensitive fallback matches. `smart` also returns prefix suggestions on a miss. |
| `dictionaries` | Restrict and order the search. Empty means all, in registry order. |
| `limit` | Stop after this many dictionaries answer. |
| `maxExamples` | Cap parsed and displayed examples independently per sense or subsense. |
| `includeExamples` / `includeExtras` | Trim both rendered presentation formats consistently. |
| `multiRecordMode` | Bob rendering: `separate` shows one record with sibling navigation; `combined` renders the complete EntrySet with ordinal labels. Markdown keeps all record boundaries as `Record n of total` headings. |
| `recordOrdinal` | One-based ordinal in the visible EntrySet after resolved-byte dedupe and parser-empty filtering—not a raw MDX record index. In Markdown it selects that record; zero/omitted renders every record. Bob behavior remains unchanged. |
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

`format: "markdown"` leaves the same `matches` array intact and adds a
top-level Markdown string:

```json
{
  "query": "lead",
  "matches": [{"lookupKey": "lead", "records": []}],
  "markdown": "# lead\n\n## Record 1 of 2\n\n...\n"
}
```

This is ordinary user presentation: parser rules, confidence, validation
warnings, and raw-source diagnostics are never included. Resolved MDD images
use `![alt](http://127.0.0.1:.../v2/resource/<opaque-token>)`; unresolved or
external image references emit no broken URL. Conventional tables are rendered
as Markdown tables with escaped cells and padded uneven rows.

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

## `GET /v2/resource/{token}`

Streams one MDD resource. Tokens come from a lookup response and are AES-GCM
sealed with a per-process key.

- `Content-Type` reflects the served bytes — Ogg-Speex is transcoded, so it is
  reported as `audio/wav`; dictionary images retain their image MIME type.
- `Accept-Ranges`, `ETag` and `Cache-Control: immutable` are set; Range requests
  return `206`.
- Bad, forged, edited or expired tokens get `400`; unknown resources `404`; a
  Speex asset with no decoder installed gets `503` with `speexUnavailable`.
