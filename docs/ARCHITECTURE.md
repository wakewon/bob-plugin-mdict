# Architecture

## The shape of the thing

```
Bob
 │  selected text
 ▼
plugin/main.js                 thin: selector → base query + recordOrdinal
 │  POST /v2/lookup {query, recordOrdinal, multiRecordMode, format:"bob"|"markdown"}
 ▼
bob-mdict  (127.0.0.1 only)
 │
 ├── registry      recursive discovery, stable IDs, per-dictionary health
 ├── mdict         MDX/MDD access, duplicate-aware lookup, redirects, resources
 ├── profiles      declarative per-dictionary selectors, fingerprint-matched
 ├── diagnose      representative sampling, profile evidence, structure reports
 ├── parser        one resolved MDX record + profile overrides → one Entry IR
 ├── entryir       EntrySet aggregate preserving semantic record boundaries
 ├── presentation  cached EntrySet → combined or selected record
 ├── bobadapter    selected view → one Bob toDict (+ sibling navigation)
 ├── mdrender      EntrySet → user/diagnostic Markdown (sibling of bobadapter)
 ├── validate      development only: real backend over real records, ranked
 └── resource      opaque tokens, MIME, Range, SPX→WAV disk cache
```

## Why a companion service at all

Bob plugins run in Bob's JavaScript environment, not Node. Parsing a 1.3 GB MDD
volume, LZO/zlib block decompression, RIPEMD key-block decryption, and building
an index over a million headwords are not things to attempt there. Putting all
of it behind loopback HTTP keeps the plugin at a few hundred lines with no
dependencies, and lets the heavy part be a single native binary.

## Entry and EntrySet are the contract

The parser does not produce Bob objects. It produces an `Entry` — headword,
pronunciations, parts, senses, subsenses, examples, forms, phrases, idioms,
cross-references, usage notes and etymology — that models **what dictionaries
contain**, not what Bob can currently display. Duplicate exact MDX keys are
resolved before parsing; each resolved record is parsed independently and then
wrapped in an `EntrySet`. The parser never merges DOM trees or attempts to
classify homographs.

`internal/bobadapter` is the only package that knows Bob exists. That boundary
is the reason a future Bob with richer display needs one new adapter rather than
changes reaching back into the MDX layer. It is also why the sense hierarchy
survives: the IR keeps source numbers and nested subsenses, while the adapter
generates display numbers afresh inside each POS at the very last step.

### Two adapters, no conversion path

`internal/mdrender` renders the same EntrySet as Markdown. It is a *sibling* of
the Bob adapter, not a layer on top of it:

```text
EntrySet → bobadapter → Bob toDict
EntrySet → mdrender   → Markdown
```

Neither can see the other, and neither reads entry HTML. A second semantic path
— MDX HTML straight to Markdown — would drift from the parser the moment either
changed, and would have to relearn everything the parser already knows.

Diagnostic rendering stays deterministic and may include provenance while
omitting per-process resource URLs. User rendering contains only dictionary
content and enables resolved loopback audio/image links. The plugin requests
this user rendering with `format:"markdown"` and assigns it directly to
`toParagraphs`; it never reparses Markdown or dictionary HTML.

Free-form sections may carry a deliberately small ordered rich vocabulary:
text, resolved MDD image, and conventional table. This preserves positions such
as text → image → text → table without turning the Entry IR into a browser DOM.

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

A dictionary is fingerprinted once at rescan, from a handful of representative
records, by testing structural selectors against them. Titles are a weak hint
only; repacks rename themselves constantly.

### Sense evidence, in order

The generic parser tries four kinds of evidence and stops at the first that
produces something, because they are in descending order of how much the
dictionary is telling you:

1. **Class names.** `sense`, `def-g`, `n-g`, `trg` and their relatives. Only
   works when the publisher named its classes for humans.
2. **Visible numbering.** `1.`, `①`, `(a)`, `II` — either at the start of a
   block's text or in an element of its own. A survey of a hundred real
   dictionaries found this to be the one convention that survives everything:
   several titles ship machine-generated class names, sixteen carry no class
   attribute at all, and no class vocabulary is shared by more than a handful
   of unrelated publishers. Numbering is in all of them.
3. **`<ol>` and `<dl>`.** HTML already says what an ordered list and a
   definition list mean.
4. **A repeated boundary element.** A definition-classed span that occurs once
   per meaning, with the examples that follow it belonging to it.

Numbering is only believed when it forms a sequence: at least two markers of
one kind, ascending, starting at the beginning, restarting only where a new
part of speech would restart it, and yielding pieces small enough to be
meanings rather than whole articles. A lone "1." is far more often a homograph
superscript than a sense.

### Bilingual entries without a profile

Which half of a bilingual entry is the gloss follows from the headword: in a
dictionary keyed by English words the CJK text is the translation, and in one
keyed by Chinese the English is. Nothing in the parser needs to know which
languages are involved — only that two scripts are present and which of them
the entry is keyed by. A profile's `translation` selector, when there is one,
always wins.

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
filename, and the neighbouring text — but a block that prints its own `BrE` or
`NAmE` label is stating the region outright, and a neighbour's text is read only
when the block says nothing about itself. Borrowing it unconditionally is how
the American half of a pronunciation pair inherits the British label printed
just above it. `IPARegion` and `AudioRegion` are separate
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

## Diagnostics

`--debug-lookup` answers a question about one word. `internal/diagnose` answers
the question that comes before it: is this dictionary understood at all?

It picks a handful of representative records by **striding the key index and
scoring the results structurally** — record size, distinct tags, distinct class
vocabulary, repeated sibling structures, cross-references, pronunciation
references. Deterministic, so before/after comparisons mean something, and
language-independent, because a probe list of English words fingerprints only
the languages it was written for. The service uses the same sampling to choose
a parser at rescan, so there is one code path rather than two that drift.

