// Package bobadapter renders the dictionary-neutral Entry IR into Bob's
// documented toDict structure. Bob-specific compatibility decisions stay here
// and never rewrite dictionary facts in the IR.
package bobadapter

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

type TTS struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Phonetic struct {
	// Bob documents only "uk" and "us" carrier slots.
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
	TTS   *TTS   `json:"tts,omitempty"`
}

type Part struct {
	Part  string   `json:"part"`
	Means []string `json:"means"`
}

type Exchange struct {
	Name  string   `json:"name"`
	Words []string `json:"words"`
}

// RelatedWordPart and RelatedWord mirror Bob's documented related-word
// presentation schema. Means is optional, so cross-references can be expressed
// without inventing definitions that are not present in the dictionary.
type RelatedWordPart struct {
	Part  string        `json:"part,omitempty"`
	Words []RelatedWord `json:"words"`
}

type RelatedWord struct {
	Word  string   `json:"word"`
	Means []string `json:"means,omitempty"`
}

type Addition struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Dict struct {
	Word             string            `json:"word"`
	Phonetics        []Phonetic        `json:"phonetics,omitempty"`
	Parts            []Part            `json:"parts,omitempty"`
	Exchanges        []Exchange        `json:"exchanges,omitempty"`
	RelatedWordParts []RelatedWordPart `json:"relatedWordParts,omitempty"`
	Additions        []Addition        `json:"additions,omitempty"`
}

// siblingPreviewMaxRunes is deliberately conservative for Bob's compact
// related-word layout. Longer source text is cut before Bob can clip it without
// showing our explicit ellipsis.
const siblingPreviewMaxRunes = 80

type Options struct {
	IncludeExamples     bool
	IncludeExtras       bool
	MaxExamplesPerSense int
	MultiRecordMode     MultiRecordMode
	RecordOrdinal       int
}

// MultiRecordMode controls only Bob presentation. The service cache always
// keeps the complete EntrySet, regardless of this value.
type MultiRecordMode string

const (
	MultiRecordSeparate MultiRecordMode = "separate"
	MultiRecordCombined MultiRecordMode = "combined"
)

func DefaultOptions() Options {
	return Options{
		IncludeExamples:     true,
		IncludeExtras:       true,
		MaxExamplesPerSense: 8,
		MultiRecordMode:     MultiRecordSeparate,
	}
}

// ShouldUsePlainFallback reports whether the selected Bob presentation would
// be an empty shell around untyped free-form Entry/headword sections. The test is
// intentionally conservative: long or dense structured entries remain Bob
// cards, and a single structured record keeps combined mode in Bob as well.
func ShouldUsePlainFallback(set *entryir.EntrySet, opts Options) bool {
	if set == nil || len(set.Records) == 0 {
		return false
	}
	if opts.RecordOrdinal > 0 || opts.MultiRecordMode == MultiRecordSeparate || opts.MultiRecordMode == "" {
		selected := opts.RecordOrdinal
		if selected <= 0 {
			selected = 1
		}
		return selected <= len(set.Records) && isFreeFormFallback(set.Records[selected-1].Entry)
	}
	found := false
	for _, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		found = true
		if !isFreeFormFallback(record.Entry) {
			return false
		}
	}
	return found
}

func isFreeFormFallback(entry *entryir.Entry) bool {
	if entry == nil || len(entry.Forms) != 0 ||
		len(entry.Phrases) != 0 || len(entry.Idioms) != 0 || len(entry.PhrasalVerbs) != 0 ||
		len(entry.Derivatives) != 0 || len(entry.Collocations) != 0 ||
		len(entry.UsageNotes) != 0 || len(entry.GrammarNotes) != 0 ||
		len(entry.Synonyms) != 0 || len(entry.Antonyms) != 0 ||
		len(entry.CrossReferences) != 0 || len(entry.Related) != 0 ||
		len(entry.WordFamily) != 0 || strings.TrimSpace(entry.Etymology) != "" {
		return false
	}
	if len(entry.Parts) > 0 {
		return len(entry.Sections) == 0 && weakUntypedMarkerParts(entry.Parts)
	}
	if len(entry.Sections) == 0 {
		return false
	}
	foundContent := false
	headword := strings.TrimSpace(entry.Headword)
	for _, section := range entry.Sections {
		title := strings.TrimSpace(section.Title)
		if !strings.EqualFold(title, "Entry") && (headword == "" || title != headword) {
			return false
		}
		if strings.TrimSpace(section.Body) != "" || len(section.Blocks) != 0 {
			foundContent = true
		}
	}
	return foundContent
}

