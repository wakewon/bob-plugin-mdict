package validate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
)

// Backend parity asks a different question from the metrics: not "is the parse
// a fair reading of the record?" but "does what the parser understood survive
// the rest of the pipeline?".
//
//	MDX record → parser Entry → service EntrySet → Bob toDict
//	                                            └→ Markdown
//
// The two presentation layers are checked against the IR, never against each
// other. Requiring them to agree textually would be wrong — they exist because
// they present the same facts differently — so what is checked is that neither
// invents anything and neither quietly drops a field.

// Check is one invariant and whether it held.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// checker accumulates results in a fixed order, so two runs of the pipeline
// produce byte-identical reports.
type checker struct{ checks []Check }

func (c *checker) assert(name string, ok bool, detail string, args ...any) {
	check := Check{Name: name, OK: ok}
	if !ok {
		check.Detail = fmt.Sprintf(detail, args...)
	}
	c.checks = append(c.checks, check)
}

// parityInput is everything one validated lookup produced, at every layer.
type parityInput struct {
	source   *mdict.LookupSet
	set      *entryir.EntrySet
	separate *bobadapter.Dict
	combined *bobadapter.Dict
	markdown string
	// irJSON is the serialized EntrySet. It is the complete vocabulary of the
	// IR, which is what the Markdown is allowed to contain and nothing more.
	irJSON string
}

// checkParity runs every invariant over one lookup.
func checkParity(in parityInput) []Check {
	var c checker
	c.checkRecords(in)
	c.checkBob(in)
	c.checkMarkdown(in)
	return c.checks
}

func (c *checker) checkRecords(in parityInput) {
	c.assert("entryset-key-is-the-matched-key",
		in.set.LookupKey == in.source.MatchedKey,
		"EntrySet key %q but MDX matched %q", in.set.LookupKey, in.source.MatchedKey)

	c.assert("record-count-within-source",
		len(in.set.Records) <= len(in.source.Records),
		"EntrySet has %d records for %d source records", len(in.set.Records), len(in.source.Records))

	consecutive := true
	distinct := map[string]struct{}{}
	collision := 0
	for index, record := range in.set.Records {
		if record.RecordOrdinal != index+1 {
			consecutive = false
		}
		if record.Entry == nil {
			continue
		}
		// Provenance is per key, not per lookup: a record reached directly and
		// a record reached through a redirect are both the first record of
		// their own key. The pair is what identifies a source record.
		raw := record.Entry.Source.MatchedKey + "\x00" +
			strconv.Itoa(record.Entry.Source.RawRecordOrdinal)
		if _, seen := distinct[raw]; seen {
			collision++
		}
		distinct[raw] = struct{}{}
	}
	c.assert("record-ordinals-consecutive", consecutive,
		"visible record ordinals are not 1..%d", len(in.set.Records))
	// Duplicate keys are common and the records behind them are usually
	// similar. Keeping them apart is the product behaviour being protected:
	// two records that became one, or one that became two, are both failures.
	c.assert("duplicate-records-distinguishable", collision == 0,
		"%d records share a raw source ordinal", collision)

	sameKey := true
	for _, record := range in.set.Records {
		if record.Entry == nil {
			continue
		}
		if strings.TrimSpace(record.Entry.Source.MatchedKey) == "" {
			sameKey = false
		}
	}
	c.assert("records-carry-their-provenance", sameKey,
		"a record reached the EntrySet without a matched key")
}

