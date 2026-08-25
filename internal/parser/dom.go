package parser

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// ClassSet returns the node's class attribute as a set.
func ClassSet(node *html.Node) map[string]struct{} {
	set := make(map[string]struct{})
	for _, field := range strings.Fields(Attr(node, "class")) {
		set[field] = struct{}{}
	}
	return set
}

// Attr returns an attribute value, or "" when absent.
func Attr(node *html.Node, name string) string {
	value, _ := AttrOK(node, name)
	return value
}

// AttrOK returns an attribute value and whether it was present.
func AttrOK(node *html.Node, name string) (string, bool) {
	if node == nil {
		return "", false
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

// whitespaceRe collapses runs of whitespace, including the NBSP that
// dictionaries use liberally for layout.
var whitespaceRe = regexp.MustCompile(`[\s\x{00a0}\x{200b}\x{feff}]+`)

// Normalize collapses whitespace and trims a string.
func Normalize(text string) string {
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(text, " "))
}

// hiddenByStyle reports whether an inline style hides the node.
//
// This is the only CSS the parser interprets, and only because dictionaries use
// display:none to hide the language variant the user did not select — content
// that is genuinely not part of the entry as presented.
func hiddenByStyle(node *html.Node) bool {
	style := strings.ToLower(strings.ReplaceAll(Attr(node, "style"), " ", ""))
	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden")
}

// TextOptions controls text extraction.
type TextOptions struct {
	// Skip matches nodes whose subtree is excluded entirely.
	Skip Selector
	// SkipNodes excludes specific subtrees by identity, for callers that have
	// already decided which nodes to leave out rather than how to select them.
	SkipNodes map[*html.Node]struct{}
	// SkipHidden drops nodes hidden with inline styles.
	SkipHidden bool
	// Separator is inserted between block-level elements.
	Separator string
}

// blockTags get a separator inserted around them so adjacent sentences do not
// run together into one unreadable string.
var blockTags = map[string]bool{
	"div": true, "p": true, "li": true, "br": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "ul": true, "ol": true, "table": true,
}

// Text extracts the visible text of a subtree.
func Text(node *html.Node, opts TextOptions) string {
	var builder strings.Builder
	collectText(node, opts, &builder, true)
	return Normalize(builder.String())
}

func collectText(node *html.Node, opts TextOptions, builder *strings.Builder, isRoot bool) {
	if node == nil {
		return
	}
	switch node.Type {
	case html.TextNode:
		builder.WriteString(node.Data)
		return
	case html.ElementNode:
		if node.Data == "script" || node.Data == "style" || node.Data == "head" {
			return
		}
		if !isRoot {
			if opts.SkipHidden && hiddenByStyle(node) {
				return
			}
			if !opts.Skip.IsEmpty() && opts.Skip.Matches(node) {
				return
			}
			if _, skipped := opts.SkipNodes[node]; skipped {
				return
			}
		}
		if blockTags[node.Data] {
			builder.WriteString(" ")
		}
	case html.CommentNode, html.DoctypeNode:
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectText(child, opts, builder, false)
	}
	if node.Type == html.ElementNode && blockTags[node.Data] {
		builder.WriteString(" ")
	}
}

// Walk visits every element node in document order. Returning false from the
// visitor skips the node's subtree.
func Walk(root *html.Node, visit func(*html.Node) bool) {
	if root == nil {
		return
	}
	if root.Type == html.ElementNode && !visit(root) {
		return
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode {
			Walk(child, visit)
		}
	}
}

// QueryAll returns every descendant matching the selector, in document order.
// Matches are not nested: once a node matches, its subtree is not searched for
// further matches of the same selector, which is what "the sense blocks" or
// "the example blocks" always means in practice.
func QueryAll(root *html.Node, sel Selector) []*html.Node {
	if root == nil || sel.IsEmpty() {
		return nil
	}
	var found []*html.Node
	Walk(root, func(node *html.Node) bool {
		if node == root {
			return true
		}
		if sel.Matches(node) {
			found = append(found, node)
			return false
		}
		return true
	})
	return found
}

// QueryAllNested behaves like QueryAll but also descends into matches, which is
// needed where subsenses share their parent's class.
func QueryAllNested(root *html.Node, sel Selector) []*html.Node {
	if root == nil || sel.IsEmpty() {
		return nil
	}
	var found []*html.Node
	Walk(root, func(node *html.Node) bool {
		if node != root && sel.Matches(node) {
			found = append(found, node)
		}
		return true
	})
	return found
}

// Query returns the first descendant matching the selector.
func Query(root *html.Node, sel Selector) *html.Node {
	matches := QueryAll(root, sel)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// RemoveMatching detaches every node matching the selector from the tree.
// Profiles use it to drop chrome — fold buttons, speaker icons, "show all"
// widgets — before any text extraction happens.
func RemoveMatching(root *html.Node, sel Selector) int {
	if root == nil || sel.IsEmpty() {
		return 0
	}
	var doomed []*html.Node
	Walk(root, func(node *html.Node) bool {
		if node != root && sel.Matches(node) {
			doomed = append(doomed, node)
			return false
		}
		return true
	})
	for _, node := range doomed {
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}
	return len(doomed)
}

// Ancestors returns the node's ancestor chain, nearest first.
func Ancestors(node *html.Node) []*html.Node {
	var chain []*html.Node
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode {
			chain = append(chain, parent)
		}
	}
	return chain
}

// DescriptorText gathers the class names, ids, titles and hrefs on and around a
// node. Region detection uses it to decide whether an audio link is British or
// American without depending on any single dictionary's conventions.
func DescriptorText(node *html.Node) string {
	var builder strings.Builder
	appendNode := func(n *html.Node) {
		if n == nil || n.Type != html.ElementNode {
			return
		}
		for _, attr := range n.Attr {
			switch strings.ToLower(attr.Key) {
			case "class", "id", "title", "href", "src", "addr", "name", "data-src-mp3", "alt":
				builder.WriteString(" ")
				builder.WriteString(attr.Val)
			}
		}
	}
	appendNode(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		appendNode(child)
	}
	ancestors := Ancestors(node)
	for i, ancestor := range ancestors {
		if i >= 3 {
			break
		}
		appendNode(ancestor)
	}
	// A block that prints its own "BrE" or "NAmE" label is stating the region
	// outright, and that statement outranks anything nearby.
	own := Text(node, TextOptions{SkipHidden: true})
	if len([]rune(own)) <= maxRegionLabelRunes {
		builder.WriteString(" ")
		builder.WriteString(own)
	}
	if node.PrevSibling != nil {
		appendNode(node.PrevSibling)
	}
	if node.NextSibling != nil {
		appendNode(node.NextSibling)
	}
	descriptor := strings.ToLower(builder.String())
	// Only when the node says nothing about itself is a neighbour's text worth
	// reading. Borrowing it unconditionally is how the American half of a
	// pronunciation block inherits the British label printed just above it.
	if node.PrevSibling != nil && !hasRegionMarker(descriptor) {
		descriptor += " " + strings.ToLower(Text(node.PrevSibling, TextOptions{}))
	}
	return descriptor
}

// maxRegionLabelRunes keeps a whole entry's prose out of the region evidence
// when the "node" turns out to be a large container.
const maxRegionLabelRunes = 120