// weakUntypedMarkerParts identifies the narrow case where generic recovery
// found numbered blocks but no publisher-declared POS or other typed evidence.
// Bob can technically draw these as blank-column Parts, but Plain preserves
// their article-like reading more honestly. Stronger generic sense classes
// (for example Cambridge's `generic:senseHints`) remain dictionary cards.
func weakUntypedMarkerParts(parts []entryir.Part) bool {
	for _, part := range parts {
		if strings.TrimSpace(part.POS) != "" || strings.TrimSpace(part.Grammar) != "" ||
			part.Rule != "generic:markerBlocks" || part.Confidence > 0.4 || len(part.Senses) == 0 {
			return false
		}
		for _, sense := range part.Senses {
			if sense.Rule != "generic:markerBlocks" || strings.TrimSpace(sense.Grammar) != "" ||
				len(sense.Labels) != 0 || strings.TrimSpace(sense.Topic) != "" || len(sense.Patterns) != 0 ||
				len(sense.Synonyms) != 0 || len(sense.Antonyms) != 0 || len(sense.Subsenses) != 0 {
				return false
			}
		}
	}
	return len(parts) > 0
}

// Render is the single-record convenience renderer. RenderEntrySet owns the
// presentation implementation so single- and multi-record paths cannot drift.
func Render(entry *entryir.Entry, opts Options) *Dict {
	if entry == nil {
		return nil
	}
	lookupKey := strings.TrimSpace(entry.Source.RedirectedFrom)
	if lookupKey == "" {
		lookupKey = strings.TrimSpace(entry.Source.MatchedKey)
	}
	if lookupKey == "" {
		lookupKey = entry.Headword
	}
	return RenderEntrySet(&entryir.EntrySet{
		LookupKey: lookupKey,
		Headword:  entry.Headword,
		Records:   []entryir.EntryRecord{{RecordOrdinal: 1, Entry: entry}},
	}, opts)
}

// RenderEntrySet presents one cached semantic EntrySet. Combined mode renders
// every record with ordinal labels. Separate mode (and any explicit ordinal)
// renders one ordinary record plus native related-word sibling navigation.
func RenderEntrySet(set *entryir.EntrySet, opts Options) *Dict {
	if set == nil || len(set.Records) == 0 {
		return nil
	}
	if opts.MaxExamplesPerSense <= 0 {
		opts.MaxExamplesPerSense = DefaultOptions().MaxExamplesPerSense
	}

	lookupKey := strings.TrimSpace(set.LookupKey)
	if lookupKey == "" {
		lookupKey = strings.TrimSpace(set.Headword)
	}
	if lookupKey == "" && set.Primary() != nil {
		lookupKey = set.Primary().Headword
	}
	if opts.MultiRecordMode == "" {
		opts.MultiRecordMode = MultiRecordSeparate
	}
	if opts.RecordOrdinal > 0 || opts.MultiRecordMode == MultiRecordSeparate {
		selected := opts.RecordOrdinal
		if selected == 0 {
			selected = 1
		}
		return renderSelectedRecord(set, lookupKey, selected, opts, opts.RecordOrdinal > 0)
	}

	dict := &Dict{Word: lookupKey}
	multi := len(set.Records) > 1
	for index, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		ordinal := ""
		if multi {
			value := record.RecordOrdinal
			if value <= 0 {
				value = index + 1
			}
			ordinal = superscriptOrdinal(value)
		}
		phonetics, pronunciationNotes := renderPhonetics(record.Entry.Pronunciations)
		for i := range phonetics {
			phonetics[i].Value = appendPhoneticOrdinal(phonetics[i].Value, ordinal)
		}
		dict.Phonetics = append(dict.Phonetics, phonetics...)
		renderEntry(dict, record.Entry, opts, ordinal)
		for _, note := range pronunciationNotes {
			appendTextAddition(dict, ordinalLabel(ordinal, "发音说明"), note)
		}
	}
	return dict
}

