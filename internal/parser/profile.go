package parser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Profile is a declarative description of one dictionary's HTML conventions.
//
// A profile never replaces the generic parser: it supplies selectors the
// generic heuristics would otherwise have to guess at, and anything it leaves
// unset falls back to generic behaviour. A dictionary with no profile is
// therefore still usable, just with lower confidence.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Match decides whether this profile applies to a dictionary.
	Match ProfileMatch `json:"match"`

	// Root narrows parsing to the first matching node. Dictionaries that ship
	// several regional editions of the same entry in one record use this to
	// keep one of them instead of reporting every sense twice.
	Root []string `json:"root,omitempty"`

	// Ignore removes chrome — fold buttons, speaker icons, scripts — before
	// anything else runs.
	Ignore []string `json:"ignore,omitempty"`

	// Headword locates the entry's own headword.
	Headword []string `json:"headword,omitempty"`

	// Translation marks nodes holding a translation of the neighbouring
	// English text, which must be lifted out rather than concatenated into it.
	Translation []string `json:"translation,omitempty"`

	// Pronunciation rules are evaluated in order.
	Pronunciation []PronunciationRule `json:"pronunciation,omitempty"`

	// PartBlock delimits one part-of-speech section. When unset the whole entry
	// is treated as a single block.
	PartBlock []string `json:"partBlock,omitempty"`
	POS       []string `json:"pos,omitempty"`
	Grammar   []string `json:"grammar,omitempty"`

	// Sense-level selectors.
	Sense       []string `json:"sense,omitempty"`
	SenseNumber []string `json:"senseNumber,omitempty"`
	Subsense    []string `json:"subsense,omitempty"`
	Definition  []string `json:"definition,omitempty"`
	// DefinitionStrip removes nodes from inside a definition before its text is
	// read, for dictionaries that inline the sense number or POS there.
	DefinitionStrip []string `json:"definitionStrip,omitempty"`
	Labels          []string `json:"labels,omitempty"`
	Topic           []string `json:"topic,omitempty"`
	Patterns        []string `json:"patterns,omitempty"`
	Example         []string `json:"example,omitempty"`
	ExampleText     []string `json:"exampleText,omitempty"`
	Synonyms        []string `json:"synonyms,omitempty"`
	Antonyms        []string `json:"antonyms,omitempty"`

	// Forms locates inflection tables.
	Forms []FormRule `json:"forms,omitempty"`

	// Sections map named regions of the entry onto typed IR fields.
	Sections []SectionRule `json:"sections,omitempty"`

	// WordFamily lists related derived words.
	WordFamily []string `json:"wordFamily,omitempty"`

	compiled *compiledProfile
}

// ProfileMatch fingerprints a dictionary.
type ProfileMatch struct {
	// TitleContains matches against the MDX title or filename.
	TitleContains []string `json:"titleContains,omitempty"`
	// Selectors must all be present in a sample entry. This is the reliable
	// signal; titles vary between repacks of the same dictionary.
	Selectors []string `json:"selectors,omitempty"`
}

// PronunciationRule extracts one pronunciation variant.
type PronunciationRule struct {
	// Selector picks the node holding the pronunciation.
	Selector []string `json:"selector"`
	// Region is "uk", "us", "other", or "auto" to detect from context.
	Region string `json:"region,omitempty"`
	// IPA is a selector relative to the matched node; empty means the node's
	// own text.
	IPA []string `json:"ipa,omitempty"`
	// Audio names the attribute holding the resource reference, e.g. "href".
	Audio []string `json:"audio,omitempty"`
	// NoAudio marks a rule that contributes a transcription only. Without it a
	// transcription wrapped in an audio link would claim that link, which is
	// how one clip ends up labelled as both British and American.
	NoAudio bool `json:"noAudio,omitempty"`
}

// FormRule extracts inflected forms.
type FormRule struct {
	// Container bounds the inflection table.
	Container []string `json:"container"`
	// Label selects the form name, Word the form itself. When Label is unset
	// every word found is reported under Name.
	Label []string `json:"label,omitempty"`
	Word  []string `json:"word,omitempty"`
	Name  string   `json:"name,omitempty"`
}

// SectionKind names the IR field a section feeds.
type SectionKind string

const (
	SectionIdiom       SectionKind = "idiom"
	SectionPhrase      SectionKind = "phrase"
	SectionPhrasalVerb SectionKind = "phrasalVerb"
	SectionUsage       SectionKind = "usage"
	SectionGrammar     SectionKind = "grammar"
	SectionEtymology   SectionKind = "etymology"
	SectionSynonyms    SectionKind = "synonyms"
	SectionCollocation SectionKind = "collocation"
	SectionGeneric     SectionKind = "generic"
)

// SectionRule maps a region of the entry onto a typed section.
type SectionRule struct {
	Selector []string    `json:"selector"`
	Kind     SectionKind `json:"kind"`
	// Title labels the section in output; defaults to a name derived from Kind.
	Title string `json:"title,omitempty"`
	// Items splits a section holding several entries — a list of idioms, for
	// instance — into one IR entry each.
	Items []string `json:"items,omitempty"`
	// Lemma selects the phrase itself within the section.
	Lemma []string `json:"lemma,omitempty"`
	// Body selects the explanatory content.
	Body []string `json:"body,omitempty"`
	// StripTitle removes this selector's text from the body, for sections whose
	// heading lives inside the block.
	StripTitle []string `json:"stripTitle,omitempty"`
}

