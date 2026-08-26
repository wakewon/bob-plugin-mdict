package parser

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// Enumeration is the one sense convention that survives every publisher and
// every language.
//
// Class names do not: a corpus of a hundred real dictionaries shows several
// with machine-generated class names (`.ysl`, `.muj`, `.xsx`), sixteen with no
// class attribute at all, and no class vocabulary shared by more than a handful
// of unrelated titles. What those dictionaries do share is that they number
// their meanings — 1., ①, (a), II — and that the numbering is visible in the
// text rather than in the markup.
//
// So when class-name evidence finds nothing, the parser looks for numbering
// instead. It requires a real sequence of at least two markers before it will
// claim anything, because a lone "1." is as likely to be a homograph
// superscript or a quantity as it is to be a sense.

// markerKind separates numbering systems that must not be mixed in one
// sequence.
type markerKind int

const (
	markerNone markerKind = iota
	markerArabic
	markerCircled
	markerLetter
	markerRoman
)

// enumMarker is one parsed enumeration marker.
type enumMarker struct {
	// Text is the marker as it should appear in the IR ("1", "1.1", "a").
	Text string
	// Major is the leading position: 1 for both "1" and "1.2".
	Major int
	// Ordinal is a composite sort key that keeps "1" < "1.1" < "1.2" < "2",
	// so a run consisting only of subsense numbers still reads as a sequence.
	Ordinal int
	// Parent is the owning marker for a dotted marker: "1.1" belongs to "1".
	Parent string
	Kind   markerKind
}

// maxEnumeratedSenses bounds what the parser is willing to call a sense list.
// Beyond this a "sequence" is a table of contents or a corpus citation list.
const maxEnumeratedSenses = 80

// maxMarkerGroup is the larger bound for a group of like elements gathered
// across a whole record. A big verb in an unabridged dictionary really does
// carry a hundred numbered senses spread over several parts of speech, and the
// sequence check — not the count — is what keeps a list of contents out.
const maxMarkerGroup = 200

// minSenseTextRunes is the shortest text that can follow a marker and still be
// a meaning rather than a stray digit.
const minSenseTextRunes = 3

// minSenseShareOfRecord is how much of a record its sense list has to account
// for.
//
// A dictionary entry is mostly its meanings. A numbered list that accounts for
// a fortieth of the record is an index of the record, not the meanings in it —
// which is exactly what an encyclopedia's table of contents looks like to a
// parser that only checks whether the numbering ascends. The bar is set low
// because a long etymology, a usage note or a citation block can legitimately
// outweigh short definitions; it only has to separate "some of the entry" from
// "a listing of it".
const minSenseShareOfRecord = 0.12

// exampleBilingualShare is how consistently an element shape has to carry a
// translation before that becomes the test for what an example is.
const exampleBilingualShare = 0.6

var openingBrackets = "(（[【〔"
var closingBrackets = ")）]】〕"

// splitEnumMarker parses a leading enumeration marker, returning the marker,
// the text that follows it, and whether one was found.
//
// Roman numerals are deliberately not accepted here: a definition beginning
// "I " is far more often a pronoun than a section number. They are accepted
// only as a standalone marker element, where a sequence must prove itself.
func splitEnumMarker(text string) (enumMarker, string, bool) {
	trimmed := Normalize(text)
	if trimmed == "" {
		return enumMarker{}, text, false
	}
	runes := []rune(trimmed)
	index := 0
	bracketed := false
	if strings.ContainsRune(openingBrackets, runes[0]) {
		bracketed = true
		index++
	}
	marker, consumed, kind := scanMarkerBody(runes[index:], false)
	if kind == markerNone {
		return enumMarker{}, text, false
	}
	index += consumed

	// A closing bracket, punctuation or plain space may separate the marker
	// from the text. A bracketed marker must be closed.
	closed := false
	if index < len(runes) && strings.ContainsRune(closingBrackets, runes[index]) {
		closed = true
		index++
	}
	if bracketed && !closed {
		return enumMarker{}, text, false
	}
	separated := closed
	for index < len(runes) && strings.ContainsRune(".、,:：)．。 \t", runes[index]) {
		separated = true
		index++
	}
	if !separated {
		return enumMarker{}, text, false
	}
	rest := strings.TrimSpace(string(runes[index:]))
	if rest == "" {
		return enumMarker{}, text, false
	}
	// "1 000 metres" is a quantity, not a sense number. A marker separated only
	// by a space has to be followed by a letter.
	if !closed && kind == markerArabic {
		next := []rune(rest)[0]
		if unicode.IsDigit(next) {
			return enumMarker{}, text, false
		}
	}
	// "v. transitive" opens with a letter and a full stop and is a part of
	// speech, not sense (v). Dictionaries abbreviate their word classes to
	// exactly the shape a lettered marker has.
	if kind == markerLetter && CanonicalPOS(marker.Text+".") != "" {
		return enumMarker{}, text, false
	}
	return marker, rest, true
}

