package parser

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// parseParts fills Entry.Parts, grouping senses under their part of speech.
func (s *parseState) parseParts() {
	if s.profile != nil && !s.profile.sense.IsEmpty() {
		s.parsePartsFromProfile()
	}
	if len(s.entry.Parts) == 0 && !s.partsHandled {
		s.parsePartsGeneric()
	}
	// Prune parts that ended up with nothing readable in them.
	kept := s.entry.Parts[:0]
	for _, part := range s.entry.Parts {
		if len(part.Senses) == 0 {
			continue
		}
		kept = append(kept, part)
	}
	s.entry.Parts = kept
	s.note("parts: %d senses: %d", len(s.entry.Parts), s.entry.SenseCount())
}

func (s *parseState) parsePartsFromProfile() {
	if s.profile.partBlock.IsEmpty() && !s.profile.pos.IsEmpty() {
		// Without a part-of-speech container the label lives inside each sense.
		// Grouping by the whole document would file every sense under whichever
		// label happened to come first.
		if s.parseSensesGroupedByOwnPOS() {
			return
		}
	}

	blocks := []*html.Node{s.doc}
	if !s.profile.partBlock.IsEmpty() {
		if found := QueryAll(s.doc, s.profile.partBlock); len(found) > 0 {
			blocks = found
		}
	}

	for _, block := range blocks {
		if role := ClassifySemanticLabel(s.rawPOSLabel(block)); role != "" {
			s.partsHandled = true
			s.storeSemanticBlock(block, role)
			continue
		}
		pos := s.posOf(block)
		grammar := s.firstText(block, s.profile.grammar)

		senseNodes := QueryAll(block, s.profile.sense)
		if len(senseNodes) == 0 {
			// A part with a single meaning often carries no sense wrapper at
			// all — there is nothing to number. Treat the block itself as one
			// sense so it is not dropped.
			if s.profile.definition.IsEmpty() || Query(block, s.profile.definition) == nil {
				continue
			}
			senseNodes = []*html.Node{block}
		}
		var senses []entryir.Sense
		seen := make(map[string]struct{})
		for _, node := range senseNodes {
			sense := s.senseFromNode(node)
			if sense.Definition == "" && sense.Translation == "" && len(sense.Examples) == 0 && len(sense.Subsenses) == 0 {
				continue
			}
			// Guard against a record that repeats the same sense, which happens
			// when one entry carries several regional editions.
			key := strings.ToLower(sense.Number + "|" + sense.Definition + "|" + sense.Translation)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			senses = append(senses, sense)
		}
		if len(senses) == 0 {
			continue
		}
		s.appendPart(entryir.Part{
			POS:        pos,
			Grammar:    grammar,
			Senses:     senses,
			Confidence: confidenceForPOS(pos),
			Rule:       "profile:" + s.profile.sense.String(),
		})
	}
}

// parseSensesGroupedByOwnPOS handles dictionaries that label each sense with
// its own part of speech instead of grouping senses into blocks. It reports
// whether it produced anything.
func (s *parseState) parseSensesGroupedByOwnPOS() bool {
	senseNodes := QueryAll(s.doc, s.profile.sense)
	if len(senseNodes) == 0 {
		return false
	}
	type bucket struct {
		pos    string
		senses []entryir.Sense
	}
	var buckets []bucket
	seen := make(map[string]struct{})
	previous := ""
	handled := false

	for _, node := range senseNodes {
		rawLabel := s.rawPOSLabel(node)
		if role := ClassifySemanticLabel(rawLabel); role != "" {
			handled = true
			s.partsHandled = true
			s.storeSemanticSense(node, role)
			continue
		}
		pos := s.posOf(node)
		if pos == "" {
			// A sense with no label of its own continues the previous one.
			pos = previous
		}
		previous = pos

		sense := s.senseFromNode(node)
		if sense.Definition == "" && sense.Translation == "" && len(sense.Examples) == 0 && len(sense.Subsenses) == 0 {
			continue
		}
		key := strings.ToLower(pos + "|" + sense.Number + "|" + sense.Definition + "|" + sense.Translation)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		index := -1
		for i := range buckets {
			if buckets[i].pos == pos {
				index = i
				break
			}
		}
		if index < 0 {
			buckets = append(buckets, bucket{pos: pos})
			index = len(buckets) - 1
		}
		buckets[index].senses = append(buckets[index].senses, sense)
	}

	for _, item := range buckets {
		if len(item.senses) == 0 {
			continue
		}
		s.appendPart(entryir.Part{
			POS:        item.pos,
			Senses:     item.senses,
			Confidence: confidenceForPOS(item.pos),
			Rule:       "profile:senseOwnPOS",
		})
	}
	return handled || len(s.entry.Parts) > 0
}

