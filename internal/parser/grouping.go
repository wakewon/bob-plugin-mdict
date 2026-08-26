package parser

import (
	"sort"
	"strconv"

	"golang.org/x/net/html"
)

// Some dictionaries neither wrap a sense in a container nor number it. What
// they do instead is repeat one recognisable element — a definition span, a
// <dd> — once per meaning, and let everything between two of them belong to
// the first. Those repetitions are the sense boundaries, and finding them is
// the last structural evidence available before the parser gives up.

// parseGenericGroupedSenses recovers senses from repeated boundary elements.
// It reports whether it produced anything.
func (s *parseState) parseGenericGroupedSenses() bool {
	if blocks := s.definitionListSenses(); len(blocks) > 0 {
		s.note("generic: %d senses from a definition list", len(blocks))
		return s.buildEnumeratedParts(blocks, "generic:definitionList")
	}
	if blocks := s.definitionRunSenses(); len(blocks) > 0 {
		s.note("generic: %d senses from repeated definition blocks", len(blocks))
		return s.buildEnumeratedParts(blocks, "generic:definitionRun")
	}
	return false
}

// definitionListSenses reads <dl> the way HTML defines it: <dd> is what a term
// means, and several of them are several meanings. Bilingual dictionaries that
// grew out of a printed two-column layout use it constantly.
func (s *parseState) definitionListSenses() []enumBlock {
	for _, list := range QueryAll(s.doc, ParseSelector("dl")) {
		var blocks []enumBlock
		for child := list.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode || child.Data != "dd" {
				continue
			}
			if len([]rune(Normalize(s.textOf(child)))) < minSenseTextRunes {
				continue
			}
			blocks = append(blocks, enumBlock{node: child, marker: ordinalMarker(len(blocks) + 1)})
		}
		if len(blocks) >= 2 {
			return blocks
		}
	}
	return nil
}

// definitionRunSenses splits a parent's children at every element that looks
// like the start of a definition.
//
// The boundary element has to look like a definition and not like an example:
// a class such as `def-sentence-from` satisfies both vocabularies, and reading
// example sentences as sense boundaries would invert the entry.
func (s *parseState) definitionRunSenses() []enumBlock {
	return s.bestBoundarySplit(func(child *html.Node) (enumMarker, bool) {
		if !classMatchesHint(child, definitionClassHints) {
			return enumMarker{}, false
		}
		if classMatchesHint(child, exampleClassHints) || classMatchesHint(child, senseClassHints) {
			return enumMarker{}, false
		}
		return enumMarker{}, true
	}, true)
}

// boundarySplit is one parent's prospective division into senses, measured
// before anything is moved.
type boundarySplit struct {
	parent   *html.Node
	children []*html.Node
	starts   []int
	markers  []enumMarker
	sizes    []int
}

// bestBoundarySplit finds the parent that divides into the most senses, then
// performs the division on that one alone.
//
// Splitting is destructive — the children are moved into new containers — so
// it must not happen speculatively. An earlier version restructured every
// candidate parent while looking for the best, which emptied parents that were
// then rejected and truncated the fallback text that should have survived.
func (s *parseState) bestBoundarySplit(isBoundary func(*html.Node) (enumMarker, bool), keepBoundary bool) []enumBlock {
	var best *boundarySplit
	Walk(s.doc, func(parent *html.Node) bool {
		split := s.measureSplit(parent, isBoundary, keepBoundary)
		if split == nil {
			return true
		}
		if best == nil || len(split.starts) > len(best.starts) {
			best = split
		}
		return true
	})
	if best == nil {
		return nil
	}
	return s.materializeSplit(best, keepBoundary)
}

// measureSplit evaluates a parent without touching it.
func (s *parseState) measureSplit(parent *html.Node, isBoundary func(*html.Node) (enumMarker, bool), keepBoundary bool) *boundarySplit {
	var children []*html.Node
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		children = append(children, child)
	}
	split := &boundarySplit{parent: parent, children: children}
	for i, child := range children {
		if child.Type != html.ElementNode {
			continue
		}
		marker, ok := isBoundary(child)
		if !ok {
			continue
		}
		split.markers = append(split.markers, marker)
		split.starts = append(split.starts, i)
	}
	if len(split.starts) < 2 || len(split.starts) > maxEnumeratedSenses {
		return nil
	}
	usable := 0
	for i, start := range split.starts {
		end := len(children)
		if i+1 < len(split.starts) {
			end = split.starts[i+1]
		}
		first := start
		if !keepBoundary {
			first = start + 1
		}
		size := 0
		for _, node := range children[first:end] {
			size += len([]rune(Normalize(s.anyText(node))))
		}
		split.sizes = append(split.sizes, size)
		if size >= minSenseTextRunes {
			usable++
		}
	}
	if usable < 2 || !plausibleSenseSizes(split.sizes) {
		return nil
	}
	return split
}

// materializeSplit moves each group into a container of its own, so the rest
// of the parser can treat them as ordinary sense blocks. The document is
// parsed fresh for every record and discarded afterwards, so moving nodes is
// safe once the division has been decided.
func (s *parseState) materializeSplit(split *boundarySplit, keepBoundary bool) []enumBlock {
	var blocks []enumBlock
	for i, start := range split.starts {
		end := len(split.children)
		if i+1 < len(split.starts) {
			end = split.starts[i+1]
		}
		first := start
		if !keepBoundary {
			first = start + 1
		}
		if split.sizes[i] < minSenseTextRunes {
			continue
		}
		container := &html.Node{Type: html.ElementNode, Data: "div"}
		for _, node := range split.children[first:end] {
			if node.Parent != nil {
				node.Parent.RemoveChild(node)
			}
			container.AppendChild(node)
		}
		marker := split.markers[i]
		if marker.Kind == markerNone {
			marker = ordinalMarker(len(blocks) + 1)
		}
		blocks = append(blocks, enumBlock{node: container, marker: marker})
	}
	if len(blocks) < 2 {
		return nil
	}
	return blocks
}

// maxSenseTextRunes is how long a "sense" may be before the split is more
// likely to be at the wrong level of the entry.
//
// One Oxford title opens each record with a clickable index of its homographs,
// numbered 1 to 5. Splitting there is not wrong about the numbering — those
// really are divisions — but each resulting "sense" is an entire homograph
// article of ten thousand characters. A fallback that says "here is the entry"
// is more honest than five definitions that are nothing of the kind.
const maxSenseTextRunes = 1500

// plausibleSenseSizes rejects a division whose typical piece is far too large
// to be a meaning.
func plausibleSenseSizes(sizes []int) bool {
	if len(sizes) == 0 {
		return false
	}
	ordered := append([]int(nil), sizes...)
	sort.Ints(ordered)
	return ordered[len(ordered)/2] <= maxSenseTextRunes
}

// ordinalMarker numbers a sense whose only claim to a number is its position.
func ordinalMarker(position int) enumMarker {
	return enumMarker{
		Text:    strconv.Itoa(position),
		Major:   position,
		Ordinal: composeOrdinal(position, 0, 0),
		Kind:    markerArabic,
	}
}
