# HTTP API

All endpoints are served on `http://127.0.0.1:<port>` (default `15321`) and on
`[::1]` when IPv6 loopback is available. Requests from any other address are
refused, as are requests carrying a non-loopback `Origin`.

The contract is versioned by `apiVersion`. The plugin refuses to talk to a
service advertising a different one, so a server upgrade cannot silently break
an older plugin.

---

## `GET /v1/status`

```json
{
  "service": "bob-mdict",
  "serviceVersion": "0.1.0",
  "apiVersion": "v1",
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

---

## `GET /v1/dictionaries`

```json
{
  "directory": "...",
  "dictionaries": [
    {
      "id": "1472050dd3ed",
      "title": "Collins COBUILD overhaul V2.30",
      "entryCount": 141091,
      "encoding": "UTF-8",
      "version": "2.000000",
      "createdAt": "2018-8-19",
      "hasMDD": true,
      "mddVolumes": 1,
      "profile": "collins-cobuild-overhaul",
      "health": "ok",
      "diagnostics": []
    }
  ]
}
```

`id` is derived from the dictionary path but does not disclose it. A dictionary
that fails to open reports `health: "unavailable"` with a reason in
`diagnostics`; the others stay usable.

---

## `POST /v1/lookup`

```json
{
  "query": "abandon",
  "format": "bob",
  "mode": "exact",
  "dictionaries": ["1472050dd3ed"],
  "limit": 1,
  "maxExamples": 3,
  "includeExamples": true,
  "includeExtras": true,
  "debug": false
}
```

| Field | Meaning |
|---|---|
| `query` | Required. |
| `format` | `ir` (default) returns the Entry IR only. `bob` adds a rendered `toDict`. |
| `mode` | `exact` (default) tries exact, Unicode-normalized and case-insensitive. `smart` also returns prefix suggestions on a miss. |
| `dictionaries` | Restrict and order the search. Empty means all, in registry order. |
| `limit` | Stop after this many dictionaries answer. |
| `maxExamples` | Cap examples per sense. |
| `includeExamples` / `includeExtras` | Trim the rendered `toDict`. |
| `debug` | Attach parser provenance notes to each entry. |

Responses: `200` with matches, `404` with an empty match list, `503` with
`error: "noDictionaries"` and a `hint` naming the dictionary directory.

Each match carries the dictionary-neutral `entry`; `format: "bob"` adds a
top-level `bob` object that is a complete `toDict`.

---

## `POST /v1/rescan`

Rediscovers and reindexes. **Takes no arguments** — the directory it walks is
fixed by configuration, so this endpoint cannot be aimed at the filesystem.

```json
{ "dictionaryCount": 4, "healthyDictionaryCount": 4, "elapsedSeconds": 1.4 }
```

---

## `GET /v1/resource/{token}`

Streams one MDD resource. Tokens come from a lookup response and are AES-GCM
sealed with a per-process key.

- `Content-Type` reflects the served bytes — Ogg-Speex is transcoded, so it is
  reported as `audio/wav`.
- `Accept-Ranges`, `ETag` and `Cache-Control: immutable` are set; Range requests
  return `206`.
- Bad, forged, edited or expired tokens get `400`; unknown resources `404`; a
  Speex asset with no decoder installed gets `503` with `speexUnavailable`.