// markerOnly parses an element whose entire text is a marker, which is how
// dictionaries that keep the number in its own span or bold tag are built.
func markerOnly(text string) (enumMarker, bool) {
	trimmed := Normalize(text)
	if trimmed == "" || len([]rune(trimmed)) > 8 {
		return enumMarker{}, false
	}
	runes := []rune(trimmed)
	index := 0
	if strings.ContainsRune(openingBrackets, runes[0]) {
		index++
	}
	marker, consumed, kind := scanMarkerBody(runes[index:], true)
	if kind == markerNone {
		return enumMarker{}, false
	}
	index += consumed
	for index < len(runes) && strings.ContainsRune(closingBrackets+".、,:：．。 ", runes[index]) {
		index++
	}
	if index != len(runes) {
		return enumMarker{}, false
	}
	return marker, true
}

// scanMarkerBody reads the marker itself, returning how many runes it used.
func scanMarkerBody(runes []rune, allowRoman bool) (enumMarker, int, markerKind) {
	if len(runes) == 0 {
		return enumMarker{}, 0, markerNone
	}
	if ordinal, ok := circledOrdinal(runes[0]); ok {
		return enumMarker{
			Text: strconv.Itoa(ordinal), Major: ordinal, Ordinal: composeOrdinal(ordinal, 0, 0), Kind: markerCircled,
		}, 1, markerCircled
	}
	if unicode.IsDigit(runes[0]) {
		index := 0
		var groups []string
		for index < len(runes) {
			start := index
			for index < len(runes) && unicode.IsDigit(runes[index]) && index-start < 3 {
				index++
			}
			if index == start {
				break
			}
			groups = append(groups, string(runes[start:index]))
			// A dotted marker continues only when a digit follows the dot.
			if index+1 < len(runes) && runes[index] == '.' && unicode.IsDigit(runes[index+1]) && len(groups) < 3 {
				index++
				continue
			}
			break
		}
		if len(groups) == 0 {
			return enumMarker{}, 0, markerNone
		}
		levels := [3]int{}
		for i, group := range groups {
			value, err := strconv.Atoi(group)
			if err != nil {
				return enumMarker{}, 0, markerNone
			}
			levels[i] = value
		}
		marker := enumMarker{
			Text:    strings.Join(groups, "."),
			Major:   levels[0],
			Ordinal: composeOrdinal(levels[0], levels[1], levels[2]),
			Kind:    markerArabic,
		}
		if len(groups) > 1 {
			marker.Parent = strings.Join(groups[:len(groups)-1], ".")
		}
		return marker, index, markerArabic
	}
	if allowRoman {
		if ordinal, consumed, ok := romanOrdinal(runes); ok {
			return enumMarker{
				Text: string(runes[:consumed]), Major: ordinal, Ordinal: composeOrdinal(ordinal, 0, 0), Kind: markerRoman,
			}, consumed, markerRoman
		}
	}
	if len(runes) >= 1 && runes[0] >= 'a' && runes[0] <= 'z' {
		// A single letter needs explicit punctuation after it. A space is not
		// enough: "a barbecue area" would otherwise be sense "a", and English
		// examples begin with an article constantly.
		if len(runes) == 1 || strings.ContainsRune(closingBrackets+".、)．", runes[1]) {
			ordinal := int(runes[0]-'a') + 1
			return enumMarker{
				Text:    string(runes[0]),
				Major:   ordinal,
				Ordinal: composeOrdinal(ordinal, 0, 0),
				Kind:    markerLetter,
			}, 1, markerLetter
		}
	}
	return enumMarker{}, 0, markerNone
}