func renderSelectedRecord(set *entryir.EntrySet, headword string, selected int, opts Options, explicit bool) *Dict {
	if selected < 1 || selected > len(set.Records) {
		return nil
	}
	record := set.Records[selected-1]
	if record.Entry == nil {
		return nil
	}
	word := headword
	if explicit {
		word += superscriptOrdinal(selected)
	}
	dict := &Dict{Word: word}
	phonetics, pronunciationNotes := renderPhonetics(record.Entry.Pronunciations)
	dict.Phonetics = append(dict.Phonetics, phonetics...)
	renderEntry(dict, record.Entry, opts, "")
	for _, note := range pronunciationNotes {
		appendTextAddition(dict, "发音说明", note)
	}
	appendSiblingNavigation(dict, set, selected, headword)
	return dict
}

func appendSiblingNavigation(dict *Dict, set *entryir.EntrySet, selected int, headword string) {
	if len(set.Records) <= 1 {
		return
	}
	words := make([]RelatedWord, 0, len(set.Records)-1)
	for index, record := range set.Records {
		ordinal := record.RecordOrdinal
		if ordinal <= 0 {
			ordinal = index + 1
		}
		if ordinal == selected || record.Entry == nil {
			continue
		}
		related := RelatedWord{Word: headword + superscriptOrdinal(ordinal)}
		if preview := recordPreview(record.Entry, siblingPreviewMaxRunes); preview != "" {
			related.Means = []string{preview}
		}
		words = append(words, related)
	}
	if len(words) > 0 {
		dict.RelatedWordParts = append(dict.RelatedWordParts, RelatedWordPart{
			Part:  "Other entries",
			Words: words,
		})
	}
}

func recordPreview(entry *entryir.Entry, maxRunes int) string {
	if entry == nil {
		return ""
	}
	firstSense := ""
	senseCount := 0
	for _, part := range entry.Parts {
		var visitSense func(entryir.Sense)
		visitSense = func(sense entryir.Sense) {
			content := sensePreviewText(sense)
			if content != "" {
				senseCount++
				if firstSense == "" {
					if pos := strings.TrimSpace(part.POS); pos != "" {
						content = pos + " · " + content
					}
					firstSense = content
				}
			}
			for _, subsense := range sense.Subsenses {
				visitSense(subsense)
			}
		}
		for _, sense := range part.Senses {
			visitSense(sense)
		}
	}
	if firstSense != "" {
		return truncatePreview(firstSense, maxRunes, senseCount > 1)
	}

	firstSection := ""
	sectionCount := 0
	for _, section := range entry.Sections {
		if body := strings.TrimSpace(section.Body); body != "" {
			sectionCount++
			if firstSection == "" {
				firstSection = body
			}
		}
	}
	if firstSection != "" {
		return truncatePreview(firstSection, maxRunes, sectionCount > 1)
	}

	firstPhrase := ""
	phraseCount := 0
	for _, entries := range [][]entryir.PhraseEntry{entry.Phrases, entry.Idioms, entry.PhrasalVerbs, entry.Derivatives} {
		for _, phrase := range entries {
			text := strings.TrimSpace(phrase.Phrase)
			definition := strings.TrimSpace(phrase.Definition)
			if text != "" && definition != "" {
				text += " — " + definition
			} else if text == "" {
				text = definition
			}
			if text != "" {
				phraseCount++
				if firstPhrase == "" {
					firstPhrase = text
				}
			}
		}
	}
	return truncatePreview(firstPhrase, maxRunes, phraseCount > 1)
}

