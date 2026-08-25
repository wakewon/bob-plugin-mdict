package validate

import (
	"sort"
	"strings"
	"unicode"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

// The diagnostics from the previous round answer "did the parser produce
// structure?". They do not answer "is the structure it produced a faithful
// reading of the record?", and structure that is present can still be wrong.
//
// These metrics are the cheap half of that second question. They are not
// accuracy: none of them compares the output against a human reading. What
// they do is find the shapes a wrong parse takes — text that vanished, text
// emitted twice under two different fields, one field that swallowed the whole
// record — so that a person reviews the entries most likely to be wrong
// instead of a random hundred.

// Metrics are the automatic consistency signals for one parsed record.
type Metrics struct {
	// SourceTokens is the record text the parser was actually asked to read:
	// the whole record, less what the profile scopes out. OutputTokens is
	// counted over the same tokenization, so the two are directly comparable.
	SourceTokens int `json:"sourceTokens"`
	OutputTokens int `json:"outputTokens"`
	// RecordTokens is the whole record before scoping, and Scope the fraction
	// of it left in play. A profile that narrows a record to a tenth of itself
	// is making a real editorial decision, and hiding that inside a healthy
	// retention figure would be the wrong kind of tidy.
	RecordTokens int     `json:"recordTokens"`
	Scope        float64 `json:"scope"`
	// Retention is the share of source tokens the semantic output accounts
	// for. Low retention means the parser dropped content on the floor.
	Retention float64 `json:"retention"`
	// Duplication is the share of output tokens in excess of what the source
	// contains. High duplication means one passage was emitted twice.
	Duplication float64 `json:"duplication"`
	// LargestFieldShare is the biggest single extracted field as a share of
	// the record. Near 1 means one field swallowed the entry.
	LargestFieldShare float64 `json:"largestFieldShare"`
	LargestFieldKind  string  `json:"largestFieldKind,omitempty"`

	RepeatedDefinitions  int `json:"repeatedDefinitions,omitempty"`
	RepeatedExamples     int `json:"repeatedExamples,omitempty"`
	SubsenseEchoesParent int `json:"subsenseEchoesParent,omitempty"`
	SectionEchoesSense   int `json:"sectionEchoesSense,omitempty"`
}

// Fields counts what one record's parse produced, by kind.
type Fields struct {
	Parts               int `json:"parts"`
	POSLabels           int `json:"posLabels"`
	Senses              int `json:"senses"`
	Subsenses           int `json:"subsenses"`
	Definitions         int `json:"definitions"`
	Translations        int `json:"translations"`
	Examples            int `json:"examples"`
	ExampleTranslations int `json:"exampleTranslations"`
	Labels              int `json:"labels"`
	IPA                 int `json:"ipa"`
	Audio               int `json:"audio"`
	Forms               int `json:"forms"`
	Phrases             int `json:"phrases"`
	CrossReferences     int `json:"crossReferences"`
	Sections            int `json:"sections"`
	Fallback            int `json:"fallback"`
}

// Total is the number of extracted semantic items, used to notice a parse that
// changed shape without changing count.
func (f Fields) Total() int {
	return f.Parts + f.Senses + f.Subsenses + f.Definitions + f.Translations +
		f.Examples + f.ExampleTranslations + f.Labels + f.IPA + f.Audio +
		f.Forms + f.Phrases + f.CrossReferences + f.Sections
}

// countFields tallies one entry.
func countFields(entry *entryir.Entry) Fields {
	var fields Fields
	if entry == nil {
		return fields
	}
	for _, pronunciation := range entry.Pronunciations {
		if pronunciation.IPA != "" {
			fields.IPA++
		}
		if pronunciation.Audio != nil {
			fields.Audio++
		}
	}
	for _, part := range entry.Parts {
		fields.Parts++
		if strings.TrimSpace(part.POS) != "" {
			fields.POSLabels++
		}
		var walk func(senses []entryir.Sense, depth int)
		walk = func(senses []entryir.Sense, depth int) {
			for _, sense := range senses {
				if depth == 0 {
					fields.Senses++
				} else {
					fields.Subsenses++
				}
				if strings.TrimSpace(sense.Definition) != "" {
					fields.Definitions++
				}
				if strings.TrimSpace(sense.Translation) != "" {
					fields.Translations++
				}
				fields.Labels += len(sense.Labels)
				for _, example := range sense.Examples {
					fields.Examples++
					if strings.TrimSpace(example.Translation) != "" {
						fields.ExampleTranslations++
					}
				}
				walk(sense.Subsenses, depth+1)
			}
		}
		walk(part.Senses, 0)
	}
	fields.Forms = len(entry.Forms)
	fields.Phrases = len(entry.Phrases) + len(entry.Idioms) + len(entry.PhrasalVerbs) + len(entry.Derivatives)
	fields.CrossReferences = len(entry.CrossReferences) + len(entry.Related) +
		len(entry.Synonyms) + len(entry.Antonyms) + len(entry.WordFamily) + len(entry.Collocations)
	fields.Sections = len(entry.Sections) + len(entry.UsageNotes) + len(entry.GrammarNotes)
	for _, section := range entry.Sections {
		if section.Title == parser.FallbackSectionTitle {
			fields.Fallback++
		}
	}
	return fields
}

// semanticField is one piece of extracted text, kept with its kind so a
// dominant field can be named rather than merely measured.
type semanticField struct {
	kind string
	text string
}

// semanticFields flattens everything the parse claims to have understood.
func semanticFields(entry *entryir.Entry) []semanticField {
	var out []semanticField
	add := func(kind, text string) {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, semanticField{kind: kind, text: trimmed})
		}
	}
	if entry == nil {
		return out
	}
	add("headword", entry.Headword)
	for _, pronunciation := range entry.Pronunciations {
		add("ipa", pronunciation.IPA)
		add("label", pronunciation.Label)
	}
	for _, part := range entry.Parts {
		add("pos", part.POS)
		add("grammar", part.Grammar)
		var walk func([]entryir.Sense)
		walk = func(senses []entryir.Sense) {
			for _, sense := range senses {
				add("definition", sense.Definition)
				add("translation", sense.Translation)
				add("grammar", sense.Grammar)
				add("topic", sense.Topic)
				for _, label := range sense.Labels {
					add("label", label)
				}
				for _, pattern := range sense.Patterns {
					add("pattern", pattern)
				}
				for _, example := range sense.Examples {
					add("example", example.Text)
					add("exampleTranslation", example.Translation)
				}
				for _, value := range sense.Synonyms {
					add("synonym", value)
				}
				for _, value := range sense.Antonyms {
					add("antonym", value)
				}
				walk(sense.Subsenses)
			}
		}
		walk(part.Senses)
	}
	for _, form := range entry.Forms {
		add("form", form.Name)
		for _, word := range form.Words {
			add("form", word)
		}
	}
	for _, group := range []struct {
		kind    string
		entries []entryir.PhraseEntry
	}{
		{"phrase", entry.Phrases}, {"idiom", entry.Idioms},
		{"phrasalVerb", entry.PhrasalVerbs}, {"derivative", entry.Derivatives},
	} {
		for _, item := range group.entries {
			add(group.kind, item.Phrase)
			add(group.kind, item.Definition)
			for _, example := range item.Examples {
				add("example", example.Text)
				add("exampleTranslation", example.Translation)
			}
		}
	}
	for _, group := range []struct {
		kind   string
		values []string
	}{
		{"crossReference", entry.CrossReferences}, {"related", entry.Related},
		{"synonym", entry.Synonyms}, {"antonym", entry.Antonyms},
		{"wordFamily", entry.WordFamily}, {"collocation", entry.Collocations},
	} {
		for _, value := range group.values {
			add(group.kind, value)
		}
	}
	for _, group := range []struct {
		kind     string
		sections []entryir.Section
	}{
		{"usageNote", entry.UsageNotes}, {"grammarNote", entry.GrammarNotes},
	} {
		for _, section := range group.sections {
			add(group.kind, section.Title)
			add(group.kind, section.Body)
		}
	}
	add("etymology", entry.Etymology)
	for _, section := range entry.Sections {
		kind := "section"
		if section.Title == parser.FallbackSectionTitle {
			kind = "fallback"
		}
		add(kind, section.Body)
	}
	return out
}