The reports carry tag names, class names, attribute names, reference schemes,
counts, rates and warning codes — never dictionary text. That is what makes
them safe to keep, quote and check into a report.

Coverage numbers are **not accuracy**. Nothing has been compared against a
human reading of the entry; a field being present says the parser produced
something of that kind, not that what it produced is right. A small set of
conservative signals (`rich-html-no-definitions`, `oversized-definition`,
`implausible-sense-count`, `bilingual-without-translations`, …) marks
dictionaries worth a human look. Every one of them has a legitimate
explanation for some dictionary; they are observations, not verdicts.

`--parser generic|<id>|auto` forces a parser for comparison runs. It is a
debugging aid with no persisted state: the product always resolves a parser
from the current MDX and the current rules, which is what lets an existing
dictionary pick up a future parser improvement without anyone re-recording a
mapping for it.

## Validation

`internal/validate` answers the question after *that*: structure the parser
produced may still be a misreading, and structure it produced correctly may
still be lost on the way to a client.

It runs the shipping implementation end to end — the same sampling as profile
detection, the same `service.Lookup`, the same Bob adapter — over records the
dictionaries really contain, and it measures three things the coverage numbers
cannot see:

- **Content retention.** How much of the record's text the parse accounts for,
  compared token for token. A structured parse that keeps a twentieth of the
  record has found a table of contents, not a sense list.
- **Duplication.** How much output text exists beyond what the record contains.
  The same passage emitted under two fields shows up here and nowhere else.
- **Largest-field dominance.** Whether one extracted field is most of the
  record, which is what whole-record swallowing looks like from outside.

Retention is measured against the record *as the profile scopes it*: a `root`
that narrows a record to the edition being presented, and an `ignore` list that
names speaker icons as chrome, are deliberate decisions, not content the parser
dropped.

Alongside the metrics it checks invariants between the layers — the EntrySet is
keyed by the key the MDX matched, visible record ordinals are consecutive,
duplicate records stay distinguishable, the Bob word derives from the lookup key
rather than from a parser guess, sense order survives, every semantic field
survives into both presentations, and no token appears in the Markdown that is
absent from the IR. That last one is what makes "the Markdown is derived from
the IR" a checked fact rather than an intention.

Nothing in the served request path imports the package, and the daemon never
starts it.

### Priority, because a corpus is not a product

A hundred dictionaries are not equally important to this project. Validation
weights its sampling and its review queue by what the dictionaries are:
Chinese on either side first, English monolingual second, English paired with
another language third, other lexicons fourth, and article-style reference
works last. The classification is made from scripts in the sampled records —
Han characters with no kana and no hangul are Chinese, wherever the title
claims otherwise — corroborated but never decided by the title.

That ordering only affects how much attention a dictionary gets. No parsing
behaviour depends on it, which is why a rough classification is acceptable: the
cost of getting one wrong is that a person reads it in the wrong order.

## Bob result boundary

The server remains multi-dictionary: callers can search all dictionaries,
restrict by ID, or stop at the first match. The Bob adapter renders exactly one
EntrySet from one dictionary. A blank plugin Dictionary ID requests the first match; a populated ID
requests only that dictionary. Users who want several pinned dictionaries add
several Bob MDict service instances, keeping cards, ordering and enablement
under Bob's control.

The cache always stores the complete EntrySet—including its actual MDX
`LookupKey`—under dictionary ID, normalized input query and parser options.
`recordOrdinal` and `multiRecordMode` are applied after that cache boundary:

```text
MDX LookupAll → EntrySet cache → presentation selection
                                  ├── combined
                                  └── separate + recordOrdinal
                               → Bob adapter
```

Within a single-record EntrySet, presentation remains unchanged. Separate mode
renders one ordinary record and an independent `Other entries`
`relatedWordParts` group containing clickable aliases and deterministic source
previews. A preview receives an ellipsis both when its text is truncated and
when further meaningful senses/subsenses remain in that record. Every Bob
`word` and sibling alias derives from `EntrySet.LookupKey`, never the raw input
query or a parser-discovered title. The input remains `Result.Query`; each
resolved semantic record retains its own `Source.MatchedKey`, which can differ
after a redirect. These presentation aliases never rewrite the provenance.
Combined mode retains compact superscript
ordinals (`¹`, `²`, …) on phonetics, parts, exchanges, related words and
additions without changing sense/subsense numbering. Each top-level sense becomes one Bob `Part`. The part label
may repeat for consecutive senses of the same POS, while subsenses stay in the
same `Part` as their parent.

Examples use one addition per sense or subsense (`Examples · verb 1`,
`Examples · verb 1.1`). Cross-references stay dictionary-neutral in the IR and
are mapped only here to Bob's `relatedWordParts`; they are never presented as
inflection exchanges.

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

On the final 1.0.0 code (Apple M4, 2026-08-25), index construction benchmarks
at **0.60 s** for the same four dictionaries; the complete CLI rescan including
process setup and memory cleanup is **0.77 s**. Resident memory is **260 MB**
after rescan and **325 MB** after representative queries. Current reproducible
measurements live in [COMPATIBILITY.md](COMPATIBILITY.md).

GC is tuned as well. The live set is large and almost entirely static, while
each lookup produces a burst of short-lived garbage from parsing an entry; at
the default GOGC the heap is allowed to double before collecting, so a handful
of lookups permanently inflated the process to 526 MB. A lower GC target plus
returning memory after the initial index build keeps it bounded under
representative use.

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
- No endpoint accepts a path. `/v2/rescan` walks the configured directory and
  nothing else.
- Request bodies are capped; no shell, no outbound fetch, no filesystem paths in
  any response.