func sensePreviewText(sense entryir.Sense) string {
	content := strings.TrimSpace(sense.Definition)
	translation := strings.TrimSpace(sense.Translation)
	if content != "" && translation != "" {
		return content + " — " + translation
	}
	if content == "" {
		return translation
	}
	return content
}

func truncatePreview(value string, maxRunes int, hasMore bool) string {
	value = strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
	if value == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = strings.TrimSpace(string(runes[:maxRunes]))
		hasMore = true
	}
	if hasMore && !strings.HasSuffix(value, "…") && !strings.HasSuffix(value, "...") {
		value += "…"
	}
	return value
}

type carrier struct {
	kind       string
	ipa        string
	audio      *entryir.Audio
	annotation string
}

func (c carrier) occupied() bool { return c.ipa != "" || c.audio != nil }

// renderPhonetics maps faithful IR facts onto Bob's two carrier slots. Shared
// or unknown provenance is annotated after the IPA; unknown facts remain
// unchanged in the source IR.
func renderPhonetics(items []entryir.Pronunciation) ([]Phonetic, []string) {
	carriers := map[entryir.Region]*carrier{
		entryir.RegionUK: {kind: "uk"},
		entryir.RegionUS: {kind: "us"},
	}
	var neutralIPA, unknownIPA string
	var unknownAudio *entryir.Audio
	var notes []string

	for i := range items {
		item := &items[i]
		if item.IPA != "" {
			switch item.IPARegion {
			case entryir.RegionUK, entryir.RegionUS:
				if carriers[item.IPARegion].ipa == "" {
					carriers[item.IPARegion].ipa = item.IPA
				}
			case entryir.RegionNeutral:
				if neutralIPA == "" {
					neutralIPA = item.IPA
				}
			default:
				if unknownIPA == "" {
					unknownIPA = item.IPA
				}
			}
		}
		if item.Audio != nil {
			switch item.AudioRegion {
			case entryir.RegionUK, entryir.RegionUS:
				if carriers[item.AudioRegion].audio == nil {
					carriers[item.AudioRegion].audio = item.Audio
				}
			default:
				if unknownAudio == nil {
					unknownAudio = item.Audio
				}
			}
		}
	}

	// A source-declared shared transcription may be carried by both regional
	// recordings. If there are no regional facts, show it once.
	if neutralIPA != "" {
		used := false
		for _, region := range []entryir.Region{entryir.RegionUK, entryir.RegionUS} {
			candidate := carriers[region]
			if candidate.occupied() && candidate.ipa == "" {
				candidate.ipa = neutralIPA
				candidate.annotation = "共用音标"
				used = true
			}
		}
		if !used {
			var target *carrier
			for _, region := range []entryir.Region{entryir.RegionUK, entryir.RegionUS} {
				if !carriers[region].occupied() {
					target = carriers[region]
					break
				}
			}
			if target != nil {
				target.ipa = neutralIPA
				target.annotation = "共用音标"
			} else {
				notes = append(notes, "原词典还提供共用音标 "+neutralIPA+"；Bob 的两个发音槽已被占用，无法同时展示。")
			}
		}
	}

	if unknownIPA != "" || unknownAudio != nil {
		var target *carrier
		for _, region := range []entryir.Region{entryir.RegionUK, entryir.RegionUS} {
			if !carriers[region].occupied() {
				target = carriers[region]
				break
			}
		}
		if target != nil {
			target.ipa = unknownIPA
			target.audio = unknownAudio
			target.annotation = "未标口音"
			if unknownIPA == "" && unknownAudio != nil {
				notes = append(notes, "该词典音频在原词典中未标注英/美口音；Bob 仅提供英式和美式发音槽。")
			}
		} else {
			if unknownIPA != "" {
				notes = append(notes, "原词典还提供未标注口音的音标 "+unknownIPA+"；Bob 的两个发音槽已被占用，无法同时展示。")
			}
			if unknownAudio != nil {
				notes = append(notes, "原词典还提供一条未标注英/美口音的音频；Bob 的两个发音槽已被占用，无法同时展示。")
			}
		}
	}

	var out []Phonetic
	for _, region := range []entryir.Region{entryir.RegionUK, entryir.RegionUS} {
		candidate := carriers[region]
		if !candidate.occupied() {
			continue
		}
		value := candidate.ipa
		if value != "" && candidate.annotation != "" {
			value += " · " + candidate.annotation
		}
		phonetic := Phonetic{Type: candidate.kind, Value: value}
		if candidate.audio != nil {
			phonetic.TTS = &TTS{Type: "url", Value: candidate.audio.URL}
		}
		out = append(out, phonetic)
	}
	dedupeStrings(&notes)
	return out, notes
}

