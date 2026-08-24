// Package bobadapter renders the dictionary-neutral Entry IR into Bob's
// toDict structure.
//
// It is deliberately the only place in the service that knows anything about
// Bob. The IR stays a faithful model of what dictionaries contain, and when Bob
// gains richer display capabilities only this file changes.
package bobadapter

import (
	"fmt"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// TTS is Bob's audio reference.
type TTS struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Phonetic is one entry of toDict.phonetics.
type Phonetic struct {
	// Type is "uk" or "us". Bob defines no other value.
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
	TTS   *TTS   `json:"tts,omitempty"`
}

// Part is one entry of toDict.parts.
type Part struct {
	Part  string   `json:"part"`
	Means []string `json:"means"`
}

// Exchange is one entry of toDict.exchanges.
type Exchange struct {
	Name  string   `json:"name"`
	Words []string `json:"words"`
}

// Addition is one entry of toDict.additions.
type Addition struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Dict is Bob's toDict object.
type Dict struct {
	Word      string     `json:"word"`
	Phonetics []Phonetic `json:"phonetics,omitempty"`
	Parts     []Part     `json:"parts,omitempty"`
	Exchanges []Exchange `json:"exchanges,omitempty"`
	Additions []Addition `json:"additions,omitempty"`
}

// Options configures rendering.
type Options struct {
	// MultipleDictionaries labels every part and addition with its source, so
	// senses from different dictionaries never read as one list.
	MultipleDictionaries bool
	// IncludeExamples emits example sentences as additions.
	IncludeExamples bool
	// IncludeExtras emits phrases, idioms, usage notes, etymology and so on.
	IncludeExtras bool
	// MaxExamplesPerPart caps how many examples one part contributes.
	MaxExamplesPerPart int
}

// DefaultOptions returns sensible display settings.
func DefaultOptions() Options {
	return Options{IncludeExamples: true, IncludeExtras: true, MaxExamplesPerPart: 8}
}

// Source pairs an entry with the dictionary it came from.
type Source struct {
	DictionaryTitle string
	Entry           *entryir.Entry
}

// Render converts one or more dictionary entries into a single toDict.
func Render(sources []Source, opts Options) *Dict {
	if len(sources) == 0 {
		return nil
	}
	if opts.MaxExamplesPerPart <= 0 {
		opts.MaxExamplesPerPart = DefaultOptions().MaxExamplesPerPart
	}

	dict := &Dict{Word: sources[0].Entry.Headword}
	dict.Phonetics = renderPhonetics(sources)

	for _, source := range sources {
		prefix := ""
		if opts.MultipleDictionaries {
			prefix = shortTitle(source.DictionaryTitle) + " · "
		}
		renderEntry(dict, source.Entry, prefix, opts)
	}
	return dict
}

// renderPhonetics builds Bob's phonetics list.
//
// Bob only models uk and us. A pronunciation with no regional evidence is not
// promoted into one of them unless it is the entry's only pronunciation and it
// has audio the user would otherwise be unable to play at all.
func renderPhonetics(sources []Source) []Phonetic {
	var uk, us, other *entryir.Pronunciation
	for _, source := range sources {
		for i := range source.Entry.Pronunciations {
			item := &source.Entry.Pronunciations[i]
			switch item.Region {
			case entryir.RegionUK:
				uk = preferRicher(uk, item)
			case entryir.RegionUS:
				us = preferRicher(us, item)
			default:
				other = preferRicher(other, item)
			}
		}
	}
	if uk == nil && us == nil && other != nil {
		// Bob has no neutral slot. Presenting a single unlabelled pronunciation
		// under "uk" is the only way to surface it, and losing real dictionary
		// audio is the worse outcome.
		uk = other
	}

	var out []Phonetic
	if phonetic, ok := toPhonetic("uk", uk); ok {
		out = append(out, phonetic)
	}
	if phonetic, ok := toPhonetic("us", us); ok {
		out = append(out, phonetic)
	}
	return out
}

// preferRicher keeps whichever candidate carries more information.
func preferRicher(current, candidate *entryir.Pronunciation) *entryir.Pronunciation {
	if current == nil {
		return candidate
	}
	score := func(p *entryir.Pronunciation) int {
		total := 0
		if p.IPA != "" {
			total += 2
		}
		if p.Audio != nil {
			total += 3
		}
		return total
	}
	if score(candidate) > score(current) {
		return candidate
	}
	return current
}

func toPhonetic(kind string, item *entryir.Pronunciation) (Phonetic, bool) {
	if item == nil || (item.IPA == "" && item.Audio == nil) {
		return Phonetic{}, false
	}
	phonetic := Phonetic{Type: kind, Value: item.IPA}
	if item.Audio != nil {
		// The URL points at this machine's own service, which streams the
		// pronunciation straight out of the user's MDD. Nothing is synthesized.
		phonetic.TTS = &TTS{Type: "url", Value: item.Audio.URL}
	}
	return phonetic, true
}

func renderEntry(dict *Dict, entry *entryir.Entry, prefix string, opts Options) {
	for _, part := range entry.Parts {
		label := part.POS
		if label == "" {
			label = "definition"
		}
		if part.Grammar != "" {
			label = label + " " + part.Grammar
		}
		means := make([]string, 0, len(part.Senses))
		for i, sense := range part.Senses {
			means = append(means, renderSense(sense, i+1, 0)...)
		}
		if len(means) == 0 {
			continue
		}
		dict.Parts = append(dict.Parts, Part{Part: prefix + label, Means: means})

		if opts.IncludeExamples {
			if value := renderExamples(part, opts.MaxExamplesPerPart); value != "" {
				dict.Additions = append(dict.Additions, Addition{
					Name:  prefix + "Examples · " + label,
					Value: value,
				})
			}
		}
	}

	for _, form := range entry.Forms {
		dict.Exchanges = append(dict.Exchanges, Exchange{Name: form.Name, Words: form.Words})
	}

	if !opts.IncludeExtras {
		return
	}
	appendPhraseAddition(dict, prefix+"Phrases", entry.Phrases)
	appendPhraseAddition(dict, prefix+"Idioms", entry.Idioms)
	appendPhraseAddition(dict, prefix+"Phrasal verbs", entry.PhrasalVerbs)
	appendListAddition(dict, prefix+"Collocations", entry.Collocations)
	appendListAddition(dict, prefix+"Synonyms", entry.Synonyms)
	appendListAddition(dict, prefix+"Antonyms", entry.Antonyms)
	appendListAddition(dict, prefix+"Word family", entry.WordFamily)
	for _, note := range entry.UsageNotes {
		appendTextAddition(dict, prefix+"Usage · "+note.Title, note.Body)
	}
	for _, note := range entry.GrammarNotes {
		appendTextAddition(dict, prefix+"Grammar · "+note.Title, note.Body)
	}
	appendTextAddition(dict, prefix+"Origin", entry.Etymology)
	for _, section := range entry.Sections {
		appendTextAddition(dict, prefix+section.Title, section.Body)
	}
}

// renderSense formats one sense, and its subsenses, as display lines.
//
// Bob's means entries are plain strings, so the sense hierarchy is preserved
// with numbering and indentation rather than being flattened away.
func renderSense(sense entryir.Sense, fallbackNumber, depth int) []string {
	number := sense.Number
	if number == "" && depth == 0 {
		number = fmt.Sprintf("%d", fallbackNumber)
	}

	var builder strings.Builder
	builder.WriteString(strings.Repeat("    ", depth))
	if number != "" {
		builder.WriteString(number)
		builder.WriteString(". ")
	}
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
			builder.WriteString("  ")
		}
		builder.WriteString(sense.Translation)
	}
	if len(sense.Patterns) > 0 {
		builder.WriteString("  [")
		builder.WriteString(strings.Join(sense.Patterns, " / "))
		builder.WriteString("]")
	}

	line := strings.TrimRight(builder.String(), " ")
	var out []string
	if strings.TrimSpace(line) != "" {
		out = append(out, line)
	}
	for i, sub := range sense.Subsenses {
		out = append(out, renderSense(sub, i+1, depth+1)...)
	}
	return out
}

