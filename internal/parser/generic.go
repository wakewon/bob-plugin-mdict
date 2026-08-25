package parser

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// senseClassHints are class-name fragments that dictionaries overwhelmingly use
// for a block holding one meaning. They are matched as substrings because every
// publisher spells them differently ("Sense", "dsense", "n-g", "sense-block").
var senseClassHints = []string{"sense", "meaning", "def-g", "n-g", "trg", "dsense", "defblock", "def-block"}

// definitionClassHints mark the definition text inside a sense. "expla" covers
// both "explain" and "explanation", which are different words with the same
// job and no common substring longer than five characters.
var definitionClassHints = []string{"def", "ind", "means", "expla", "d-g"}

// exampleClassHints mark example sentences.
var exampleClassHints = []string{"example", "exa", "eg", "x-g", "sent", "cit", "quote"}

// labelClassHints mark register and domain labels.
var labelClassHints = []string{"label", "register", "lbl", "gram-label"}

// headwordClassHints mark the element holding the entry's own headword. They
// are matched as whole class tokens, not fragments: "hw" is two characters and
// would otherwise match half the class names in existence.
var headwordClassHints = []string{
	"hw", "headword", "orth", "entry_title", "entry_name", "entryhead",
	"entry-title", "entry-name", "keyword", "hword", "hwd",
}

// crossRefClassHints mark a block of pointers to other entries. They are
// matched as whole class tokens: nearly every word in some dictionaries is an
// entry:// link, so the link alone proves nothing — what marks a genuine
// cross-reference is the dictionary saying so.
var crossRefClassHints = []string{
	"xref", "xr", "crossref", "crossreference", "cross-ref", "cross-reference",
	"seealso", "see-also", "see_also",
}

// maxGenericCrossReferences caps a list that would otherwise grow to the size
// of the entry in a dictionary that links every word it prints.
const maxGenericCrossReferences = 20

// maxCrossReferenceBlocks is what separates a "see also" section from a link
// style. One Japanese-Chinese dictionary classes all 288 of its inline word
// links `crosslink`; a real cross-reference block occurs once or twice, and
// detaching hundreds of them would strip the entry of its own text.
const maxCrossReferenceBlocks = 6

// posClassHints mark part-of-speech labels.
var posClassHints = []string{"pos", "ps", "gramgrp", "type-gram", "wordclass", "st"}

// classTokenMatches reports whether one of the node's whole class tokens is
// exactly one of the supplied names.
func classTokenMatches(node *html.Node, names []string) bool {
	classes := ClassSet(node)
	for _, name := range names {
		for class := range classes {
			if strings.EqualFold(class, name) {
				return true
			}
		}
	}
	return false
}

// classMatchesHint reports whether any of the node's classes, or its id,
// contains one of the supplied fragments.
func classMatchesHint(node *html.Node, hints []string) bool {
	descriptor := strings.ToLower(Attr(node, "class") + " " + Attr(node, "id"))
	if descriptor == " " {
		return false
	}
	for _, hint := range hints {
		if strings.Contains(descriptor, hint) {
			return true
		}
	}
	return false
}

// parsePartsGeneric extracts senses from a dictionary with no profile, or when
// the profile produced nothing.
//
// It never falls back to flattening the record into one blob of text: if no
// structure can be recovered, the content is reported as an untyped section so
// the reader still sees it while nothing is mislabelled as a definition.
func (s *parseState) parsePartsGeneric() {
	if s.parseGenericSenseBlocks() {
		return
	}
	// Class names failed. Visible numbering is the other evidence dictionaries
	// agree on, and unlike class vocabulary it survives obfuscated markup and
	// records with no class attribute at all.
	if s.parseGenericEnumeratedSenses() {
		return
	}
	// Last: a repeated boundary element, for dictionaries that neither wrap a
	// sense nor number it.
	if s.parseGenericGroupedSenses() {
		return
	}
	s.genericFallbackSection()
}

// parseGenericSenseBlocks recovers senses from class-name evidence, reporting
// whether it produced any.
func (s *parseState) parseGenericSenseBlocks() bool {
	senseNodes := s.genericSenseNodes()
	if len(senseNodes) == 0 {
		return false
	}

	// Group senses under the nearest preceding part-of-speech label.
	type group struct {
		pos    string
		senses []entryir.Sense
	}
	var groups []group
	current := -1

	for _, node := range senseNodes {
		pos := s.genericPOSFor(node)
		if current < 0 || (pos != "" && pos != groups[current].pos) {
			groups = append(groups, group{pos: pos})
			current = len(groups) - 1
		}
		sense := s.genericSense(node)
		if sense.Definition == "" && len(sense.Examples) == 0 {
			continue
		}
		groups[current].senses = append(groups[current].senses, sense)
	}

	for _, item := range groups {
		if len(item.senses) == 0 {
			continue
		}
		s.appendPart(entryir.Part{
			POS:        item.pos,
			Senses:     item.senses,
			Confidence: confidenceForPOS(item.pos) - 0.15,
			Rule:       "generic:senseHints",
		})
	}
	return len(s.entry.Parts) > 0
}