func renderEntry(dict *Dict, entry *entryir.Entry, opts Options, ordinal string) {
	for _, part := range entry.Parts {
		// Bob's left column is narrow and semantically only a POS carrier. Grammar
		// belongs with the meaning, and an absent POS stays absent rather than
		// leaking an implementation label such as "definition".
		label := CompactPOS(part.POS)
		label = ordinalLabel(ordinal, label)
		for i, sense := range part.Senses {
			appendFlattenedSenseParts(dict, label, part.Grammar, sense, []int{i + 1})
		}
		if opts.IncludeExamples {
			appendExampleAdditions(dict, label, part.Senses, opts.MaxExamplesPerSense)
		}
	}

	for _, form := range entry.Forms {
		dict.Exchanges = append(dict.Exchanges, Exchange{Name: ordinalLabel(ordinal, form.Name), Words: form.Words})
	}
	if !opts.IncludeExtras {
		return
	}
	appendPhraseParts(dict, ordinalLabel(ordinal, "phr."), entry.Phrases, opts.IncludeExamples, opts.MaxExamplesPerSense)
	appendPhraseParts(dict, ordinalLabel(ordinal, "idiom"), entry.Idioms, opts.IncludeExamples, opts.MaxExamplesPerSense)
	appendPhraseParts(dict, ordinalLabel(ordinal, "phr. v."), entry.PhrasalVerbs, opts.IncludeExamples, opts.MaxExamplesPerSense)
	appendPhraseAddition(dict, ordinalLabel(ordinal, "Derivatives"), entry.Derivatives)
	seenRelatedWords := make(map[string]struct{}, len(entry.CrossReferences)+len(entry.Related))
	appendRelatedWordPart(dict, ordinalLabel(ordinal, "See also"), entry.CrossReferences, seenRelatedWords)
	appendRelatedWordPart(dict, ordinalLabel(ordinal, "Related"), entry.Related, seenRelatedWords)
	appendListParts(dict, ordinalLabel(ordinal, "colloc."), entry.Collocations)
	appendListAddition(dict, ordinalLabel(ordinal, "Synonyms"), entry.Synonyms)
	appendListAddition(dict, ordinalLabel(ordinal, "Antonyms"), entry.Antonyms)
	appendListAddition(dict, ordinalLabel(ordinal, "Word family"), entry.WordFamily)
	for _, note := range entry.UsageNotes {
		appendTextAddition(dict, ordinalLabel(ordinal, "Usage · "+note.Title), note.Body)
	}
	for _, note := range entry.GrammarNotes {
		appendTextAddition(dict, ordinalLabel(ordinal, "Grammar · "+note.Title), note.Body)
	}
	appendTextAddition(dict, ordinalLabel(ordinal, "Origin"), entry.Etymology)
	for _, section := range entry.Sections {
		appendTextAddition(dict, ordinalLabel(ordinal, section.Title), section.Body)
	}
}

var compactPOS = map[string]string{
	"noun":              "n.",
	"verb":              "v.",
	"transitive verb":   "vt.",
	"intransitive verb": "vi.",
	"adjective":         "adj.",
	"adverb":            "adv.",
	"preposition":       "prep.",
	"conjunction":       "conj.",
	"pronoun":           "pron.",
	"determiner":        "det.",
	"article":           "art.",
	"number":            "num.",
	"interjection":      "interj.",
	"modal verb":        "modal v.",
	"auxiliary verb":    "aux. v.",
	"abbreviation":      "abbr.",
	"combining form":    "comb. form",
	"prefix":            "pref.",
	"suffix":            "suff.",
	"symbol":            "symb.",
}