func (c *checker) checkBob(in parityInput) {
	if in.combined == nil || in.separate == nil {
		c.assert("bob-renders", false, "the Bob adapter produced nothing for a non-empty EntrySet")
		return
	}
	c.assert("bob-renders", true, "")

	key := in.set.LookupKey
	// Some dictionaries carry stray leading whitespace in their keys; the
	// adapter trims it for display, which is presentation, not invention.
	trimmed := strings.TrimSpace(key)
	c.assert("bob-word-derives-from-lookup-key",
		strings.HasPrefix(in.separate.Word, trimmed) && strings.HasPrefix(in.combined.Word, trimmed),
		"Bob word %q / %q does not derive from lookup key %q", in.separate.Word, in.combined.Word, key)

	// Combined mode is what carries every record, so field preservation is
	// measured there; separate mode is the product default and is measured for
	// the navigation that replaces the records it does not show.
	haystack := bobText(in.combined)
	var missing []string
	for _, field := range irFields(in.set) {
		switch field.kind {
		// Sense-level grammar, synonyms and antonyms have no Bob carrier.
		// That is a schema limit rather than an adapter defect, and it is
		// recorded here so the limit stays visible instead of being forgotten.
		case "senseGrammar", "senseSynonym", "senseAntonym":
			continue
		// Bob's word is the re-lookupable MDX key, never the headword the
		// parser found in the markup. That is deliberate product behaviour:
		// a card titled with a parser guess cannot be clicked back to.
		case "headword":
			continue
		}
		if !containsNormalized(haystack, field.text) {
			missing = append(missing, field.kind+": "+truncate(field.text, 40))
		}
	}
	c.assert("bob-preserves-semantic-fields", len(missing) == 0,
		"%d fields absent from the Bob result: %s", len(missing), strings.Join(head(missing, 5), " | "))

	c.assert("bob-preserves-sense-order",
		orderPreserved(bobDefinitionOrder(in.combined), irDefinitionOrder(in.set)),
		"definitions appear in the Bob result in a different order from the IR")

	if len(in.set.Records) > 1 {
		siblings := 0
		for _, group := range in.separate.RelatedWordParts {
			if group.Part == "Other entries" {
				siblings = len(group.Words)
			}
		}
		c.assert("bob-separate-mode-offers-the-other-records",
			siblings == len(in.set.Records)-1,
			"%d sibling records offered for %d records", siblings, len(in.set.Records))
	}
}

func (c *checker) checkMarkdown(in parityInput) {
	c.assert("markdown-renders", strings.TrimSpace(in.markdown) != "",
		"the Markdown renderer produced nothing for a non-empty EntrySet")

	// The renderer escapes Markdown syntax that occurs in dictionary text, so
	// the needle is escaped the same way rather than the haystack unescaped:
	// Merriam-Webster delimits its transcriptions with backslashes, and
	// stripping those would compare two different strings.
	var missing []string
	for _, field := range irFields(in.set) {
		if !containsNormalized(in.markdown, mdrender.Escape(field.text)) {
			missing = append(missing, field.kind+": "+truncate(field.text, 40))
		}
	}
	c.assert("markdown-preserves-semantic-fields", len(missing) == 0,
		"%d fields absent from the Markdown: %s", len(missing), strings.Join(head(missing, 5), " | "))

	// The renderer reads the IR, never the record. If it ever reparsed HTML,
	// text the parser deliberately discarded would reappear here — so any
	// content token in the Markdown that the IR does not contain is a defect
	// in exactly the way that matters.
	known := tokenize(in.irJSON)
	var invented []string
	for token := range tokenize(stripMarkdownChrome(in.markdown)) {
		if _, ok := known[token]; ok {
			continue
		}
		if _, ok := rendererVocabulary[token]; ok {
			continue
		}
		if isNumeric(token) {
			continue
		}
		invented = append(invented, token)
	}
	c.assert("markdown-is-derived-from-the-ir", len(invented) == 0,
		"%d tokens in the Markdown are absent from the IR: %s", len(invented), strings.Join(head(sortStrings(invented), 8), ", "))

	if len(in.set.Records) > 1 {
		headings := strings.Count(in.markdown, "\n## Record ")
		c.assert("markdown-keeps-record-boundaries", headings == len(in.set.Records),
			"%d record headings for %d records", headings, len(in.set.Records))
	}
}

// rendererVocabulary is the fixed English the Markdown renderer contributes.
// Anything outside it has to have come from the IR.
var rendererVocabulary = map[string]struct{}{}