// genericSenseNodes finds the outermost blocks that look like senses.
func (s *parseState) genericSenseNodes() []*html.Node {
	var found []*html.Node
	Walk(s.doc, func(node *html.Node) bool {
		if node == s.doc {
			return true
		}
		if !classMatchesHint(node, senseClassHints) {
			return true
		}
		// Ignore wrappers that merely contain the real sense blocks.
		if len(QueryAll(node, ParseSelector("*"))) > 0 && containsSenseChild(node) {
			return true
		}
		text := Normalize(Text(node, TextOptions{SkipHidden: true}))
		if len([]rune(text)) < 3 {
			return true
		}
		found = append(found, node)
		return false
	})
	return found
}

func containsSenseChild(node *html.Node) bool {
	nested := false
	Walk(node, func(child *html.Node) bool {
		if child == node || nested {
			return !nested
		}
		if classMatchesHint(child, senseClassHints) {
			nested = true
			return false
		}
		return true
	})
	return nested
}

// looksLikePOSLabel reports whether a node is a plausible part-of-speech
// label, from its class name or from being a leaf that holds nothing else.
//
// The class-name half only works for dictionaries that name their classes
// meaningfully. The leaf half is what covers the rest: an element whose entire
// content is "noun", "vt." or "形容词" is a label, whatever it is called —
// the same evidence-over-convention argument that lets IPA be detected from
// its characters.
func looksLikePOSLabel(node *html.Node) bool {
	if classMatchesHint(node, posClassHints) {
		return true
	}
	if !isTextLeaf(node) {
		return false
	}
	text := Normalize(Text(node, TextOptions{SkipHidden: true}))
	return text != "" && len([]rune(text)) <= 24
}

// genericPOSFor searches a sense block and the elements preceding it for a
// recognisable part-of-speech label.
func (s *parseState) genericPOSFor(node *html.Node) string {
	inside := ""
	Walk(node, func(child *html.Node) bool {
		if inside != "" {
			return false
		}
		if looksLikePOSLabel(child) {
			if pos := CanonicalPOS(Text(child, TextOptions{SkipHidden: true})); pos != "" {
				inside = pos
				return false
			}
		}
		return true
	})
	if inside != "" {
		return inside
	}

	// Walk backwards through preceding siblings and ancestors' siblings, which
	// is where a shared POS heading for a group of senses lives.
	for current := node; current != nil; current = current.Parent {
		for sibling := current.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
			if sibling.Type != html.ElementNode {
				continue
			}
			if pos := s.scanForPOS(sibling); pos != "" {
				return pos
			}
		}
	}
	return ""
}

func (s *parseState) scanForPOS(node *html.Node) string {
	found := ""
	Walk(node, func(child *html.Node) bool {
		if found != "" {
			return false
		}
		if !looksLikePOSLabel(child) {
			return true
		}
		if pos := CanonicalPOS(Text(child, TextOptions{SkipHidden: true})); pos != "" {
			found = pos
			return false
		}
		return true
	})
	if found == "" {
		if pos := CanonicalPOS(Text(node, TextOptions{SkipHidden: true})); pos != "" {
			return pos
		}
	}
	return found
}