// measure computes the consistency metrics for one record.
func measure(sourceText, recordText string, entry *entryir.Entry) Metrics {
	fields := semanticFields(entry)
	source := tokenize(sourceText)
	output := map[string]int{}
	outputTotal := 0
	for _, field := range fields {
		for token, count := range tokenize(field.text) {
			output[token] += count
			outputTotal += count
		}
	}
	sourceTotal := 0
	for _, count := range source {
		sourceTotal += count
	}

	covered, excess := 0, 0
	for token, count := range output {
		available := source[token]
		if count <= available {
			covered += count
			continue
		}
		covered += available
		excess += count - available
	}

	recordTotal := 0
	for _, count := range tokenize(recordText) {
		recordTotal += count
	}
	metrics := Metrics{SourceTokens: sourceTotal, OutputTokens: outputTotal, RecordTokens: recordTotal}
	if recordTotal > 0 {
		metrics.Scope = float64(sourceTotal) / float64(recordTotal)
	}
	if sourceTotal > 0 {
		metrics.Retention = float64(covered) / float64(sourceTotal)
	}
	if outputTotal > 0 {
		metrics.Duplication = float64(excess) / float64(outputTotal)
	}

	sourceRunes := len([]rune(sourceText))
	for _, field := range fields {
		// A headword or a part-of-speech label is short by definition and
		// cannot swallow anything; measuring dominance over them would only
		// add noise for the shortest records.
		switch field.kind {
		// A fallback section is the record by definition, so measuring how
		// much of the record it accounts for answers a question nobody asked.
		case "headword", "pos", "ipa", "label", "topic", "fallback":
			continue
		}
		share := 0.0
		if sourceRunes > 0 {
			share = float64(len([]rune(field.text))) / float64(sourceRunes)
		}
		if share > metrics.LargestFieldShare {
			metrics.LargestFieldShare = share
			metrics.LargestFieldKind = field.kind
		}
	}

	countEchoes(entry, &metrics)
	return metrics
}