// circledOrdinal decodes ①-⑳ and ❶-❿, which CJK dictionaries use constantly.
func circledOrdinal(r rune) (int, bool) {
	switch {
	case r >= '①' && r <= '⑳':
		return int(r-'①') + 1, true
	case r >= '❶' && r <= '❿':
		return int(r-'❶') + 1, true
	case r >= '㈠' && r <= '㈩':
		return int(r-'㈠') + 1, true
	}
	return 0, false
}

// composeOrdinal packs a dotted marker into one comparable number.
func composeOrdinal(major, minor, sub int) int {
	return major*10000 + minor*100 + sub
}

var romanValues = map[rune]int{'I': 1, 'V': 5, 'X': 10}

func romanOrdinal(runes []rune) (int, int, bool) {
	consumed := 0
	for consumed < len(runes) && consumed < 6 {
		if _, ok := romanValues[runes[consumed]]; !ok {
			break
		}
		consumed++
	}
	if consumed == 0 {
		return 0, 0, false
	}
	total, previous := 0, 0
	for i := consumed - 1; i >= 0; i-- {
		value := romanValues[runes[i]]
		if value < previous {
			total -= value
		} else {
			total += value
			previous = value
		}
	}
	if total <= 0 || total > 30 {
		return 0, 0, false
	}
	return total, consumed, true
}

// plausibleSequence decides whether a run of markers is really an enumeration.
//
// It is the whole safety mechanism of this strategy: without it, one number
// anywhere in a record would restructure the entry.
func plausibleSequence(markers []enumMarker) bool {
	if len(markers) < 2 || len(markers) > maxMarkerGroup {
		return false
	}
	kind := markers[0].Kind
	if kind == markerNone {
		return false
	}
	// A real sense list starts at the beginning. One that opens at "7" is a
	// fragment of something else — a cross-reference to another entry's sense,
	// or a numbered list inside a usage note.
	if markers[0].Major > 3 {
		return false
	}
	increased := false
	runs := 1
	distinct := map[int]struct{}{}
	for i, marker := range markers {
		if marker.Kind != kind {
			return false
		}
		if marker.Major <= 0 || marker.Major > maxEnumeratedSenses*2 {
			return false
		}
		distinct[marker.Ordinal] = struct{}{}
		if i == 0 {
			continue
		}
		switch {
		case marker.Ordinal > markers[i-1].Ordinal:
			increased = true
		case marker.Ordinal < markers[i-1].Ordinal:
			// Numbering restarts under each part of speech in a great many
			// dictionaries: 1…9 for the transitive verb, then 1…4 for the
			// intransitive. A drop is only allowed back to where the sequence
			// began, which keeps it from excusing arbitrary disorder.
			if marker.Major != markers[0].Major {
				return false
			}
			runs++
		}
	}
	// Every run has to enumerate something. A list that restarts as often as
	// it counts is not a sense list; it is a repeated label the parser has
	// mistaken for numbering.
	return increased && len(distinct) >= 2 && len(markers) >= 2*runs
}

// parseGenericEnumeratedSenses recovers senses from visible numbering when no
// class-name evidence was available. It reports whether it produced anything.
func (s *parseState) parseGenericEnumeratedSenses() bool {
	if blocks := s.markerLedBlocks(); len(blocks) > 0 {
		s.note("generic: %d senses from marker-led blocks", len(blocks))
		return s.buildEnumeratedParts(blocks, "generic:markerBlocks")
	}
	if blocks := s.orderedListSenses(); len(blocks) > 0 {
		s.note("generic: %d senses from an ordered list", len(blocks))
		return s.buildEnumeratedParts(blocks, "generic:orderedList")
	}
	if blocks := s.markerRunSenses(); len(blocks) > 0 {
		s.note("generic: %d senses from a marker run", len(blocks))
		return s.buildEnumeratedParts(blocks, "generic:markerRun")
	}
	return false
}

// enumBlock is one sense recovered from numbering, with any nested level of
// numbering beneath it.
type enumBlock struct {
	node   *html.Node
	marker enumMarker
	// markerNode is the element that held the number, when the number lives in
	// its own element rather than in the block's text. It is detached before
	// the definition is read.
	markerNode *html.Node
	// examples are sibling elements that follow this block and belong to it.
	examples []*html.Node
	children []enumBlock
}

