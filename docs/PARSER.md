# Writing a dictionary profile

A profile is **data, not code**: a JSON object in
[`internal/profiles/profiles.json`](../internal/profiles/profiles.json)
describing where a particular dictionary keeps things.

You do not need one to use a dictionary. The generic parser handles unknown
dictionaries on its own; a profile raises accuracy for a dictionary you use a
lot.

## Diagnosing an unknown dictionary

Before writing anything, find out whether a profile is even needed:

```bash
bob-mdict --diagnose "Penguin"
```

The argument is a stable dictionary ID or any fragment of a title or filename.
The report gives you the container facts (health, entry count, encoding, engine
version, MDD volumes), which parser was selected and on what evidence, the
dictionary's tag/class/attribute histograms and recurring selector signatures,
how much structure the current parser recovers from a sample of its own
records, and a short list of conservative signals worth a human look.

`--diagnose-json` prints it as JSON; `--diagnose-out DIR` writes it to a file.

To survey a whole directory:

```bash
bob-mdict --dictionary-dir /path/to/mdxs --diagnose-all --diagnose-out /tmp/corpus
```

That writes `corpus.json` and `corpus.md` with per-dictionary rows, the
generic-versus-profile distribution, aggregate extraction coverage, warning
counts, and any groups of dictionaries whose class vocabularies coincide
strongly enough to be one template family. Keep the output somewhere
git-ignored; it names dictionaries, and the dictionaries are the developer's
own.

To compare parsers on the same dictionary while you work:

```bash
bob-mdict --diagnose "Penguin" --parser generic
bob-mdict --diagnose "Penguin" --parser oald8
```

`--parser` is a debugging aid, not a setting. Nothing persists it, and the
product always resolves a parser from the current MDX and the current rules —
which is exactly what lets a dictionary benefit from a future parser
improvement with no action from anyone.

### How records are chosen

Sampling strides the key index and scores candidates from their markup alone:
size, distinct tags, distinct class vocabulary, classes that repeat several
times in one record (the signature of a sense list), cross-references and
pronunciation references. It is deterministic, so the same file always yields
the same samples and a before/after comparison means something, and it involves
no word list, so it works the same on a Japanese or German dictionary as on an
English one.

### Reading the numbers

They are **coverage**, not accuracy. Nothing has been compared against a human
reading of the entry. "definitions 8/10" means eight samples produced something
the parser filed as a definition — not that eight are correct. A high fallback
rate on an encyclopedia or a terminology bank is the right answer, not a bug.

## Validating what the parser produced

Coverage says a field exists. It does not say the field is a fair reading of
the record, and it does not say the reader will ever see it.

```bash
bob-mdict --validate "Penguin" --validate-out /private/review
bob-mdict --dictionary-dir /path/to/mdxs --validate-all --validate-out /private/review
```

This runs the real service and the real Bob adapter over records the dictionary
actually contains, then writes a Markdown review set: an index, a page per
dictionary, and one file per queued record showing the source markup, the
canonical EntrySet, what Bob would receive, the Markdown rendering, and every
automatic measurement of the three.

**These files quote real entries.** That is what they are for, and it is why
they are written only where you point them. Keep them somewhere private and
never commit them.

### What is measured

| | |
|---|---|
| content retention | how much of the record's text the parse accounts for |
| duplication | how much output text exists beyond what the record contains |
| largest field | whether one field swallowed the record |
| backend parity | whether the layers agree about what was found |

Retention is measured against the record *as the profile scopes it*. A `root`
that picks one edition out of a record holding several, and an `ignore` list
naming speaker icons, are decisions rather than losses.

None of these is accuracy either. What they do is rank: a record with low
retention and high duplication is a better use of your attention than a random
one, and that is the whole claim.

### The review queue

A corpus run produces around a thousand validated records. The queue is the
hundred most informative of them, scored by product priority first — Chinese on
either side, then English monolingual, then English with another language —
and after that by evidence that something is wrong, by reliance on a heuristic
with no track record, and by what changed since the last run.

### Comparing runs

```bash
bob-mdict --validate-all --validate-out /private/review-2 \
  --validate-baseline /private/review/baseline.json
```

Each run writes a `baseline.json` of measurements and hashes — never dictionary
text — and a later run classifies every record against it: unchanged, changed,
likely improvement, possible regression, or source changed.

More extracted fields is deliberately **not** treated as an improvement. A
parse can double its sense count by splitting one meaning in half. What counts
is structure recovered where there was none without losing retention, or a
parse that now accounts for materially more of its record.

## Finding out what a dictionary's markup looks like

To see the raw HTML a dictionary actually stores, add a profile with only a
`match` block and run the parser against it — or read the record directly with
any MDict tool. The class histogram in `--diagnose` is usually the fastest way
in: dictionaries are remarkably consistent with themselves, and the ten most
common class names often reveal the whole structure.

## The selector language

A deliberately small CSS subset, matched against the entry DOM:

```
div                 tag
.Sense              class
#abandon_1          id
a.speaker.brefile   compound
[href]              attribute present
[href^=sound://]    prefix   ($= suffix, *= contains, = exact)
.p-g .def-g .d      descendant
.DEF, .ind          alternatives
```

