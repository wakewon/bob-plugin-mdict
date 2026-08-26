// Package entryir defines the dictionary-neutral intermediate representation
// that every parser produces and every output adapter consumes.
//
// The IR deliberately models what dictionaries contain, not what Bob can
// display. Keeping the two apart means a richer Bob release only needs a new
// adapter, never a change to the MDX or parsing layers.
package entryir

// Region identifies a pronunciation variety.
type Region string

const (
	RegionUK Region = "uk"
	RegionUS Region = "us"
	// RegionNeutral means the dictionary presents a transcription as shared
	// across varieties rather than assigning it to one accent.
	RegionNeutral Region = "neutral"
	// RegionOther means the source did not provide enough evidence to classify
	// the variety. It must never be silently promoted to UK or US inside the IR.
	RegionOther Region = "other"
)

// Audio is a playable pronunciation asset that exists inside a user's MDD.
// There is no synthesized speech anywhere in this project: if no MDD resource
// backs a pronunciation, Audio is simply absent.
type Audio struct {
	// ResourceRef is the raw reference as it appeared in the entry HTML,
	// e.g. "sound://synthetic/uk/example.mp3" in a test fixture.
	ResourceRef string `json:"resourceRef"`
	// Token is the opaque handle the resource endpoint accepts. It never
	// contains a filesystem path.
	Token string `json:"token"`
	// URL is the fully-qualified loopback URL Bob can play.
	URL string `json:"url"`
	// MIMEType is the type the resource endpoint will serve, after any
	// transcoding (SPX is served as WAV).
	MIMEType string `json:"mimeType"`
}

// Image is an illustration that exists inside the selected dictionary's MDD.
// Like Audio, it carries only an opaque loopback resource URL; dictionary and
// filesystem identity never cross the service boundary.
type Image struct {
	ResourceRef string `json:"resourceRef"`
	Token       string `json:"token"`
	URL         string `json:"url"`
	MIMEType    string `json:"mimeType"`
	Alt         string `json:"alt,omitempty"`
}

// RichBlockKind is the deliberately small vocabulary used inside free-form
// prose. Core dictionary semantics remain typed fields; these blocks only
// preserve presentation content whose order matters.
type RichBlockKind string

const (
	RichText  RichBlockKind = "text"
	RichImage RichBlockKind = "image"
	RichTable RichBlockKind = "table"
)

// RichBlock preserves ordered text, image, and conventional table content.
// Exactly one of Text, Image, or Rows/Header is populated according to Kind.
type RichBlock struct {
	Kind  RichBlockKind `json:"kind"`
	Text  string        `json:"text,omitempty"`
	Image *Image        `json:"image,omitempty"`
	Rows  [][]string    `json:"rows,omitempty"`
	// Header is the table's own header row, present only when the source
	// marked one with <th> cells. Absent means the source declared no header,
	// which is a different fact from having an empty one.
	Header []string `json:"header,omitempty"`
}

// Pronunciation preserves transcription and recording provenance separately.
// A shared IPA can therefore coexist with distinct UK and US recordings, and
// an unlabelled recording never inherits the IPA's region by accident.
type Pronunciation struct {
	IPARegion   Region  `json:"ipaRegion,omitempty"`
	IPA         string  `json:"ipa,omitempty"`
	AudioRegion Region  `json:"audioRegion,omitempty"`
	Audio       *Audio  `json:"audio,omitempty"`
	Label       string  `json:"label,omitempty"`
	Confidence  float64 `json:"confidence"`
	// Rule records which parser rule produced this node. Debug only.
	Rule string `json:"rule,omitempty"`
}

// Example is a usage example, optionally with a translation and its own audio.
type Example struct {
	Text        string `json:"text"`
	Translation string `json:"translation,omitempty"`
	Audio       *Audio `json:"audio,omitempty"`
}

