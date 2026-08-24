// Package parser turns heterogeneous dictionary HTML into the Entry IR.
package parser

import (
	"strings"

	"golang.org/x/net/html"
)

// Selector is a compiled CSS-subset selector.
//
// The supported grammar is deliberately small — it is everything the dictionary
// profiles need and nothing more:
//
//	tag            div
//	class          .Sense
//	id             #abandon_1
//	attribute      [href], [class^=phon]
//	compound       a.speaker.brefile
//	descendant     .p-g .def-g .d
//	alternatives   .DEF, .ind
//
// There is no cascade, specificity or pseudo-class support here; this matches
// nodes, it does not style them.
type Selector struct {
	// alternatives are OR-ed; each is a descendant chain matched right to left.
	alternatives [][]compoundMatcher
	raw          string
}

type compoundMatcher struct {
	tag     string
	classes []string
	id      string
	attrs   []attrMatcher
}

type attrMatcher struct {
	name string
	// op is "" (presence), "=", "^=", "$=" or "*=".
	op    string
	value string
}

// ParseSelector compiles a selector string. Invalid fragments are dropped
// rather than raising, so one malformed profile rule cannot break a dictionary.
func ParseSelector(raw string) Selector {
	sel := Selector{raw: raw}
	for _, alternative := range strings.Split(raw, ",") {
		alternative = strings.TrimSpace(alternative)
		if alternative == "" {
			continue
		}
		var chain []compoundMatcher
		for _, part := range strings.Fields(alternative) {
			matcher, ok := parseCompound(part)
			if !ok {
				chain = nil
				break
			}
			chain = append(chain, matcher)
		}
		if len(chain) > 0 {
			sel.alternatives = append(sel.alternatives, chain)
		}
	}
	return sel
}

// ParseSelectors compiles a list of selector strings into one Selector whose
// alternatives are the union of them all.
func ParseSelectors(raws []string) Selector {
	var combined Selector
	combined.raw = strings.Join(raws, ", ")
	for _, raw := range raws {
		combined.alternatives = append(combined.alternatives, ParseSelector(raw).alternatives...)
	}
	return combined
}

func parseCompound(part string) (compoundMatcher, bool) {
	var matcher compoundMatcher
	index := 0

	// Leading bare tag name.
	for index < len(part) && isNameByte(part[index]) {
		index++
	}
	if index > 0 {
		matcher.tag = strings.ToLower(part[:index])
	}

	for index < len(part) {
		switch part[index] {
		case '.':
			index++
			start := index
			for index < len(part) && isNameByte(part[index]) {
				index++
			}
			if start == index {
				return matcher, false
			}
			matcher.classes = append(matcher.classes, part[start:index])
		case '#':
			index++
			start := index
			for index < len(part) && isNameByte(part[index]) {
				index++
			}
			if start == index {
				return matcher, false
			}
			matcher.id = part[start:index]
		case '[':
			closing := strings.IndexByte(part[index:], ']')
			if closing < 0 {
				return matcher, false
			}
			body := part[index+1 : index+closing]
			index += closing + 1
			matcher.attrs = append(matcher.attrs, parseAttr(body))
		default:
			return matcher, false
		}
	}
	if matcher.tag == "" && len(matcher.classes) == 0 && matcher.id == "" && len(matcher.attrs) == 0 {
		return matcher, false
	}
	return matcher, true
}

func parseAttr(body string) attrMatcher {
	for _, op := range []string{"^=", "$=", "*=", "="} {
		if idx := strings.Index(body, op); idx > 0 {
			return attrMatcher{
				name:  strings.ToLower(strings.TrimSpace(body[:idx])),
				op:    op,
				value: strings.Trim(strings.TrimSpace(body[idx+len(op):]), `"'`),
			}
		}
	}
	return attrMatcher{name: strings.ToLower(strings.TrimSpace(body))}
}

func isNameByte(b byte) bool {
	return b == '-' || b == '_' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// IsEmpty reports whether the selector matches nothing at all.
func (s Selector) IsEmpty() bool { return len(s.alternatives) == 0 }

// String returns the original selector text, for diagnostics.
func (s Selector) String() string { return s.raw }

// Matches reports whether a node satisfies the selector.
func (s Selector) Matches(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	for _, chain := range s.alternatives {
		if matchChain(chain, node) {
			return true
		}
	}
	return false
}

// matchChain walks the descendant chain right to left from the candidate node.
func matchChain(chain []compoundMatcher, node *html.Node) bool {
	last := len(chain) - 1
	if !matchCompound(chain[last], node) {
		return false
	}
	current := node.Parent
	for i := last - 1; i >= 0; i-- {
		matched := false
		for ancestor := current; ancestor != nil; ancestor = ancestor.Parent {
			if ancestor.Type == html.ElementNode && matchCompound(chain[i], ancestor) {
				current = ancestor.Parent
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func matchCompound(matcher compoundMatcher, node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	if matcher.tag != "" && matcher.tag != node.Data {
		return false
	}
	if matcher.id != "" && Attr(node, "id") != matcher.id {
		return false
	}
	if len(matcher.classes) > 0 {
		classes := ClassSet(node)
		for _, want := range matcher.classes {
			if _, ok := classes[want]; !ok {
				return false
			}
		}
	}
	for _, attr := range matcher.attrs {
		value, present := AttrOK(node, attr.name)
		if !present {
			return false
		}
		switch attr.op {
		case "":
		case "=":
			if value != attr.value {
				return false
			}
		case "^=":
			if !strings.HasPrefix(value, attr.value) {
				return false
			}
		case "$=":
			if !strings.HasSuffix(value, attr.value) {
				return false
			}
		case "*=":
			if !strings.Contains(value, attr.value) {
				return false
			}
		}
	}
	return true
}
