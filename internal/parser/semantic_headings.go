package parser

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// parseGenericSemanticSections lifts clear secondary headings before sense
// parsing. It relies on visible/declared heading evidence plus the shared
// semantic vocabulary; it does not guess section roles from prose.
func (s *parseState) parseGenericSemanticSections() {
	if s.profile != nil {
		return
	}
	var headings []*html.Node
	Walk(s.doc, func(node *html.Node) bool {
		if node == s.doc {
			return true
		}
		title, _, ok := semanticHeading(node)
		if ok {
			if s.opts.Headword != "" && HeadwordMatchesKey(title, s.opts.Headword) {
				return false
			}
			headings = append(headings, node)
			return false
		}
		return true
	})
	for _, heading := range headings {
		if heading.Parent == nil {
			continue
		}
		title, role, _ := semanticHeading(heading)
		var content []*html.Node
		for sibling := heading.NextSibling; sibling != nil; sibling = sibling.NextSibling {
			if sibling.Type != html.ElementNode {
				continue
			}
			if _, _, boundary := semanticHeading(sibling); boundary || isSenseOrPOSBoundary(sibling) {
				break
			}
			if text := Normalize(s.textOf(sibling)); text != "" || len(s.richBlocks(sibling)) > 0 {
				content = append(content, sibling)
			}
		}
		if len(content) == 0 {
			continue
		}
		var values []string
		var blocks []entryir.RichBlock
		for _, node := range content {
			if node.Data == "ul" || node.Data == "ol" {
				for _, item := range QueryAll(node, ParseSelector("li")) {
					if text := Normalize(s.textOf(item)); text != "" {
						values = append(values, text)
					}
				}
			} else if text := Normalize(s.textOf(node)); text != "" {
				values = append(values, text)
			}
			blocks = append(blocks, s.richBlocks(node)...)
		}
		if len(values) == 0 && len(blocks) == 0 {
			continue
		}
		if role == "" || role == LabelExamples {
			s.entry.Sections = append(s.entry.Sections, entryir.Section{
				Title: title, Body: strings.Join(values, "\n"), Blocks: blocks,
			})
		} else {
			s.storeSemanticValues(role, values)
		}
		for _, node := range content {
			if node.Parent != nil {
				node.Parent.RemoveChild(node)
			}
		}
		if heading.Parent != nil {
			heading.Parent.RemoveChild(heading)
		}
	}
}

// semanticHeading recognises a short standalone heading. Semantic labels may
// use ordinary inline elements, but then need uppercase or descriptor evidence;
// h2-h6 are explicit headings and are preserved even when their role is new.
func semanticHeading(node *html.Node) (string, SemanticLabel, bool) {
	if node == nil || node.Type != html.ElementNode {
		return "", "", false
	}
	text := strings.Trim(Normalize(Text(node, TextOptions{SkipHidden: true})), " :：")
	if text == "" || len([]rune(text)) > 50 {
		return "", "", false
	}
	tagHeading := node.Data == "h2" || node.Data == "h3" || node.Data == "h4" || node.Data == "h5" || node.Data == "h6"
	role := ClassifySemanticLabel(text)
	if !tagHeading && role == "" {
		return "", "", false
	}
	if !tagHeading {
		descriptor := Attr(node, "class") + " " + Attr(node, "id") + " " + Attr(node, "title")
		descriptorRole := ClassifySemanticLabel(descriptor)
		if !isUpperHeading(text) && descriptorRole == "" && !strings.Contains(strings.ToLower(descriptor), "head") &&
			node.Data != "strong" && node.Data != "b" {
			return "", "", false
		}
	}
	return text, role, true
}

func isUpperHeading(text string) bool {
	letters := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsLower(r) {
			return false
		}
	}
	return letters > 0
}

func isSenseOrPOSBoundary(node *html.Node) bool {
	if classMatchesHint(node, senseClassHints) || classMatchesHint(node, posClassHints) {
		return true
	}
	if isTextLeaf(node) && CanonicalPOS(Text(node, TextOptions{SkipHidden: true})) != "" {
		return true
	}
	return false
}