There is no cascade and no specificity. This matches nodes; it does not style
them.

## Fields

| Field | Purpose |
|---|---|
| `match.selectors` | **All** must be present in a sample entry. This is the reliable fingerprint. |
| `match.titleContains` | Weak hint. Repacks rename themselves, so never rely on it alone. |
| `root` | Narrow to the first match. Use when one record holds several regional editions of the same entry. |
| `ignore` | Chrome to delete before anything else runs: fold buttons, speaker icons, frequency bars. |
| `headword` | The entry's own headword. |
| `translation` | Nodes holding a translation of neighbouring text, lifted out instead of concatenated into it. |
| `pronunciation[]` | `selector`, `region` (`uk`/`us`/`neutral`/`other`/`auto`), `ipa`, `audio` (attribute names), `noAudio`. IPA and audio provenance become separate IR fields. |
| `partBlock`, `pos`, `grammar` | Part-of-speech grouping. Omit `partBlock` when each sense carries its own label. |
| `sense`, `subsense`, `senseNumber`, `definition`, `definitionStrip`, `labels`, `topic`, `patterns` | Sense structure. |
| `example`, `exampleText` | Examples, and the source-language text inside them. |
| `synonyms`, `antonyms` | Extracted **and detached** before labels are collected. |
| `forms[]` | `container`, `label`, `word`, `name`. |
| `sections[]` | `selector`, `kind`, `items`, `lemma`, `body`, `stripTitle`, `title`. |
| `wordFamily` | Related derived words. |

`kind` is one of `idiom`, `phrase`, `phrasalVerb`, `usage`, `grammar`,
`etymology`, `synonyms`, `collocation`, `generic`.

## Order of operations

Knowing this explains most surprises:

1. `root` narrows the tree.
2. `ignore` and inline-hidden nodes are removed.
3. Headword, pronunciations, forms, word family.
4. **Sections are extracted and detached.** Idioms and phrasal verbs contain
   sense-shaped markup; removing them first is what stops those senses being
   counted as the entry's own.
5. Parts and senses are parsed from what remains. Within a sense: subsenses are
   detached, then examples, then synonyms, then labels — each removal keeping
   the next step's text clean.

When a profile puts a semantic heading in the POS selector, the parser first
classifies `See also`, cross-reference, related, phrase, idiom, phrasal verb,
derivative and synonym/antonym labels. These become typed Entry fields instead
of Parts. A genuinely unknown short POS remains a generic Part rather than being
dropped.

## Traps worth knowing

- **One clip on both buttons.** A transcription wrapped in an `<a href="sound://…">`
  will claim that link. Set `noAudio: true` on the transcription rule and give
  the regions their own rules.
- **Doubled translations.** Bilingual dictionaries often emit the same gloss
  twice — before and after the definition — hiding one with CSS. Identical
  fragments are deduplicated, but check the output.
- **Labels stolen from cross-references.** A synonym list carries its own
  register labels (`ditch [slang]`). Scope the `labels` selector, or rely on
  synonyms being detached first.
- **The same entry twice.** Some dictionaries ship a British and an American
  edition in one record. That is what `root` is for.
- **Sense wrappers that do not exist.** A part with a single meaning often has
  no numbered wrapper. The parser falls back to treating the block itself as one
  sense when a `definition` selector matches inside it.

## Before you write a profile

The generic parser recovers senses from four kinds of evidence in turn — class
names, visible numbering, `<ol>`/`<dl>`, and a repeated definition element —
and lifts glosses out of bilingual entries by script when no profile names them.
Whatever evidence located the senses, the material *between* one sense and the
next is treated as belonging to the first: a great many dictionaries keep the
definition in one element and the examples in the elements after it, where no
sense contains them and nothing else reads them.
Across a survey of a hundred real dictionaries it produced structure for
three-quarters of them unaided. Check `--diagnose` first: a dictionary already
at 100% structural coverage does not need a profile, and adding one is more
code to maintain for no reader-visible gain.

Prefer, in order:

1. **A generic improvement**, when the structure you are looking at recurs
   across several unrelated publishers. Generic rules must encode a real
   semantic convention, never one dictionary's habits.
2. **A family profile**, when several dictionaries share the same element
   hierarchy, class conventions, pronunciation markup and semantic
   organisation — as the publisher-XML Oxford learner builds do. Different
   titles do not imply different templates, and a shared `.sense` class does
   not imply a shared one.
3. **A dedicated profile**, only when a dictionary matters enough, is genuinely
   specialised, and cannot be recovered any other way.

## Verifying

Add a **synthetic** fixture — `testdata/golden/profile-<id>.html` — that
reproduces the structure with invented content, then:

```bash
go test ./internal/parser -run TestGolden -update   # record
go test ./internal/parser -run TestGolden           # check
```

Read the generated JSON before committing it. A golden file records what the
parser *did*, which is only useful once you have confirmed it is what it
*should* do.

Never paste real dictionary text or copy a publisher entry skeleton into a
fixture and replace the words. Construct the smallest DOM from scratch, retain
only selectors/classes and relationships the test needs, use invented resource
paths, and omit publisher CSS, internal IDs and chrome.