// posOf resolves the part of speech for a block, preferring the profile
// selector and falling back to scanning for a recognisable POS word.
func (s *parseState) posOf(block *html.Node) string {
	raw := s.rawPOSLabel(block)
	if raw != "" {
		if pos := CanonicalPOS(raw); pos != "" {
			return pos
		}
		// Keep the raw label when it is unrecognised but short; a dictionary
		// may use a category this project has never seen.
		if len([]rune(raw)) <= 30 {
			return raw
		}
	}
	return ""
}

func (s *parseState) rawPOSLabel(block *html.Node) string {
	if s.profile == nil || s.profile.pos.IsEmpty() {
		return ""
	}
	var fallback string
	for _, node := range QueryAllNested(block, s.profile.pos) {
		raw := strings.Trim(Normalize(Text(node, TextOptions{SkipHidden: true})), " :：")
		if raw == "" {
			continue
		}
		if fallback == "" {
			fallback = raw
		}
		if ClassifySemanticLabel(raw) != "" || CanonicalPOS(raw) != "" {
			return raw
		}
	}
	return fallback
}

func (s *parseState) storeSemanticBlock(block *html.Node, role SemanticLabel) {
	senseNodes := QueryAll(block, s.profile.sense)
	if len(senseNodes) == 0 {
		senseNodes = []*html.Node{block}
	}
	for _, node := range senseNodes {
		s.storeSemanticSense(node, role)
	}
}

func (s *parseState) storeSemanticSense(node *html.Node, role SemanticLabel) {
	sense := s.senseFromNode(node)
	body := strings.TrimSpace(firstNonEmpty(sense.Definition, sense.Translation))
	if body == "" {
		body = Normalize(s.textOf(node))
	}
	// Remove the visual section label and source number from fallback text.
	body = strings.TrimSpace(strings.TrimPrefix(body, s.rawPOSLabel(node)))
	body = strings.TrimSpace(strings.TrimPrefix(body, sense.Number))
	lemma := ""
	for _, selector := range []string{".phrase", ".REFHWD", ".PHRVBHWD", "strong", "b", "a"} {
		if candidate := Query(node, ParseSelector(selector)); candidate != nil {
			lemma = cleanLemma(s.textOf(candidate))
			if lemma != "" {
				break
			}
		}
	}
	if lemma != "" && strings.HasPrefix(body, lemma) {
		body = strings.TrimSpace(strings.TrimPrefix(body, lemma))
	}
	body = strings.Trim(body, " :：-—·")

	switch role {
	case LabelCrossReference:
		s.entry.CrossReferences = append(s.entry.CrossReferences, splitList(stripLeadingMarker(firstNonEmpty(body, lemma)))...)
	case LabelRelated:
		s.entry.Related = append(s.entry.Related, splitList(stripLeadingMarker(firstNonEmpty(body, lemma)))...)
	case LabelPhrase:
		s.entry.Phrases = appendPhrase(s.entry.Phrases, lemma, body)
	case LabelIdiom:
		s.entry.Idioms = appendPhrase(s.entry.Idioms, lemma, body)
	case LabelPhrasalVerb:
		s.entry.PhrasalVerbs = appendPhrase(s.entry.PhrasalVerbs, lemma, body)
	case LabelDerivative:
		s.entry.Derivatives = appendPhrase(s.entry.Derivatives, lemma, body)
	case LabelSynonyms:
		s.entry.Synonyms = append(s.entry.Synonyms, splitList(stripLeadingMarker(firstNonEmpty(body, lemma)))...)
	case LabelAntonyms:
		s.entry.Antonyms = append(s.entry.Antonyms, splitList(stripLeadingMarker(firstNonEmpty(body, lemma)))...)
	}
}