// markerLedBlocks finds the elements whose numbering is the entry's sense
// list, together with the level beneath them, which holds subsenses.
//
// A block qualifies as numbered two ways, because dictionaries split evenly
// between them: its text starts with "1." and a separator, or its first
// element child holds nothing but the number. The second form has no separator
// at all — one Oxford build renders the number and the definition as adjacent
// spans, so the text reads "1A meal or gathering…" and no amount of text
// scanning finds the boundary that the markup states plainly.
//
// Candidates are then grouped by what kind of element they are — tag plus
// class — rather than by how deep they sit. Depth is not a reliable level:
// American Heritage nests its senses inside a part-of-speech block in the
// entry body but not inside the idioms that follow, so the same sense level
// appears at two different depths in one record. What is constant is that a
// dictionary marks its senses with one consistent element, so `div.ds-list`
// is the sense list and `div.sds-list` inside it is the subsense list.
func (s *parseState) markerLedBlocks() []enumBlock {
	candidates := s.markerCandidates()
	if len(candidates) < 2 {
		return nil
	}
	for _, keyOf := range []func(*html.Node) string{signatureKey, tagKey} {
		if blocks := s.chooseMarkerGroup(candidates, keyOf); blocks != nil {
			return blocks
		}
	}
	return nil
}

// signatureKey identifies an element by tag and class, which is how a
// dictionary names one structural role.
func signatureKey(node *html.Node) string {
	classes := strings.Fields(Attr(node, "class"))
	sort.Strings(classes)
	return node.Data + "." + strings.Join(classes, ".")
}

// tagKey is the fallback for dictionaries that carry no class attributes.
func tagKey(node *html.Node) string { return node.Data }

// chooseMarkerGroup picks the outermost group of like elements whose markers
// really are a sequence, and attaches the group nested inside it as subsenses.
func (s *parseState) chooseMarkerGroup(candidates []enumBlock, keyOf func(*html.Node) string) []enumBlock {
	index := make(map[*html.Node]int, len(candidates))
	for i, item := range candidates {
		index[item.node] = i
	}
	// nearest is the closest enclosing candidate, which is what makes a
	// subsense belong to a sense rather than to the entry.
	nearest := make([]int, len(candidates))
	for i, item := range candidates {
		nearest[i] = -1
		for parent := item.node.Parent; parent != nil; parent = parent.Parent {
			if owner, ok := index[parent]; ok {
				nearest[i] = owner
				break
			}
		}
	}

	groups := map[string][]int{}
	var order []string
	for i, item := range candidates {
		key := keyOf(item.node)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}

	// Senses sit beside one another, never inside one another. Markup with
	// unclosed tags parses into a chain where each sense element contains the
	// whole rest of the entry, and reading that as a sense list emits the
	// entry once per sense. Keeping only the outermost members of a group
	// leaves the real senses where the nesting is an artefact, and leaves a
	// single member — which cannot be a sequence — where it is not.
	for key, members := range groups {
		groups[key] = outermost(members, nearest, groups[key])
	}

	var passing []string
	for _, key := range order {
		members := groups[key]
		if len(members) < 2 || len(members) > maxMarkerGroup {
			continue
		}
		markers := make([]enumMarker, 0, len(members))
		for _, member := range members {
			markers = append(markers, candidates[member].marker)
		}
		if plausibleSequence(markers) {
			passing = append(passing, key)
		}
	}
	if len(passing) == 0 {
		return nil
	}

	// Prefer a group that encloses another: senses contain subsenses, never
	// the reverse.
	enclosed := map[string]bool{}
	for _, outer := range passing {
		for _, inner := range passing {
			if outer == inner {
				continue
			}
			for _, member := range groups[inner] {
				if owner := nearest[member]; owner >= 0 && keyOf(candidates[owner].node) == outer {
					enclosed[inner] = true
				}
			}
		}
	}
	chosen := ""
	for _, key := range passing {
		if enclosed[key] {
			continue
		}
		if chosen == "" || len(groups[key]) > len(groups[chosen]) {
			chosen = key
		}
	}
	if chosen == "" {
		return nil
	}

	position := make(map[int]int, len(groups[chosen]))
	blocks := make([]enumBlock, 0, len(groups[chosen]))
	for _, member := range groups[chosen] {
		position[member] = len(blocks)
		blocks = append(blocks, candidates[member])
	}
	for i, item := range candidates {
		if _, isSense := position[i]; isSense {
			continue
		}
		owner, ok := position[nearest[i]]
		if !ok {
			continue
		}
		blocks[owner].children = append(blocks[owner].children, item)
	}
	for i := range blocks {
		if !sameMarkerKind(blocks[i].children) {
			blocks[i].children = nil
			continue
		}
		// A single nested numbered block may be a wrapper repeating its
		// parent, or it may be the one subsense that sense happens to have.
		// What tells them apart is whether it is the parent: same number, or
		// nearly all of the parent's text. Discarding it wholesale left a real
		// subsense glued to the end of its parent's definition, with both
		// translations run together after it.
		if len(blocks[i].children) == 1 && s.childRepeatsParent(blocks[i]) {
			blocks[i].children = nil
		}
	}
	s.attachSiblingExamples(blocks)

	sizes := make([]int, 0, len(blocks))
	total := 0
	for _, block := range blocks {
		size := len([]rune(Normalize(s.textOf(block.node))))
		sizes = append(sizes, size)
		for _, example := range block.examples {
			total += len([]rune(Normalize(s.textOf(example))))
		}
		total += size
	}
	if !plausibleSenseSizes(sizes) {
		return nil
	}
	if record := s.recordTextRunes(); record > 0 && float64(total) < minSenseShareOfRecord*float64(record) {
		return nil
	}
	detachMarkerNodes(blocks)
	return blocks
}