// genericSense builds one sense using class-name evidence.
func (s *parseState) genericSense(node *html.Node) entryir.Sense {
	sense := entryir.Sense{Confidence: 0.65, Rule: "generic:senseHints"}

	// Examples first, then detached, so the definition is not polluted by them.
	var exampleNodes []*html.Node
	Walk(node, func(child *html.Node) bool {
		if child == node {
			return true
		}
		if classMatchesHint(child, exampleClassHints) {
			exampleNodes = append(exampleNodes, child)
			return false
		}
		return true
	})
	exampleNodes = append(exampleNodes, listItemExamples(node)...)
	for _, exNode := range exampleNodes {
		text, translation := s.splitTranslation(exNode)
		if text != "" && len(sense.Examples) < s.opts.MaxExamplesPerSense {
			sense.Examples = append(sense.Examples, entryir.Example{
				Text:        text,
				Translation: translation,
				Audio:       s.resolveAudioFrom(exNode, audioAttrs),
			})
		}
		if exNode.Parent != nil {
			exNode.Parent.RemoveChild(exNode)
		}
	}

	// Labels.
	Walk(node, func(child *html.Node) bool {
		if child == node {
			return true
		}
		if classMatchesHint(child, labelClassHints) {
			text := Normalize(Text(child, TextOptions{SkipHidden: true}))
			if IsKnownLabel(text) {
				sense.Labels = append(sense.Labels, splitList(strings.Trim(text, "()[]"))...)
				return false
			}
		}
		return true
	})

	// Definition: prefer a node explicitly classed as one.
	var definitionNode *html.Node
	Walk(node, func(child *html.Node) bool {
		if child == node || definitionNode != nil {
			return definitionNode == nil
		}
		if classMatchesHint(child, definitionClassHints) {
			if text := Normalize(Text(child, TextOptions{SkipHidden: true})); len([]rune(text)) >= 3 {
				definitionNode = child
				return false
			}
		}
		return true
	})
	target := node
	if definitionNode != nil {
		target = definitionNode
		sense.Confidence = 0.75
	}
	sense.Definition, sense.Translation = s.splitTranslation(target)

	// A leading "1." belongs in the number field, not the definition text.
	if number, rest := leadingNumber(sense.Definition); number != "" {
		sense.Number = number
		sense.Definition = rest
	}
	dedupeStrings(&sense.Labels)
	return sense
}

// listItemExamples treats a list nested inside a sense as that sense's
// examples or citations.
//
// A sense that contains a <ul> or <ol> is not describing its meaning in a
// bulleted list; every dictionary in the corpus that nests one uses it for
// citations, corpus sentences or sub-items. Lifting them out is what stops a
// sense definition from swallowing the rest of the record — the single most
// common way a structured parse turns into an unreadable one.
func listItemExamples(node *html.Node) []*html.Node {
	var out []*html.Node
	for _, list := range QueryAll(node, ParseSelector("ul, ol")) {
		items := QueryAll(list, ParseSelector("li"))
		if len(items) == 0 {
			continue
		}
		out = append(out, items...)
	}
	return out
}

// leadingNumber splits "1. to leave someone" into "1" and the rest.
func leadingNumber(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits > 3 || digits >= len(trimmed) {
		return "", text
	}
	rest := strings.TrimLeft(trimmed[digits:], ". )、 ")
	if rest == trimmed[digits:] {
		return "", text
	}
	if rest == "" {
		return "", text
	}
	return trimmed[:digits], rest
}

// FallbackSectionTitle labels the untyped section the generic parser emits
// when no structure could be recovered. Diagnostics identify a fallback parse
// by this title rather than by guessing from sense counts.
const FallbackSectionTitle = "Entry"

// genericFallbackSection preserves an entry whose structure could not be
// recovered, labelled honestly rather than presented as parsed definitions.
func (s *parseState) genericFallbackSection() {
	text := Normalize(Text(s.doc, TextOptions{SkipHidden: true}))
	if text == "" {
		return
	}
	const limit = 4000
	if len([]rune(text)) > limit {
		runes := []rune(text)
		text = string(runes[:limit]) + " …"
	}
	s.entry.Sections = append(s.entry.Sections, entryir.Section{Title: FallbackSectionTitle, Body: text})
	s.note("no structure recognised; emitted raw entry text as a section")
}

// parseGenericCrossReferences lifts "see also" pointers out of an entry with
// no profile, and detaches them so they are not read as part of a definition.
func (s *parseState) parseGenericCrossReferences() {
	if s.profile != nil {
		return
	}
	var found []*html.Node
	Walk(s.doc, func(node *html.Node) bool {
		if node == s.doc {
			return true
		}
		// An <a> is a link, not a section. The block that holds the links is
		// what names itself a cross-reference.
		if node.Data == "a" || !classTokenMatches(node, crossRefClassHints) {
			return true
		}
		found = append(found, node)
		return false
	})
	if len(found) > maxCrossReferenceBlocks {
		return
	}
	for _, node := range found {
		text := stripLeadingMarker(s.textOf(node))
		for _, item := range splitList(text) {
			if len([]rune(item)) > 60 || len(s.entry.CrossReferences) >= maxGenericCrossReferences {
				continue
			}
			s.entry.CrossReferences = append(s.entry.CrossReferences, item)
		}
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}
	if len(found) > 0 {
		s.note("generic: %d cross-reference blocks", len(found))
	}
}