func confidenceForPOS(pos string) float64 {
	if pos == "" {
		return 0.6
	}
	return 0.95
}

func (s *parseState) appendPart(part entryir.Part) {
	// Merge into an existing part with the same label so a dictionary that
	// splits one verb across several blocks still reads as one section.
	for i := range s.entry.Parts {
		if s.entry.Parts[i].POS == part.POS {
			s.entry.Parts[i].Senses = append(s.entry.Parts[i].Senses, part.Senses...)
			return
		}
	}
	s.entry.Parts = append(s.entry.Parts, part)
}

// senseFromNode extracts one sense, including its subsenses.
func (s *parseState) senseFromNode(node *html.Node) entryir.Sense {
	sense := entryir.Sense{Confidence: 0.9, Rule: "profile:sense"}

	// Subsenses are pulled out and detached first so their text does not leak
	// into the parent sense's definition.
	if !s.profile.subsense.IsEmpty() {
		for _, subNode := range QueryAll(node, s.profile.subsense) {
			sub := s.senseFromNodeShallow(subNode)
			if sub.Definition != "" || sub.Translation != "" || len(sub.Examples) > 0 {
				sense.Subsenses = append(sense.Subsenses, sub)
			}
			if subNode.Parent != nil {
				subNode.Parent.RemoveChild(subNode)
			}
		}
	}
	s.fillSense(&sense, node)
	return sense
}

func (s *parseState) senseFromNodeShallow(node *html.Node) entryir.Sense {
	sense := entryir.Sense{Confidence: 0.85, Rule: "profile:subsense"}
	s.fillSense(&sense, node)
	return sense
}

func (s *parseState) fillSense(sense *entryir.Sense, node *html.Node) {
	sense.Number = ParseSenseNumber(s.firstText(node, s.profile.senseNumber))
	sense.Topic = s.firstText(node, s.profile.topic)

	// Examples are extracted then detached, so the definition text that follows
	// is the definition alone.
	sense.Examples = s.examplesIn(node, true)

	// Cross-reference blocks are lifted out and detached first. They carry
	// register labels of their own ("ditch [slang]") which would otherwise be
	// read as labels of the sense itself.
	for _, synNode := range QueryAll(node, s.profile.synonyms) {
		sense.Synonyms = append(sense.Synonyms, splitList(stripLeadingMarker(s.textOf(synNode)))...)
		if synNode.Parent != nil {
			synNode.Parent.RemoveChild(synNode)
		}
	}
	for _, antNode := range QueryAll(node, s.profile.antonyms) {
		sense.Antonyms = append(sense.Antonyms, splitList(stripLeadingMarker(s.textOf(antNode)))...)
		if antNode.Parent != nil {
			antNode.Parent.RemoveChild(antNode)
		}
	}

	for _, labelNode := range QueryAll(node, s.profile.labels) {
		for _, label := range splitList(Text(labelNode, TextOptions{SkipHidden: true})) {
			sense.Labels = append(sense.Labels, strings.Trim(label, "[]()"))
		}
	}
	for _, patternNode := range QueryAll(node, s.profile.patterns) {
		if text := Normalize(s.textOf(patternNode)); text != "" {
			sense.Patterns = append(sense.Patterns, text)
		}
	}
	sense.Grammar = s.firstText(node, s.profile.grammar)

	// A sense may carry several definition nodes: the source language in one
	// and its translation in another.
	var definitions, translations []string
	for _, defNode := range QueryAll(node, s.profile.definition) {
		RemoveMatching(defNode, s.profile.definitionStrip)
		main, translated := s.splitTranslation(defNode)
		if main != "" {
			definitions = append(definitions, main)
		}
		if translated != "" {
			translations = append(translations, translated)
		}
	}
	if len(definitions) == 0 && s.profile.definition.IsEmpty() {
		main, translated := s.splitTranslation(node)
		if main != "" {
			definitions = append(definitions, main)
		}
		if translated != "" {
			translations = append(translations, translated)
		}
	}

	dedupeStrings(&definitions)
	dedupeStrings(&translations)
	sense.Definition = Normalize(strings.Join(definitions, " "))
	sense.Translation = Normalize(strings.Join(translations, " "))
	sense.Definition = stripLeadingNumber(sense.Definition, sense.Number)

	dedupeStrings(&sense.Labels)
	dedupeStrings(&sense.Patterns)
	dedupeStrings(&sense.Synonyms)
	dedupeStrings(&sense.Antonyms)
}