// outermost drops members that are contained by another member of the same
// group, following the chain of enclosing candidates.
func outermost(members []int, nearest []int, group []int) []int {
	inGroup := make(map[int]struct{}, len(group))
	for _, member := range group {
		inGroup[member] = struct{}{}
	}
	kept := make([]int, 0, len(members))
	for _, member := range members {
		nested := false
		for owner := nearest[member]; owner >= 0; owner = nearest[owner] {
			if _, ok := inGroup[owner]; ok {
				nested = true
				break
			}
		}
		if !nested {
			kept = append(kept, member)
		}
	}
	return kept
}

// attachSiblingExamples gives each numbered block the material that follows it.
func (s *parseState) attachSiblingExamples(blocks []enumBlock) {
	nodes := make([]*html.Node, 0, len(blocks))
	for _, block := range blocks {
		nodes = append(nodes, block.node)
	}
	runs := s.siblingRuns(nodes)
	for i := range blocks {
		blocks[i].examples = runs[blocks[i].node]
	}
}

// siblingRuns divides the material around a set of sense elements between
// them.
//
// A great many dictionaries put the number and the definition in one element
// and the examples in the elements after it, as siblings rather than children.
// Everything between one sense and the next belongs to the first — that is
// what a numbered list means on a printed page — and without this the examples
// are simply lost: they are inside no sense, so no sense ever reads them.
//
// Which of those siblings are examples is decided, where it can be, by the
// dictionary's own habit. In a bilingual dictionary the examples are the
// elements that carry both languages, and the grammar codes, pattern labels
// and next-lemma headings mixed in among them carry only one. Where there is
// no such habit to read, the boundary rule stands on its own: attaching a
// grammar code as an example is a small misreading, and dropping the entry's
// examples outright is not.
func (s *parseState) siblingRuns(nodes []*html.Node) map[*html.Node][]*html.Node {
	runs := make(map[*html.Node][]*html.Node, len(nodes))
	if len(nodes) < 2 {
		return runs
	}
	boundary := make(map[*html.Node]struct{}, len(nodes))
	byParent := map[*html.Node]int{}
	for _, node := range nodes {
		boundary[node] = struct{}{}
		if node.Parent != nil {
			byParent[node.Parent]++
		}
	}

	// The material after the last sense has no next sense to end it, so it runs
	// to the end of the parent and takes with it whatever the dictionary
	// prints after its sense list — derivatives, a thesaurus panel, an origin
	// note. Those are not that sense's examples, and attaching them both
	// swallows the sections and duplicates them when they are parsed again in
	// their own right.
	last := map[*html.Node]*html.Node{}
	for _, node := range nodes {
		if node.Parent != nil {
			last[node.Parent] = node
		}
	}

	signatures := map[string]int{}
	bilingual := map[string]int{}
	for _, node := range nodes {
		// One sense alone under a parent is not a list, and the rest of that
		// parent is as likely to be a heading as an example.
		if node.Parent == nil || byParent[node.Parent] < 2 {
			continue
		}
		var run []*html.Node
		for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
			if sibling.Type != html.ElementNode {
				continue
			}
			if _, _, heading := semanticHeading(sibling); heading {
				break
			}
			if _, isBoundary := boundary[sibling]; isBoundary {
				break
			}
			if len([]rune(Normalize(s.textOf(sibling)))) < minSenseTextRunes {
				continue
			}
			run = append(run, sibling)
			key := signatureKey(sibling)
			signatures[key]++
			if _, translation := s.splitTranslation(sibling); translation != "" {
				bilingual[key]++
			}
		}
		runs[node] = run
	}

	// The last sense keeps only what the dictionary has already shown to be an
	// example: an element of a shape that held examples for an earlier sense.
	established := map[string]struct{}{}
	for _, node := range nodes {
		if last[node.Parent] == node {
			continue
		}
		for _, candidate := range runs[node] {
			established[signatureKey(candidate)] = struct{}{}
		}
	}
	for _, node := range nodes {
		if last[node.Parent] != node {
			continue
		}
		kept := runs[node][:0]
		for _, candidate := range runs[node] {
			if _, ok := established[signatureKey(candidate)]; ok {
				kept = append(kept, candidate)
			}
		}
		runs[node] = kept
	}

	translated := false
	for key, count := range signatures {
		if count >= 2 && float64(bilingual[key]) >= exampleBilingualShare*float64(count) {
			translated = true
			break
		}
	}
	if !translated {
		return runs
	}
	for node, run := range runs {
		kept := run[:0]
		for _, candidate := range run {
			key := signatureKey(candidate)
			if float64(bilingual[key]) < exampleBilingualShare*float64(signatures[key]) {
				continue
			}
			kept = append(kept, candidate)
		}
		runs[node] = kept
	}
	return runs
}

