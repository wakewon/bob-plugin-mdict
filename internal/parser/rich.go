package parser

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// richBlocks extracts the intentionally small presentation vocabulary needed
// by Markdown. It is not an HTML renderer: CSS, layout, scripts, and arbitrary
// elements are ignored, while visible prose, resolved MDD images, and ordinary
// tables retain their document order.
func (s *parseState) richBlocks(root *html.Node) []entryir.RichBlock {
	if root == nil {
		return nil
	}
	var blocks []entryir.RichBlock
	var text strings.Builder
	flush := func() {
		value := Normalize(text.String())
		text.Reset()
		if value == "" {
			return
		}
		blocks = append(blocks, entryir.RichBlock{Kind: entryir.RichText, Text: value})
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		switch node.Type {
		case html.TextNode:
			text.WriteString(node.Data)
			text.WriteByte(' ')
			return
		case html.CommentNode, html.DoctypeNode:
			return
		case html.ElementNode:
			if hiddenByStyle(node) || node.Data == "script" || node.Data == "style" || node.Data == "head" {
				return
			}
			switch node.Data {
			case "img":
				ref := firstAttr(node, "src", "data-src")
				if ref == "" || s.opts.Image == nil {
					return
				}
				alt := firstNonEmpty(Attr(node, "alt"), Attr(node, "title"))
				if image := s.opts.Image.ResolveImage(ref, alt); image != nil {
					flush()
					blocks = append(blocks, entryir.RichBlock{Kind: entryir.RichImage, Image: image})
				}
				return
			case "table":
				rows := tableRows(node)
				if len(rows) > 0 {
					flush()
					blocks = append(blocks, entryir.RichBlock{Kind: entryir.RichTable, Rows: rows})
				}
				return
			case "br":
				text.WriteByte('\n')
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if node.Type == html.ElementNode && blockTags[node.Data] {
			text.WriteByte('\n')
		}
	}
	walk(root)
	flush()
	hasRich := false
	for _, block := range blocks {
		if block.Kind == entryir.RichImage || block.Kind == entryir.RichTable {
			hasRich = true
			break
		}
	}
	if !hasRich {
		return nil
	}
	// Publisher markup commonly contains the same illustration twice for two
	// switchable language views. Once hidden variants and CSS are unavailable,
	// identical adjacent resources are the strongest safe dedupe evidence.
	seenImages := map[string]bool{}
	kept := blocks[:0]
	for _, block := range blocks {
		if block.Kind == entryir.RichImage && block.Image != nil {
			key := block.Image.ResourceRef
			if seenImages[key] {
				continue
			}
			seenImages[key] = true
		}
		kept = append(kept, block)
	}
	return kept
}

func firstAttr(node *html.Node, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(Attr(node, name)); value != "" {
			return value
		}
	}
	return ""
}

func tableRows(table *html.Node) [][]string {
	var rows [][]string
	for _, row := range QueryAllNested(table, ParseSelector("tr")) {
		var cells []string
		for child := row.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.ElementNode || (child.Data != "th" && child.Data != "td") {
				continue
			}
			cells = append(cells, Normalize(Text(child, TextOptions{SkipHidden: true})))
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}