// examplesIn collects examples under a node, optionally detaching them.
func (s *parseState) examplesIn(node *html.Node, detach bool) []entryir.Example {
	if s.profile == nil || s.profile.example.IsEmpty() {
		return nil
	}
	var out []entryir.Example
	nodes := QueryAll(node, s.profile.example)
	for _, exNode := range nodes {
		example := s.exampleFromNode(exNode)
		if example.Text != "" || example.Translation != "" {
			if len(out) < s.opts.MaxExamplesPerSense {
				out = append(out, example)
			}
		}
		if detach && exNode.Parent != nil {
			exNode.Parent.RemoveChild(exNode)
		}
	}
	return out
}

func (s *parseState) exampleFromNode(node *html.Node) entryir.Example {
	var example entryir.Example
	target := node
	if !s.profile.exampleText.IsEmpty() {
		if inner := Query(node, s.profile.exampleText); inner != nil {
			target = inner
		}
	}
	example.Text, example.Translation = s.splitTranslation(target)
	if target != node && example.Translation == "" {
		// The translation may sit beside the English text rather than inside it.
		_, translated := s.splitTranslation(node)
		example.Translation = translated
	}
	example.Audio = s.resolveAudioFrom(node, audioAttrs)
	return example
}

func (s *parseState) firstText(node *html.Node, sel Selector) string {
	if sel.IsEmpty() {
		return ""
	}
	for _, match := range QueryAll(node, sel) {
		if text := Normalize(s.textOf(match)); text != "" {
			return text
		}
	}
	return ""
}

// markerPrefixes are the labels dictionaries prepend to a cross-reference list.
// Longer forms come first so "see also" is not left as a dangling "also".
var markerPrefixes = []string{
	"SYNONYMS", "ANTONYMS", "SYNONYM", "ANTONYM", "see also", "compare",
	"SYN", "OPP", "ANT", "cf.", "cf", "→", "⇒", "see", "参见", "参照", "同", "反",
}

// stripLeadingMarker removes those labels, matching without regard to case
// because dictionaries capitalise them however their typography demanded.
func stripLeadingMarker(text string) string {
	trimmed := Normalize(text)
	for _, marker := range markerPrefixes {
		if len(trimmed) >= len(marker) && strings.EqualFold(trimmed[:len(marker)], marker) {
			trimmed = strings.TrimSpace(trimmed[len(marker):])
		}
	}
	return strings.Trim(trimmed, " :：,，")
}

// stripLeadingNumber removes a sense number that the definition text repeats.
func stripLeadingNumber(text, number string) string {
	if number == "" || text == "" {
		return text
	}
	for _, prefix := range []string{number + ". ", number + ".", number + " ", number} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
	}
	return text
}