// appendSiblingExamples adds the material that followed a sense to it.
func (s *parseState) appendSiblingExamples(sense *entryir.Sense, nodes []*html.Node) {
	for _, node := range nodes {
		if len(sense.Examples) >= s.opts.MaxExamplesPerSense {
			return
		}
		text, translation := s.splitTranslation(node)
		if strings.TrimSpace(text) == "" {
			continue
		}
		sense.Examples = append(sense.Examples, entryir.Example{
			Text:        text,
			Translation: translation,
			Audio:       s.resolveAudioFrom(node, audioAttrs),
		})
	}
}

// childRepeatsParent reports whether a lone nested block is its parent again
// rather than a subdivision of it.
func (s *parseState) childRepeatsParent(block enumBlock) bool {
	child := block.children[0]
	if child.marker.Text == block.marker.Text {
		return true
	}
	parentText := len([]rune(Normalize(s.textOf(block.node))))
	childText := len([]rune(Normalize(s.textOf(child.node))))
	return parentText > 0 && float64(childText) >= 0.9*float64(parentText)
}

func sameMarkerKind(blocks []enumBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, block := range blocks[1:] {
		if block.marker.Kind != blocks[0].marker.Kind {
			return false
		}
	}
	return true
}

// markerCandidates collects every element that opens with an enumeration
// marker, in document order.
func (s *parseState) markerCandidates() []enumBlock {
	var candidates []enumBlock
	Walk(s.doc, func(node *html.Node) bool {
		if node == s.doc {
			return true
		}
		// A block whose every word is link text points at content rather than
		// being content. Encyclopedias open with a numbered table of contents
		// built exactly that way, and it ascends as convincingly as any sense
		// list.
		if s.textIsAllLinks(node) {
			return true
		}
		if marker, markerNode, ok := s.leadingMarkerElement(node); ok {
			candidates = append(candidates, enumBlock{node: node, marker: marker, markerNode: markerNode})
			return true
		}
		marker, rest, ok := splitEnumMarker(s.textOf(node))
		if !ok || len([]rune(rest)) < minSenseTextRunes {
			return true
		}
		candidates = append(candidates, enumBlock{node: node, marker: marker})
		return true
	})
	return candidates
}

