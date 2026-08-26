package parser

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// parseSections lifts idioms, phrases, usage notes, etymology and similar
// blocks out of the entry and detaches them, so the sense parser sees only the
// entry's own senses.
func (s *parseState) parseSections() {
	if s.profile == nil {
		return
	}
	for _, rule := range s.profile.sections {
		for _, node := range QueryAll(s.doc, rule.selector) {
			if rule.items.IsEmpty() {
				s.applySectionRule(node, rule)
			} else {
				s.applySectionItems(node, rule)
			}
			if node.Parent != nil {
				node.Parent.RemoveChild(node)
			}
		}
	}
}

// applySectionItems handles a section that lists several entries. Items whose
// lemma is empty are continuations of the previous one, which is how these
// lists are actually laid out: a heading item followed by its sense items.
func (s *parseState) applySectionItems(section *html.Node, rule compiledSection) {
	RemoveMatching(section, rule.stripTitle)

	type pending struct {
		lemma string
		body  []string
	}
	var items []pending

	for _, node := range QueryAll(section, rule.items) {
		lemma := cleanLemma(s.firstText(node, rule.lemma))
		body := ""
		if !rule.body.IsEmpty() {
			body = s.firstText(node, rule.body)
		} else {
			body = Normalize(s.textOf(node))
		}
		body = cleanLemma(body)
		if lemma != "" && strings.HasPrefix(body, lemma) {
			body = strings.TrimSpace(body[len(lemma):])
		}
		if lemma == "" && len(items) > 0 {
			if body != "" {
				items[len(items)-1].body = append(items[len(items)-1].body, body)
			}
			continue
		}
		if lemma == "" && body == "" {
			continue
		}
		items = append(items, pending{lemma: lemma, body: []string{}})
		if body != "" {
			items[len(items)-1].body = append(items[len(items)-1].body, body)
		}
	}

	for _, item := range items {
		s.storeSection(rule, item.lemma, strings.Trim(strings.Join(item.body, " "), " :：-—·"), nil)
	}
}

func (s *parseState) applySectionRule(node *html.Node, rule compiledSection) {
	RemoveMatching(node, rule.stripTitle)
	richRoot := node

	lemma := cleanLemma(s.firstText(node, rule.lemma))
	body := ""
	if !rule.body.IsEmpty() {
		var parts []string
		bodyNodes := QueryAll(node, rule.body)
		if len(bodyNodes) == 1 {
			richRoot = bodyNodes[0]
		}
		for _, bodyNode := range bodyNodes {
			if text := Normalize(s.textOf(bodyNode)); text != "" {
				parts = append(parts, text)
			}
		}
		body = strings.Join(parts, " ")
	} else {
		body = Normalize(s.textOf(node))
	}
	body = cleanLemma(body)
	// The lemma is usually the opening words of the block; do not print it twice.
	if lemma != "" && strings.HasPrefix(body, lemma) {
		body = strings.TrimSpace(body[len(lemma):])
	}
	body = strings.Trim(body, " :：-—·")

	s.storeSection(rule, lemma, body, s.richBlocks(richRoot))
}

// storeSection files an extracted section into the right IR field.
func (s *parseState) storeSection(rule compiledSection, lemma, body string, blocks []entryir.RichBlock) {
	if lemma == "" && body == "" {
		return
	}
	title := rule.title
	if title == "" {
		title = defaultSectionTitle(rule.kind)
	}

	switch rule.kind {
	case SectionIdiom:
		s.entry.Idioms = appendPhrase(s.entry.Idioms, lemma, body)
	case SectionPhrase:
		s.entry.Phrases = appendPhrase(s.entry.Phrases, lemma, body)
	case SectionPhrasalVerb:
		s.entry.PhrasalVerbs = appendPhrase(s.entry.PhrasalVerbs, lemma, body)
	case SectionCollocation:
		if text := firstNonEmpty(lemma, body); text != "" {
			s.entry.Collocations = append(s.entry.Collocations, text)
		}
	case SectionSynonyms:
		s.entry.Synonyms = append(s.entry.Synonyms, splitList(stripLeadingMarker(firstNonEmpty(body, lemma)))...)
	case SectionEtymology:
		if body != "" {
			if s.entry.Etymology != "" {
				s.entry.Etymology += " "
			}
			s.entry.Etymology += body
		}
	case SectionUsage:
		if body != "" {
			s.entry.UsageNotes = append(s.entry.UsageNotes, entryir.Section{Title: firstNonEmpty(lemma, title), Body: body, Blocks: blocks})
		}
	case SectionGrammar:
		if body != "" {
			s.entry.GrammarNotes = append(s.entry.GrammarNotes, entryir.Section{Title: firstNonEmpty(lemma, title), Body: body, Blocks: blocks})
		}
	default:
		// Anything the profile could not classify is preserved verbatim rather
		// than being guessed into a typed field.
		if body != "" {
			s.entry.Sections = append(s.entry.Sections, entryir.Section{Title: firstNonEmpty(lemma, title), Body: body, Blocks: blocks})
		}
	}
}