// CompactPOS applies Bob-only abbreviations. Unknown non-empty labels are
// preserved verbatim: an unfamiliar publisher category must never disappear.
func CompactPOS(pos string) string {
	trimmed := strings.TrimSpace(pos)
	if compact, ok := compactPOS[strings.ToLower(trimmed)]; ok {
		return compact
	}
	return trimmed
}

func ordinalLabel(ordinal, label string) string {
	if ordinal == "" {
		return label
	}
	if label == "" {
		return ordinal
	}
	return ordinal + " " + label
}

func appendPhoneticOrdinal(value, ordinal string) string {
	if ordinal == "" {
		return value
	}
	if value == "" {
		return ordinal
	}
	return value + " · " + ordinal
}

func superscriptOrdinal(value int) string {
	if value <= 0 {
		return ""
	}
	digits := [...]rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}
	plain := fmt.Sprintf("%d", value)
	var builder strings.Builder
	for _, digit := range plain {
		builder.WriteRune(digits[digit-'0'])
	}
	return builder.String()
}

// appendFlattenedSenseParts recursively gives every semantic sense node its own
// top-level Bob Part. Bob has no nested Part schema and concatenates Means, so
// hierarchy is expressed only by the stable display number on each unit.
func appendFlattenedSenseParts(dict *Dict, label, partGrammar string, sense entryir.Sense, displayPath []int) {
	if line := renderSense(sense, displayPath, partGrammar); line != "" {
		dict.Parts = append(dict.Parts, Part{Part: label, Means: []string{line}})
	}
	for i, sub := range sense.Subsenses {
		path := append(append([]int(nil), displayPath...), i+1)
		appendFlattenedSenseParts(dict, label, partGrammar, sub, path)
	}
}

// renderSense generates presentation numbering from position within the Bob
// POS group. Source numbering remains untouched in entryir.Sense.Number.
func renderSense(sense entryir.Sense, displayPath []int, partGrammar string) string {
	var builder strings.Builder
	if grammar := combinedGrammar(partGrammar, sense.Grammar); grammar != "" {
		builder.WriteString(grammar)
		builder.WriteString(" ")
	}
	builder.WriteString(formatDisplayNumber(displayPath))
	builder.WriteString(". ")
	if len(sense.Labels) > 0 {
		builder.WriteString("(")
		builder.WriteString(strings.Join(sense.Labels, ", "))
		builder.WriteString(") ")
	}
	if sense.Topic != "" {
		builder.WriteString("[")
		builder.WriteString(sense.Topic)
		builder.WriteString("] ")
	}
	builder.WriteString(sense.Definition)
	if sense.Translation != "" {
		if sense.Definition != "" {
			builder.WriteString(" — ")
		}
		builder.WriteString(sense.Translation)
	}
	if len(sense.Patterns) > 0 {
		builder.WriteString(" · ")
		builder.WriteString(strings.Join(sense.Patterns, " / "))
	}

	line := strings.TrimSpace(builder.String())
	if line == formatDisplayNumber(displayPath)+"." {
		return ""
	}
	return line
}

func combinedGrammar(partGrammar, senseGrammar string) string {
	partGrammar = strings.TrimSpace(partGrammar)
	senseGrammar = strings.TrimSpace(senseGrammar)
	if partGrammar == "" {
		return senseGrammar
	}
	if senseGrammar == "" || equivalentGrammar(partGrammar, senseGrammar) {
		return partGrammar
	}
	return partGrammar + " " + senseGrammar
}

func equivalentGrammar(left, right string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "[](){}")
		return strings.Join(strings.Fields(value), " ")
	}
	return normalize(left) == normalize(right)
}