// detachMarkerNodes removes the number elements, so the definition text read
// from these blocks does not open with the number all over again.
func detachMarkerNodes(blocks []enumBlock) {
	for _, block := range blocks {
		if block.markerNode != nil && block.markerNode.Parent != nil {
			block.markerNode.Parent.RemoveChild(block.markerNode)
		}
		detachMarkerNodes(block.children)
	}
}

// leadingMarkerElement reports whether a block opens with a number held in its
// own element.
func (s *parseState) leadingMarkerElement(node *html.Node) (enumMarker, *html.Node, bool) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		switch child.Type {
		case html.TextNode:
			if strings.TrimSpace(child.Data) != "" {
				return enumMarker{}, nil, false
			}
			continue
		case html.ElementNode:
		default:
			continue
		}
		text := s.textOf(child)
		if strings.TrimSpace(text) == "" {
			continue
		}
		marker, ok := markerOnly(text)
		if !ok {
			return enumMarker{}, nil, false
		}
		// What follows the number has to be a meaning, not another number.
		rest := 0
		for sibling := child.NextSibling; sibling != nil; sibling = sibling.NextSibling {
			rest += len([]rune(Normalize(s.anyText(sibling))))
		}
		if rest < minSenseTextRunes {
			return enumMarker{}, nil, false
		}
		return marker, child, true
	}
	return enumMarker{}, nil, false
}

// textIsAllLinks reports whether every word a node shows comes from a link.
func (s *parseState) textIsAllLinks(node *html.Node) bool {
	if Normalize(s.textOf(node)) == "" {
		return false
	}
	links := map[*html.Node]struct{}{}
	Walk(node, func(child *html.Node) bool {
		if child != node && child.Data == "a" && child.Type == html.ElementNode {
			links[child] = struct{}{}
			return false
		}
		return true
	})
	if len(links) == 0 {
		return false
	}
	return Normalize(Text(node, TextOptions{SkipHidden: true, SkipNodes: links})) == ""
}

// recordTextRunes is how much text the record holds at this point in parsing,
// after chrome and detached sections have gone. It is memoized because the
// sense strategies each ask for it.
func (s *parseState) recordTextRunes() int {
	if s.recordRunes < 0 {
		s.recordRunes = len([]rune(Normalize(s.textOf(s.doc))))
	}
	return s.recordRunes
}

// anyText reads a node's text whether it is an element or a bare text node.
func (s *parseState) anyText(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	return s.textOf(node)
}

// orderedListSenses treats an <ol> as what HTML says it is: an enumeration.
// The list items carry no visible number of their own, so the markup is the
// only place the numbering exists.
func (s *parseState) orderedListSenses() []enumBlock {
	for _, list := range QueryAll(s.doc, ParseSelector("ol")) {
		items := QueryAll(list, ParseSelector("li"))
		if len(items) < 2 || len(items) > maxEnumeratedSenses {
			continue
		}
		linkOnly := 0
		for _, item := range items {
			if s.textIsAllLinks(item) {
				linkOnly++
			}
		}
		// A navigation menu can contain one plain current item among links, so
		// require a strong majority rather than literally every item.
		if linkOnly*4 >= len(items)*3 {
			continue
		}
		var blocks []enumBlock
		for _, item := range items {
			if len([]rune(Normalize(s.textOf(item)))) < minSenseTextRunes {
				continue
			}
			ordinal := len(blocks) + 1
			blocks = append(blocks, enumBlock{node: item, marker: enumMarker{
				Text:    strconv.Itoa(ordinal),
				Major:   ordinal,
				Ordinal: composeOrdinal(ordinal, 0, 0),
				Kind:    markerArabic,
			}})
		}
		if len(blocks) >= 2 {
			return blocks
		}
	}
	return nil
}