// cleanLemma strips the arrows and separators dictionaries prefix onto a
// cross-referenced phrase.
func cleanLemma(text string) string {
	return strings.Trim(Normalize(text), " →⇒>·:：,，.")
}

func appendPhrase(list []entryir.PhraseEntry, lemma, body string) []entryir.PhraseEntry {
	if lemma == "" && body == "" {
		return list
	}
	if lemma == "" {
		// Without a lemma the first clause is the best available label.
		lemma, body = splitFirstClause(body)
	}
	return append(list, entryir.PhraseEntry{Phrase: lemma, Definition: body})
}

// splitFirstClause separates a leading phrase from its explanation.
func splitFirstClause(text string) (string, string) {
	if idx := strings.IndexAny(text, ".:"); idx > 0 && idx < 60 {
		return strings.TrimSpace(text[:idx]), strings.TrimSpace(text[idx+1:])
	}
	return text, ""
}

func defaultSectionTitle(kind SectionKind) string {
	switch kind {
	case SectionIdiom:
		return "Idioms"
	case SectionPhrase:
		return "Phrases"
	case SectionPhrasalVerb:
		return "Phrasal Verbs"
	case SectionUsage:
		return "Usage"
	case SectionGrammar:
		return "Grammar"
	case SectionEtymology:
		return "Origin"
	case SectionSynonyms:
		return "Synonyms"
	case SectionCollocation:
		return "Collocations"
	default:
		return "Notes"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// parseForms extracts inflected forms.
func (s *parseState) parseForms() {
	if s.profile == nil {
		return
	}
	for _, rule := range s.profile.forms {
		for _, container := range QueryAll(s.doc, rule.container) {
			s.applyFormRule(container, rule)
		}
	}
	// Collapse duplicates that arise when a dictionary lists the same form
	// under two labels.
	for i := range s.entry.Forms {
		dedupeStrings(&s.entry.Forms[i].Words)
	}
}

func (s *parseState) applyFormRule(container *html.Node, rule compiledForm) {
	if rule.label.IsEmpty() {
		// Unlabelled list: every word goes under the rule's fixed name.
		name := rule.name
		if name == "" {
			name = "Forms"
		}
		var words []string
		targets := QueryAll(container, rule.word)
		if len(targets) == 0 {
			targets = []*html.Node{container}
		}
		for _, node := range targets {
			if text := Normalize(s.textOf(node)); text != "" {
				words = append(words, text)
			}
		}
		s.addForm(name, words, nil)
		return
	}

	// Labelled list: labels and words alternate as siblings, so each label
	// claims the words that follow it until the next label.
	type slot struct {
		isLabel bool
		text    string
		node    *html.Node
	}
	var slots []slot
	Walk(container, func(node *html.Node) bool {
		if node == container {
			return true
		}
		switch {
		case rule.label.Matches(node):
			slots = append(slots, slot{isLabel: true, text: Normalize(s.textOf(node)), node: node})
			return false
		case !rule.word.IsEmpty() && rule.word.Matches(node):
			slots = append(slots, slot{text: Normalize(s.textOf(node)), node: node})
			return false
		}
		return true
	})

	current := rule.name
	for _, item := range slots {
		if item.isLabel {
			if canonical := CanonicalForm(item.text); canonical != "" {
				current = canonical
			} else if item.text != "" {
				current = item.text
			}
			continue
		}
		if item.text == "" {
			continue
		}
		name := current
		if name == "" {
			name = "Forms"
		}
		s.addForm(name, []string{item.text}, s.resolveAudioFrom(item.node, audioAttrs))
	}
}

func (s *parseState) addForm(name string, words []string, audio *entryir.Audio) {
	dedupeStrings(&words)
	if len(words) == 0 {
		return
	}
	for i := range s.entry.Forms {
		if strings.EqualFold(s.entry.Forms[i].Name, name) {
			s.entry.Forms[i].Words = append(s.entry.Forms[i].Words, words...)
			if s.entry.Forms[i].Audio == nil {
				s.entry.Forms[i].Audio = audio
			}
			return
		}
	}
	s.entry.Forms = append(s.entry.Forms, entryir.Form{Name: name, Words: words, Audio: audio})
}

// parseWordFamily collects related derived words.
func (s *parseState) parseWordFamily() {
	if s.profile == nil || s.profile.wordFamily.IsEmpty() {
		return
	}
	for _, node := range QueryAll(s.doc, s.profile.wordFamily) {
		if text := Normalize(s.textOf(node)); text != "" {
			s.entry.WordFamily = append(s.entry.WordFamily, text)
		}
	}
}