func init() {
	for _, word := range strings.Fields(`record of headword audio uk us shared unmarked syn ant
		forms phrases idioms phrasal verbs derivatives collocations synonyms antonyms
		see also related word family usage grammar origin unlabelled note`) {
		rendererVocabulary[word] = struct{}{}
	}
}

// irField is one piece of IR text that a presentation layer must not lose.
func irFields(set *entryir.EntrySet) []semanticField {
	var out []semanticField
	for _, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		for _, field := range semanticFields(record.Entry) {
			// Very short strings match by accident everywhere and prove
			// nothing about preservation.
			if len([]rune(field.text)) < 8 {
				continue
			}
			switch field.kind {
			case "grammar":
				field.kind = "senseGrammar"
			case "synonym":
				field.kind = "senseSynonym"
			case "antonym":
				field.kind = "senseAntonym"
			}
			out = append(out, field)
		}
	}
	return out
}

func irDefinitionOrder(set *entryir.EntrySet) []string {
	var out []string
	for _, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		for _, part := range record.Entry.Parts {
			var walk func([]entryir.Sense)
			walk = func(senses []entryir.Sense) {
				for _, sense := range senses {
					if text := normalizeForCompare(sense.Definition); len(text) >= 12 {
						out = append(out, text)
					}
					walk(sense.Subsenses)
				}
			}
			walk(part.Senses)
		}
	}
	return out
}

func bobDefinitionOrder(dict *bobadapter.Dict) []string {
	var out []string
	for _, part := range dict.Parts {
		for _, mean := range part.Means {
			out = append(out, normalizeForCompare(mean))
		}
	}
	return out
}

// orderPreserved reports whether every wanted item appears in the haystack in
// order, allowing extra material between them.
func orderPreserved(haystack, wanted []string) bool {
	cursor := 0
	for _, want := range wanted {
		found := false
		for ; cursor < len(haystack); cursor++ {
			if strings.Contains(haystack[cursor], want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// bobText concatenates every textual field of a Bob result.
//
// Building the haystack from the struct rather than from its JSON keeps the
// comparison exact: Go's encoder escapes angle brackets and ampersands, which
// occur in real definitions.
func bobText(dict *bobadapter.Dict) string {
	if dict == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString(dict.Word)
	for _, phonetic := range dict.Phonetics {
		out.WriteString("\n" + phonetic.Type + " " + phonetic.Value)
		if phonetic.TTS != nil {
			out.WriteString(" " + phonetic.TTS.Value)
		}
	}
	for _, part := range dict.Parts {
		out.WriteString("\n" + part.Part + "\n" + strings.Join(part.Means, "\n"))
	}
	for _, exchange := range dict.Exchanges {
		out.WriteString("\n" + exchange.Name + " " + strings.Join(exchange.Words, ", "))
	}
	for _, group := range dict.RelatedWordParts {
		out.WriteString("\n" + group.Part)
		for _, word := range group.Words {
			out.WriteString("\n" + word.Word + " " + strings.Join(word.Means, " "))
		}
	}
	for _, addition := range dict.Additions {
		out.WriteString("\n" + addition.Name + "\n" + addition.Value)
	}
	return out.String()
}

func sortStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func containsNormalized(haystack, needle string) bool {
	return strings.Contains(normalizeForCompare(haystack), normalizeForCompare(needle))
}

// stripMarkdownChrome removes the syntax the renderer adds, so only content
// tokens are compared against the IR.
func stripMarkdownChrome(markdown string) string {
	replacer := strings.NewReplacer(
		"#", " ", "*", " ", "`", " ", "-", " ", "[", " ", "]", " ",
		"(", " ", ")", " ", "/", " ", "\\", " ", "—", " ", "·", " ", "🔊", " ",
		"‹", " ", "›", " ", ":", " ",
	)
	return replacer.Replace(markdown)
}

func isNumeric(token string) bool {
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return token != ""
}

func head(values []string, limit int) []string {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