// markerRunSenses handles dictionaries that keep the number in its own element
// and leave the meaning as loose siblings after it — a shape with no sense
// container anywhere to select.
func (s *parseState) markerRunSenses() []enumBlock {
	var best *boundarySplit
	Walk(s.doc, func(parent *html.Node) bool {
		// The markers have to be a sequence before any restructuring happens:
		// splitting a parent apart is destructive, and doing it around numbers
		// that are not an enumeration would scatter the entry.
		var markers []enumMarker
		for child := parent.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode {
				continue
			}
			if marker, ok := markerOnly(s.textOf(child)); ok {
				markers = append(markers, marker)
			}
		}
		if !plausibleSequence(markers) {
			return true
		}
		split := s.measureSplit(parent, func(child *html.Node) (enumMarker, bool) {
			return markerOnly(s.textOf(child))
		}, false)
		if split != nil && (best == nil || len(split.starts) > len(best.starts)) {
			best = split
		}
		return true
	})
	if best == nil {
		return nil
	}
	return s.materializeSplit(best, false)
}

// oversizedSense marks a piece that is not a meaning but the remainder of the
// entry.
//
// Numbered indexes at the top of a long entry are a real convention — one
// thesaurus opens every article with its own list of senses and then prints
// the article — and the division at those numbers is correct right up to the
// last one, which takes everything left. The median-size check cannot see it,
// because one huge piece among many small ones leaves the median small.
//
// Such a piece is emitted as untyped content rather than dropped or presented
// as a definition. It is the same choice the parser makes everywhere else:
// text it cannot classify stays visible and stays unlabelled.
const (
	oversizeMedianFactor = 10
	oversizeMinRunes     = 3000
)

// buildEnumeratedParts turns marker-carrying blocks into parts and senses.
func (s *parseState) buildEnumeratedParts(blocks []enumBlock, rule string) bool {
	type group struct {
		pos    string
		senses []entryir.Sense
	}
	var groups []group
	current := -1

	sizes := make([]int, 0, len(blocks))
	for _, block := range blocks {
		sizes = append(sizes, len([]rune(Normalize(s.textOf(block.node)))))
	}
	ordered := append([]int(nil), sizes...)
	sort.Ints(ordered)
	median := ordered[len(ordered)/2]

	for index, block := range blocks {
		if sizes[index] > oversizeMinRunes && sizes[index] > oversizeMedianFactor*median {
			s.genericSectionFrom(block.node)
			s.note("enumerated block %d is too large to be a sense; kept as untyped content", index+1)
			continue
		}
		sense := s.enumeratedSense(block, rule)
		if sense.Definition == "" && sense.Translation == "" &&
			len(sense.Examples) == 0 && len(sense.Subsenses) == 0 {
			continue
		}
		// A dotted marker is a subsense of the marker it extends. Recovering
		// that nesting is free here and is otherwise lost entirely.
		if parent := block.marker.Parent; parent != "" && current >= 0 {
			senses := groups[current].senses
			if len(senses) > 0 && senses[len(senses)-1].Number == parent {
				senses[len(senses)-1].Subsenses = append(senses[len(senses)-1].Subsenses, sense)
				continue
			}
		}
		pos := s.genericPOSFor(block.node)
		if current < 0 || (pos != "" && pos != groups[current].pos) {
			groups = append(groups, group{pos: pos})
			current = len(groups) - 1
		}
		groups[current].senses = append(groups[current].senses, sense)
	}

	produced := false
	for _, item := range groups {
		if len(item.senses) == 0 {
			continue
		}
		produced = true
		s.appendPart(entryir.Part{
			POS:        item.pos,
			Senses:     item.senses,
			Confidence: confidenceForPOS(item.pos) - 0.25,
			Rule:       rule,
		})
	}
	return produced
}

// enumeratedSense builds one sense, taking its nested numbered blocks out
// first so their text does not end up inside the parent's definition.
func (s *parseState) enumeratedSense(block enumBlock, rule string) entryir.Sense {
	var subsenses []entryir.Sense
	for _, child := range block.children {
		sub := s.enumeratedSense(child, rule)
		if sub.Definition != "" || sub.Translation != "" || len(sub.Examples) > 0 {
			subsenses = append(subsenses, sub)
		}
		if child.node.Parent != nil {
			child.node.Parent.RemoveChild(child.node)
		}
	}
	sense := s.genericSense(block.node)
	s.appendSiblingExamples(&sense, block.examples)
	sense.Number = block.marker.Text
	sense.Rule = rule
	sense.Definition = stripLeadingNumber(sense.Definition, sense.Number)
	sense.Subsenses = subsenses
	return sense
}