func appendExampleAdditions(dict *Dict, label string, senses []entryir.Sense, limit int) {
	var walk func([]entryir.Sense, []int)
	walk = func(senses []entryir.Sense, parent []int) {
		for i, sense := range senses {
			path := append(append([]int(nil), parent...), i+1)
			var examples []string
			for exampleIndex, example := range sense.Examples {
				if exampleIndex >= limit {
					break
				}
				line := "• " + example.Text
				if example.Translation != "" {
					line += "\n  — " + example.Translation
				}
				examples = append(examples, line)
			}
			if len(examples) > 0 {
				name := "Examples"
				if label = strings.TrimSpace(label); label != "" {
					name += " · " + label
				}
				name += " " + formatDisplayNumber(path)
				dict.Additions = append(dict.Additions, Addition{
					Name:  name,
					Value: strings.Join(examples, "\n\n"),
				})
			}
			walk(sense.Subsenses, path)
		}
	}
	walk(senses, nil)
}

func formatDisplayNumber(path []int) string {
	parts := make([]string, len(path))
	for i, value := range path {
		parts[i] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, ".")
}

func appendPhraseAddition(dict *Dict, name string, entries []entryir.PhraseEntry) {
	var lines []string
	for _, entry := range entries {
		line := entry.Phrase
		if entry.Definition != "" {
			if line != "" {
				line += " — "
			}
			line += entry.Definition
		}
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	appendTextAddition(dict, name, strings.Join(lines, "\n"))
}

func appendPhraseParts(dict *Dict, label string, entries []entryir.PhraseEntry, includeExamples bool, limit int) {
	for index, entry := range entries {
		line := strings.TrimSpace(entry.Phrase)
		if definition := strings.TrimSpace(entry.Definition); definition != "" {
			if line != "" {
				line += " — "
			}
			line += definition
		}
		if line != "" {
			dict.Parts = append(dict.Parts, Part{Part: label, Means: []string{line}})
		}
		if !includeExamples || len(entry.Examples) == 0 {
			continue
		}
		var examples []string
		for exampleIndex, example := range entry.Examples {
			if exampleIndex >= limit {
				break
			}
			exampleLine := "• " + example.Text
			if example.Translation != "" {
				exampleLine += "\n  — " + example.Translation
			}
			examples = append(examples, exampleLine)
		}
		appendTextAddition(dict, fmt.Sprintf("Examples · %s %d", strings.TrimSpace(label), index+1), strings.Join(examples, "\n\n"))
	}
}

func appendListParts(dict *Dict, label string, values []string) {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			dict.Parts = append(dict.Parts, Part{Part: label, Means: []string{value}})
		}
	}
}

func appendListAddition(dict *Dict, name string, values []string) {
	appendTextAddition(dict, name, strings.Join(values, "\n"))
}

func appendRelatedWordPart(dict *Dict, part string, values []string, seen map[string]struct{}) {
	words := make([]RelatedWord, 0, len(values))
	for _, value := range values {
		word := strings.TrimSpace(value)
		if word == "" {
			continue
		}
		// Exact, case-sensitive comparison preserves legitimate distinctions
		// such as US/us while removing repeated presentation targets.
		if _, exists := seen[word]; exists {
			continue
		}
		seen[word] = struct{}{}
		words = append(words, RelatedWord{Word: word})
	}
	if len(words) > 0 {
		dict.RelatedWordParts = append(dict.RelatedWordParts, RelatedWordPart{Part: part, Words: words})
	}
}

func appendTextAddition(dict *Dict, name, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for i := range dict.Additions {
		if dict.Additions[i].Name == name {
			if !strings.Contains(dict.Additions[i].Value, value) {
				dict.Additions[i].Value += "\n" + value
			}
			return
		}
	}
	dict.Additions = append(dict.Additions, Addition{Name: name, Value: value})
}

func dedupeStrings(values *[]string) {
	seen := make(map[string]struct{}, len(*values))
	out := (*values)[:0]
	for _, value := range *values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	*values = out
}