// Sense is one numbered meaning, possibly with nested subsenses.
type Sense struct {
	Number      string    `json:"number,omitempty"`
	Definition  string    `json:"definition"`
	Translation string    `json:"translation,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	Grammar     string    `json:"grammar,omitempty"`
	Topic       string    `json:"topic,omitempty"`
	Patterns    []string  `json:"patterns,omitempty"`
	Examples    []Example `json:"examples,omitempty"`
	Subsenses   []Sense   `json:"subsenses,omitempty"`
	Synonyms    []string  `json:"synonyms,omitempty"`
	Antonyms    []string  `json:"antonyms,omitempty"`
	Confidence  float64   `json:"confidence"`
	Rule        string    `json:"rule,omitempty"`
}

// Part groups senses under one part of speech.
type Part struct {
	POS        string  `json:"pos"`
	Grammar    string  `json:"grammar,omitempty"`
	Senses     []Sense `json:"senses"`
	Confidence float64 `json:"confidence"`
	Rule       string  `json:"rule,omitempty"`
}

// Form is an inflected form, e.g. plural or past participle.
type Form struct {
	Name  string   `json:"name"`
	Words []string `json:"words"`
	Audio *Audio   `json:"audio,omitempty"`
}

// PhraseEntry covers phrases, idioms, phrasal verbs and collocations, which
// all share the same shape: a lemma plus explanatory content.
type PhraseEntry struct {
	Phrase     string    `json:"phrase"`
	Definition string    `json:"definition,omitempty"`
	Examples   []Example `json:"examples,omitempty"`
}

// Section is free-form extended content that did not fit a typed field.
// Anything the parser is not confident about lands here rather than being
// mis-filed as a definition.
type Section struct {
	Title  string      `json:"title"`
	Body   string      `json:"body"`
	Blocks []RichBlock `json:"blocks,omitempty"`
}

// Source records where an entry came from.
type Source struct {
	DictionaryID    string `json:"dictionaryId"`
	DictionaryTitle string `json:"dictionaryTitle"`
	MatchedKey      string `json:"matchedKey"`
	// RedirectedFrom is set when @@@LINK or entry:// redirection was followed.
	RedirectedFrom string `json:"redirectedFrom,omitempty"`
	// Profile names the parser profile that handled this entry, or "generic".
	Profile string `json:"profile"`
	// RawRecordOrdinal is the one-based position among the exact MDX records
	// for MatchedKey, before resolved-content dedupe and empty filtering.
	RawRecordOrdinal int `json:"rawRecordOrdinal,omitempty"`
	// RecordStartOffset is a stable low-level locator for local diagnostics.
	RecordStartOffset int64 `json:"recordStartOffset,omitempty"`
}

// EntryRecord preserves one independently parsed semantic record. Its ordinal
// is assigned only after safe dedupe and empty filtering, so visible records
// are always numbered consecutively in source order.
type EntryRecord struct {
	RecordOrdinal int    `json:"recordOrdinal"`
	Entry         *Entry `json:"entry"`
}

// EntrySet is the dictionary-neutral aggregate produced for one selected key.
// Parser remains strictly one raw record -> one Entry; adapters decide how to
// present the preserved record boundaries.
type EntrySet struct {
	// LookupKey is the actual MDX key selected for the aggregate before
	// duplicate expansion. It is the stable, re-lookupable presentation alias;
	// redirects and parser-discovered titles do not rewrite it.
	LookupKey string `json:"lookupKey"`
	// Headword is the parser-discovered display headword of the first semantic
	// record. It is a content fact and need not equal LookupKey.
	Headword string        `json:"headword"`
	Records  []EntryRecord `json:"records"`
}

// Primary returns the first semantic record, if one exists.
func (s *EntrySet) Primary() *Entry {
	if s == nil || len(s.Records) == 0 {
		return nil
	}
	return s.Records[0].Entry
}

// Entry is the complete parsed dictionary entry.
type Entry struct {
	Headword        string          `json:"headword"`
	Source          Source          `json:"source"`
	Pronunciations  []Pronunciation `json:"pronunciations,omitempty"`
	Parts           []Part          `json:"parts,omitempty"`
	Forms           []Form          `json:"forms,omitempty"`
	Phrases         []PhraseEntry   `json:"phrases,omitempty"`
	Idioms          []PhraseEntry   `json:"idioms,omitempty"`
	PhrasalVerbs    []PhraseEntry   `json:"phrasalVerbs,omitempty"`
	Derivatives     []PhraseEntry   `json:"derivatives,omitempty"`
	Collocations    []string        `json:"collocations,omitempty"`
	UsageNotes      []Section       `json:"usageNotes,omitempty"`
	GrammarNotes    []Section       `json:"grammarNotes,omitempty"`
	Synonyms        []string        `json:"synonyms,omitempty"`
	Antonyms        []string        `json:"antonyms,omitempty"`
	CrossReferences []string        `json:"crossReferences,omitempty"`
	Related         []string        `json:"related,omitempty"`
	WordFamily      []string        `json:"wordFamily,omitempty"`
	Etymology       string          `json:"etymology,omitempty"`
	Sections        []Section       `json:"sections,omitempty"`
	// Notes carries parser diagnostics. Populated only in debug mode.
	Notes []string `json:"notes,omitempty"`
}

// SenseCount returns the total number of senses and subsenses across all parts.
// It is the cheapest signal of whether a parse produced anything useful.
func (e *Entry) SenseCount() int {
	total := 0
	for _, part := range e.Parts {
		for _, sense := range part.Senses {
			total++
			total += countSubsenses(sense)
		}
	}
	return total
}

func countSubsenses(s Sense) int {
	total := 0
	for _, sub := range s.Subsenses {
		total++
		total += countSubsenses(sub)
	}
	return total
}

// IsEmpty reports whether the parse yielded nothing worth showing.
func (e *Entry) IsEmpty() bool {
	return e.SenseCount() == 0 &&
		len(e.Pronunciations) == 0 &&
		len(e.Forms) == 0 &&
		len(e.Sections) == 0 &&
		len(e.Phrases) == 0 &&
		len(e.Idioms) == 0 &&
		len(e.PhrasalVerbs) == 0 &&
		len(e.Derivatives) == 0 &&
		len(e.CrossReferences) == 0 &&
		len(e.Related) == 0 &&
		len(e.Collocations) == 0 &&
		len(e.UsageNotes) == 0 &&
		len(e.GrammarNotes) == 0 &&
		len(e.Synonyms) == 0 &&
		len(e.Antonyms) == 0 &&
		len(e.WordFamily) == 0 &&
		e.Etymology == ""
}