// countEchoes finds the same content emitted twice under different fields.
func countEchoes(entry *entryir.Entry, metrics *Metrics) {
	if entry == nil {
		return
	}
	seenDefinitions := map[string]int{}
	seenExamples := map[string]int{}
	var senseTexts []string

	for _, part := range entry.Parts {
		var walk func(senses []entryir.Sense, parentText string)
		walk = func(senses []entryir.Sense, parentText string) {
			for _, sense := range senses {
				definition := normalizeForCompare(sense.Definition)
				if definition != "" {
					seenDefinitions[definition]++
					if seenDefinitions[definition] > 1 {
						metrics.RepeatedDefinitions++
					}
					senseTexts = append(senseTexts, definition)
				}
				for _, example := range sense.Examples {
					text := normalizeForCompare(example.Text)
					if text == "" {
						continue
					}
					seenExamples[text]++
					if seenExamples[text] > 1 {
						metrics.RepeatedExamples++
					}
				}
				// A subsense whose text is already inside its parent is the
				// same meaning printed twice, one level apart.
				if definition != "" && parentText != "" && strings.Contains(parentText, definition) {
					metrics.SubsenseEchoesParent++
				}
				walk(sense.Subsenses, definition)
			}
		}
		walk(part.Senses, "")
	}

	for _, section := range entry.Sections {
		body := normalizeForCompare(section.Body)
		if body == "" {
			continue
		}
		for _, text := range senseTexts {
			if len(text) >= 16 && strings.Contains(body, text) {
				metrics.SectionEchoesSense++
				break
			}
		}
	}
}

func normalizeForCompare(text string) string {
	return strings.ToLower(parser.Normalize(text))
}

// tokenize turns text into a comparable bag of tokens without knowing what
// language it is in.
//
// Latin, Cyrillic and Greek words tokenize on their own boundaries. CJK text
// has none, so it tokenizes into character bigrams instead: single characters
// recur too often inside one record to distinguish retained content from
// coincidence, and bigrams do not.
func tokenize(text string) map[string]int {
	tokens := map[string]int{}
	var word []rune
	var cjk []rune

	flushWord := func() {
		if len(word) > 0 {
			tokens[string(word)]++
			word = word[:0]
		}
	}
	flushCJK := func() {
		switch {
		case len(cjk) == 1:
			tokens[string(cjk)]++
		case len(cjk) > 1:
			for i := 0; i+1 < len(cjk); i++ {
				tokens[string(cjk[i:i+2])]++
			}
		}
		cjk = cjk[:0]
	}

	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			word = append(word, r)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return tokens
}

// ruleCounts aggregates which parser rule produced each structure.
func ruleCounts(entry *entryir.Entry, into map[string]int) {
	if entry == nil {
		return
	}
	record := func(rule string) {
		if rule = strings.TrimSpace(rule); rule != "" {
			into[rule]++
		}
	}
	for _, pronunciation := range entry.Pronunciations {
		record(pronunciation.Rule)
	}
	for _, part := range entry.Parts {
		record(part.Rule)
		var walk func([]entryir.Sense)
		walk = func(senses []entryir.Sense) {
			for _, sense := range senses {
				record(sense.Rule)
				walk(sense.Subsenses)
			}
		}
		walk(part.Senses)
	}
	for _, section := range entry.Sections {
		if section.Title == parser.FallbackSectionTitle {
			into["generic:fallback"]++
		}
	}
	if len(entry.Parts) == 0 && len(entry.Sections) == 0 {
		into["none"]++
	}
}

// sortedCounts renders a count map in a stable order: commonest first, then
// alphabetical, so two runs of the pipeline produce comparable output.
func sortedCounts(counts map[string]int) []NameCount {
	out := make([]NameCount, 0, len(counts))
	for name, count := range counts {
		out = append(out, NameCount{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// NameCount is one name and how often it occurred.
type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
