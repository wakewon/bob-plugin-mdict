# Architecture

## The shape of the thing

```
Bob
 │  selected text
 ▼
plugin/main.js                 thin: no MDX, no HTML parsing, no libraries
 │  POST /v1/lookup {query, format:"bob"}
 ▼
bob-mdict  (127.0.0.1 only)
 │
 ├── registry      recursive discovery, stable IDs, per-dictionary health
 ├── mdict         MDX/MDD access, @@@LINK redirects, O(1) resource index
 ├── profiles      declarative per-dictionary selectors, fingerprint-matched
 ├── parser        generic semantic parser + profile overrides → Entry IR
 ├── bobadapter    Entry IR → Bob toDict
 └── resource      opaque tokens, MIME, Range, SPX→WAV disk cache
```

## Why a companion service at all

Bob plugins run in Bob's JavaScript environment, not Node. Parsing a 1.3 GB MDD
volume, LZO/zlib block decompression, RIPEMD key-block decryption, and building
an index over a million headwords are not things to attempt there. Putting all
of it behind loopback HTTP keeps the plugin at a few hundred lines with no
dependencies, and lets the heavy part be a single native binary.

## The Entry IR is the contract

The parser does not produce Bob objects. It produces an `Entry` — headword,
pronunciations, parts, senses, subsenses, examples, forms, phrases, idioms,
cross-references, usage notes and etymology — that models **what dictionaries
contain**, not what Bob can currently display.

`internal/bobadapter` is the only package that knows Bob exists. That boundary
is the reason a future Bob with richer display needs one new adapter rather than
changes reaching back into the MDX layer. It is also why the sense hierarchy
survives: the IR keeps source numbers and nested subsenses, while the adapter
generates display numbers afresh inside each POS at the very last step.

## Parsing: generic first, profiles as reinforcement

Every dictionary invents its own HTML. Four real dictionaries produced four
completely different class vocabularies for the same concepts.

So the parser has two layers:

**A generic parser** that works from evidence rather than convention —
IPA detected by its characteristic Unicode characters, parts of speech by a
vocabulary that spans plain names (`verb`), abbreviations (`v.`, `vt.`),
grammar codes (`N-COUNT`, `V-ERG`) and Chinese labels (`动词`); sense blocks by
class-name fragments that recur across publishers (`sense`, `def-g`, `n-g`,
`trg`); regions by markers found in class names, ids, titles, filenames and
neighbouring text at once.

**Declarative profiles** (`internal/profiles/profiles.json`) that supply exact
selectors for a recognised dictionary. A profile is data, not code: a small CSS
subset, compiled once. It raises confidence and fixes structures the heuristics
would miss — but it never removes the generic fallback, so an unknown dictionary
is still usable on the day it is installed.

A dictionary is fingerprinted once, by probing a few common words and testing
structural selectors against the result. Titles are a weak hint only; repacks
rename themselves constantly.

### What the parser refuses to do

- It never flattens a record to text and calls it a definition. When no
  structure can be recovered, the content is emitted as an untyped section
  labelled "Entry" — visible to the reader, but not misrepresented as parsed.
- It does not classify what it cannot distinguish. Content that might be a
  definition or might be a usage note goes to a generic section rather than
  being filed under a guess.
- It interprets exactly one CSS property: `display:none` / `visibility:hidden`,
  because dictionaries use it to hide the language variant the reader did not
  select. There is no cascade, no specificity, no layout — semantic fidelity is
  the goal, not visual fidelity.

## Pronunciation

The single worst failure mode in a dictionary plugin is attaching one recording
to both the UK and US buttons. Region is therefore decided per candidate, from
all the evidence around it — class names, ids, titles, hrefs, the audio
filename, and the neighbouring text. `IPARegion` and `AudioRegion` are separate
IR facts, so a shared transcription can coexist with two regional recordings
and an unlabelled clip never inherits the IPA's region. Profiles can state the
region outright, and a rule may declare that it contributes a transcription but
no audio.

Bob documents only UK and US phonetic carrier slots. The adapter may use a free
slot for a neutral or unknown fact, but annotates that decision after the IPA
(`共用音标` / `未标口音`) and never changes the IR. Audio-only unknown provenance
uses a single result-level pronunciation note. If both Bob slots are already
occupied, an additional unknown recording cannot be displayed; that is a Bob
schema limitation, not a dictionary or parser fact.

Audio is offered only when the reference actually resolves in the user's MDD,
and only when a Speex asset can actually be decoded on this machine. There is
no synthesis path anywhere in the codebase.

## Bob result boundary

The server remains multi-dictionary: callers can search all dictionaries,
restrict by ID, or stop at the first match. The Bob adapter renders exactly one
entry. A blank plugin Dictionary ID requests the first match; a populated ID
requests only that dictionary. Users who want several pinned dictionaries add
several Bob MDict service instances, keeping cards, ordering and enablement
under Bob's control.

Within that entry, each top-level sense becomes one Bob `Part`. The part label
may repeat for consecutive senses of the same POS, while subsenses stay in the
same `Part` as their parent.

## Memory

The bundled engine's `BuildIndex` constructs three maps over every entry, one of
which only serves resource files. For a library of four large dictionaries that
was 464 MB resident.

The service instead calls `PrepareForExternalIndex` and builds only what it
needs: for an MDX an exact map plus a case-folded map that stores an entry only
when the folded form actually differs from the headword — most headwords are
already lowercase, and the fallback pass finds those by probing the exact map
with the folded query. For an MDD, one map from normalized resource key to
volume.

Cold start went from 1.48 s to **0.62 s**, and resident memory from 464 MB to
**253 MB** at rest for the same four dictionaries.

GC is tuned as well. The live set is large and almost entirely static, while
each lookup produces a burst of short-lived garbage from parsing an entry; at
the default GOGC the heap is allowed to double before collecting, so a handful
of lookups permanently inflated the process to 526 MB. A lower GC target plus
returning memory after the initial index build holds it at 313 MB under
sustained use, and it plateaus there.

The MDD index also replaces a linear scan. The engine's own resource lookup
walks every entry — 184 000 of them for one volume — on every audio request.

## Security boundary

The service holds a user's entire dictionary library, so it is treated as
something worth protecting even though it only listens on loopback:

- Binds `127.0.0.1` and `::1`. Never a LAN address, not even optionally.
- Rejects requests whose `Origin` is not loopback, checked by **parsing the
  URL** — a prefix test accepts `http://127.0.0.1.evil.example`.
- Resource tokens are AES-GCM sealed with a per-process key. They are opaque,
  unforgeable, and cannot be edited to address another dictionary.
- Resolution consults an in-memory index, never the filesystem. Path traversal,
  symlink escape and arbitrary file reads are structurally impossible rather
  than filtered.
- No endpoint accepts a path. `/v1/rescan` walks the configured directory and
  nothing else.
- Request bodies are capped; no shell, no outbound fetch, no filesystem paths in
  any response.