// compiledProfile holds the parsed selectors so compilation happens once.
type compiledProfile struct {
	root            Selector
	ignore          Selector
	headword        Selector
	translation     Selector
	partBlock       Selector
	pos             Selector
	grammar         Selector
	sense           Selector
	senseNumber     Selector
	subsense        Selector
	definition      Selector
	definitionStrip Selector
	labels          Selector
	topic           Selector
	patterns        Selector
	example         Selector
	exampleText     Selector
	synonyms        Selector
	antonyms        Selector
	wordFamily      Selector
	matchSel        []Selector
	titleRes        []*regexp.Regexp

	pronunciation []compiledPronunciation
	forms         []compiledForm
	sections      []compiledSection
}

type compiledPronunciation struct {
	selector Selector
	region   string
	ipa      Selector
	audio    []string
	noAudio  bool
}

type compiledForm struct {
	container Selector
	label     Selector
	word      Selector
	name      string
}

type compiledSection struct {
	selector   Selector
	items      Selector
	kind       SectionKind
	title      string
	lemma      Selector
	body       Selector
	stripTitle Selector
}

// Compile prepares the profile's selectors. It is idempotent.
func (p *Profile) Compile() {
	if p.compiled != nil {
		return
	}
	c := &compiledProfile{
		root:            ParseSelectors(p.Root),
		ignore:          ParseSelectors(p.Ignore),
		headword:        ParseSelectors(p.Headword),
		translation:     ParseSelectors(p.Translation),
		partBlock:       ParseSelectors(p.PartBlock),
		pos:             ParseSelectors(p.POS),
		grammar:         ParseSelectors(p.Grammar),
		sense:           ParseSelectors(p.Sense),
		senseNumber:     ParseSelectors(p.SenseNumber),
		subsense:        ParseSelectors(p.Subsense),
		definition:      ParseSelectors(p.Definition),
		definitionStrip: ParseSelectors(p.DefinitionStrip),
		labels:          ParseSelectors(p.Labels),
		topic:           ParseSelectors(p.Topic),
		patterns:        ParseSelectors(p.Patterns),
		example:         ParseSelectors(p.Example),
		exampleText:     ParseSelectors(p.ExampleText),
		synonyms:        ParseSelectors(p.Synonyms),
		antonyms:        ParseSelectors(p.Antonyms),
		wordFamily:      ParseSelectors(p.WordFamily),
	}
	for _, raw := range p.Match.Selectors {
		c.matchSel = append(c.matchSel, ParseSelector(raw))
	}
	for _, raw := range p.Match.TitleContains {
		c.titleRes = append(c.titleRes, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(raw)))
	}
	for _, rule := range p.Pronunciation {
		c.pronunciation = append(c.pronunciation, compiledPronunciation{
			selector: ParseSelectors(rule.Selector),
			region:   strings.ToLower(strings.TrimSpace(rule.Region)),
			ipa:      ParseSelectors(rule.IPA),
			audio:    rule.Audio,
			noAudio:  rule.NoAudio,
		})
	}
	for _, rule := range p.Forms {
		c.forms = append(c.forms, compiledForm{
			container: ParseSelectors(rule.Container),
			label:     ParseSelectors(rule.Label),
			word:      ParseSelectors(rule.Word),
			name:      rule.Name,
		})
	}
	for _, rule := range p.Sections {
		c.sections = append(c.sections, compiledSection{
			selector:   ParseSelectors(rule.Selector),
			items:      ParseSelectors(rule.Items),
			kind:       rule.Kind,
			title:      rule.Title,
			lemma:      ParseSelectors(rule.Lemma),
			body:       ParseSelectors(rule.Body),
			stripTitle: ParseSelectors(rule.StripTitle),
		})
	}
	p.compiled = c
}

// Fingerprint scores how well this profile matches a dictionary sample.
// A score of 0 means "does not apply".
func (p *Profile) Fingerprint(title string, sample *html.Node) int {
	p.Compile()
	score := 0
	for _, re := range p.compiled.titleRes {
		if re.MatchString(title) {
			score += 2
		}
	}
	if len(p.compiled.matchSel) > 0 {
		matched := 0
		for _, sel := range p.compiled.matchSel {
			if Query(sample, sel) != nil {
				matched++
			}
		}
		// Structural fingerprints must match in full: a partial match usually
		// means a different dictionary that happens to share one class name.
		if matched < len(p.compiled.matchSel) {
			return 0
		}
		score += 5 * matched
	} else if score == 0 {
		return 0
	}
	return score
}

// LoadProfiles decodes a JSON array of profiles.
func LoadProfiles(data []byte) ([]*Profile, error) {
	var profiles []*Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("decode profiles: %w", err)
	}
	for _, profile := range profiles {
		profile.Compile()
	}
	return profiles, nil
}
