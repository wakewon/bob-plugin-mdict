# Writing a dictionary profile

A profile is **data, not code**: a JSON object in
[`internal/profiles/profiles.json`](../internal/profiles/profiles.json)
describing where a particular dictionary keeps things.

You do not need one to use a dictionary. The generic parser handles unknown
dictionaries on its own; a profile raises accuracy for a dictionary you use a
lot.

## Finding out what you are dealing with

```bash
bob-mdict --debug-lookup abandon
```

To see the raw HTML a dictionary actually stores, add a profile with only a
`match` block and run the parser against it — or read the record directly with
any MDict tool. Class histograms are the fastest way in: dictionaries are
remarkably consistent with themselves, and the ten most common class names
usually reveal the whole structure.

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