func renderExamples(part entryir.Part, limit int) string {
	var lines []string
	var walk func(senses []entryir.Sense, prefix string)
	walk = func(senses []entryir.Sense, prefix string) {
		for i, sense := range senses {
			number := sense.Number
			if number == "" {
				number = fmt.Sprintf("%d", i+1)
			}
			label := prefix + number
			for _, example := range sense.Examples {
				if len(lines) >= limit {
					return
				}
				line := label + ". " + example.Text
				if example.Translation != "" {
					line += "\n   " + example.Translation
				}
				lines = append(lines, line)
			}
			walk(sense.Subsenses, label+".")
		}
	}
	walk(part.Senses, "")
	return strings.Join(lines, "\n")
}

func appendPhraseAddition(dict *Dict, name string, entries []entryir.PhraseEntry) {
	if len(entries) == 0 {
		return
	}
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
	if len(values) == 0 {
		return
	}
	appendTextAddition(dict, name, strings.Join(values, ", "))
}

func appendTextAddition(dict *Dict, name, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	dict.Additions = append(dict.Additions, Addition{Name: name, Value: value})
}

// shortTitle trims a dictionary title down to something that fits a label.
func shortTitle(title string) string {
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) <= 28 {
		return title
	}
	return string(runes[:27]) + "…"
}
