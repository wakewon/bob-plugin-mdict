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

type Options struct {
	IncludeExamples     bool
	IncludeExtras       bool
	MaxExamplesPerSense int
	MultiRecordMode     MultiRecordMode
	RecordOrdinal       int
	// NavigationHeadword is the canonical base query used only for clickable
	// aliases. It never rewrites EntrySet or Entry facts.
	NavigationHeadword string
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

// Render is the single-record convenience renderer. RenderEntrySet owns the
// presentation implementation so single- and multi-record paths cannot drift.
func Render(entry *entryir.Entry, opts Options) *Dict {
	if entry == nil {
		return nil
	}
	return RenderEntrySet(&entryir.EntrySet{
		Headword: entry.Headword,
		Records:  []entryir.EntryRecord{{RecordOrdinal: 1, Entry: entry}},
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

	headword := set.Headword
	if headword == "" && set.Primary() != nil {
		headword = set.Primary().Headword
	}
	if opts.MultiRecordMode == "" {
		opts.MultiRecordMode = MultiRecordSeparate
	}
	if opts.RecordOrdinal > 0 || opts.MultiRecordMode == MultiRecordSeparate {
		navigationHeadword := strings.TrimSpace(opts.NavigationHeadword)
		if navigationHeadword == "" {
			navigationHeadword = headword
		}
		selected := opts.RecordOrdinal
		if selected == 0 {
			selected = 1
		}
		return renderSelectedRecord(set, navigationHeadword, selected, opts, opts.RecordOrdinal > 0)
	}

	dict := &Dict{Word: headword}
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
		if preview := recordPreview(record.Entry, 100); preview != "" {
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
	for _, part := range entry.Parts {
		for _, sense := range part.Senses {
			content := strings.TrimSpace(sense.Definition)
			translation := strings.TrimSpace(sense.Translation)
			if content != "" && translation != "" {
				content += " — " + translation
			} else if content == "" {
				content = translation
			}
			if content == "" {
				continue
			}
			if pos := strings.TrimSpace(part.POS); pos != "" {
				content = pos + " · " + content
			}
			return truncatePreview(content, maxRunes)
		}
	}
	for _, section := range entry.Sections {
		if body := strings.TrimSpace(section.Body); body != "" {
			return truncatePreview(body, maxRunes)
		}
	}
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
				return truncatePreview(text, maxRunes)
			}
		}
	}
	return ""
}

func truncatePreview(value string, maxRunes int) string {
	value = strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "…"
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
				notes = append(notes, "该真人录音在原词典中未标注英/美口音；Bob 仅提供英式和美式发音槽。")
			}
		} else {
			if unknownIPA != "" {
				notes = append(notes, "原词典还提供未标注口音的音标 "+unknownIPA+"；Bob 的两个发音槽已被占用，无法同时展示。")
			}
			if unknownAudio != nil {
				notes = append(notes, "原词典还提供一条未标注英/美口音的真人录音；Bob 的两个发音槽已被占用，无法同时展示。")
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
		label := part.POS
		if label == "" {
			label = "definition"
		}
		if part.Grammar != "" {
			label += " " + part.Grammar
		}
		label = ordinalLabel(ordinal, label)
		for i, sense := range part.Senses {
			// Bob's parts field is an ordinary array and does not require part
			// labels to be unique. Giving each top-level sense its own Part lets
			// Bob provide the visual separation instead of turning a whole POS
			// group into one dense means block. Subsenses remain with their parent.
			means := renderSense(sense, []int{i + 1})
			if len(means) > 0 {
				dict.Parts = append(dict.Parts, Part{Part: label, Means: means})
			}
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
	appendPhraseAddition(dict, ordinalLabel(ordinal, "Phrases"), entry.Phrases)
	appendPhraseAddition(dict, ordinalLabel(ordinal, "Idioms"), entry.Idioms)
	appendPhraseAddition(dict, ordinalLabel(ordinal, "Phrasal verbs"), entry.PhrasalVerbs)
	appendPhraseAddition(dict, ordinalLabel(ordinal, "Derivatives"), entry.Derivatives)
	seenRelatedWords := make(map[string]struct{}, len(entry.CrossReferences)+len(entry.Related))
	appendRelatedWordPart(dict, ordinalLabel(ordinal, "See also"), entry.CrossReferences, seenRelatedWords)
	appendRelatedWordPart(dict, ordinalLabel(ordinal, "Related"), entry.Related, seenRelatedWords)
	appendListAddition(dict, ordinalLabel(ordinal, "Collocations"), entry.Collocations)
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

// renderSense generates presentation numbering from position within the Bob
// POS group. Source numbering remains untouched in entryir.Sense.Number.
func renderSense(sense entryir.Sense, displayPath []int) []string {
	var builder strings.Builder
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

	var out []string
	if line := strings.TrimSpace(builder.String()); line != "" && line != formatDisplayNumber(displayPath)+"." {
		out = append(out, line)
	}
	for i, sub := range sense.Subsenses {
		path := append(append([]int(nil), displayPath...), i+1)
		out = append(out, renderSense(sub, path)...)
	}
	return out
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
				dict.Additions = append(dict.Additions, Addition{
					Name:  "Examples · " + label + " " + formatDisplayNumber(path),
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
