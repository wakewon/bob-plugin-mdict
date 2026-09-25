// Package htmlmd renders a dictionary record's own HTML as Markdown.
//
// It is the one presentation that reads entry HTML rather than the Entry IR:
//
//	EntrySet → bobadapter / textrender / mdrender
//	record HTML + dictionary CSS → htmlmd → Markdown
//
// The IR presentations show what the parser understood, in a layout this
// project chose. This one shows the page the dictionary's editors laid out,
// which is what a reader of a richly typeset dictionary usually wants once Bob
// can draw Markdown. The two answer different questions, so neither replaces
// the other and neither reads the other's output.
//
// The general HTML-to-Markdown work — whitespace collapsing, escaping, lists,
// tables, emphasis — is done by github.com/JohannesKaufmann/html-to-markdown.
// What this package adds is what a generic converter cannot know about MDict
// records: that layout lives in the stylesheet rather than in the tags, that
// `sound://` and `entry://` are not web links, and that images and audio live
// in the dictionary's MDD.
package htmlmd

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Resources resolves what a record refers to. Every method may return an
// empty value, which means "not available": the reference is then dropped
// rather than emitted as a link that would not work.
type Resources interface {
	// Stylesheet returns the parsed stylesheet a <link> names, or nil.
	Stylesheet(href string) *Stylesheet
	// Audio returns a playable URL for a sound reference.
	Audio(ref string) string
	// Image returns a displayable URL for an image reference.
	Image(ref string) string
}

// Options configures one conversion.
type Options struct {
	Resources Resources
	// Stylesheets apply after the record's own, so they win ties. A
	// dictionary profile uses them to hide interface chrome.
	Stylesheets []*Stylesheet
	// Scope narrows the parsed document before conversion — a dictionary
	// profile's entry root. It receives the document after the cascade and
	// returns the node to convert.
	Scope func(doc *html.Node) *html.Node
	// LookupLink turns a word a dictionary links to into a URL that looks it
	// up in Bob, or returns "" when it cannot. Nil, or "", leaves the link as
	// plain text.
	LookupLink func(query string) string
}

// Convert renders one record. It never fails: a record the converter cannot
// make sense of comes out as its visible text.
func Convert(raw []byte, opts Options) string {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var sheets []*Stylesheet
	walk(doc, func(node *html.Node) bool {
		switch {
		case isStylesheetLink(node):
			if opts.Resources != nil {
				sheets = append(sheets, opts.Resources.Stylesheet(attr(node, "href")))
			}
		case isElement(node, "style"):
			if node.FirstChild != nil {
				sheets = append(sheets, ParseStylesheet([]byte(node.FirstChild.Data)))
			}
		}
		return true
	})
	sheets = append(sheets, opts.Stylesheets...)
	computed := cascade(doc, sheets)

	root := doc
	if opts.Scope != nil {
		if scoped := opts.Scope(doc); scoped != nil {
			root = scoped
		}
	}
	c := &conversion{styles: computed, resources: opts.Resources, lookupLink: opts.LookupLink}
	c.prepare(root)
	return c.render(root)
}

func isStylesheetLink(node *html.Node) bool {
	return isElement(node, "link") && strings.Contains(strings.ToLower(attr(node, "rel")), "stylesheet")
}

// StylesheetLinks calls visit with the href of every stylesheet a document
// links to, in document order.
func StylesheetLinks(doc *html.Node, visit func(href string)) {
	walk(doc, func(node *html.Node) bool {
		if isStylesheetLink(node) {
			visit(attr(node, "href"))
		}
		return true
	})
}

type conversion struct {
	styles     *styles
	resources  Resources
	lookupLink func(query string) string
}

// chrome is never content, whatever the stylesheet says.
var chrome = map[string]bool{
	"script": true, "style": true, "link": true, "meta": true, "head": true,
	"noscript": true, "template": true, "iframe": true, "object": true,
	"embed": true, "button": true, "input": true, "select": true,
	"textarea": true, "form": true, "svg": true, "canvas": true,
}

// prepare rewrites the DOM into the plain HTML a generic converter reads
// correctly. Order matters: hidden subtrees go first so nothing is generated
// into them, and layout is rewritten before emphasis so emphasis can see the
// final block structure.
func (c *conversion) prepare(root *html.Node) {
	var remove []*html.Node
	walk(root, func(node *html.Node) bool {
		switch node.Type {
		case html.CommentNode:
			remove = append(remove, node)
			return false
		case html.ElementNode:
		default:
			return true
		}
		if chrome[node.Data] || c.hidden(node) {
			remove = append(remove, node)
			return false
		}
		return true
	})
	remove = append(remove, c.invisible(root)...)
	removed := make(map[*html.Node]bool, len(remove))
	for _, node := range remove {
		removed[node] = true
	}
	for _, node := range remove {
		// A hidden label between two words is often the only thing keeping
		// them apart on the page, where the stylesheet puts a margin around
		// it. Removing it outright would print "abandon shipto leave". A
		// hidden syllable dot, by contrast, is punctuation inside one word
		// and must leave nothing behind: "a·ban·don" reads "abandon".
		if (node.Type == html.ElementNode && !chrome[node.Data] && !blockTags[node.Data] ||
			node.Type == html.TextNode) &&
			separatesWords(node) &&
			wordBoundary(adjacentRune(node, true, removed), adjacentRune(node, false, removed)) {
			node.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, node)
		}
		detach(node)
	}

	c.normalizeSpaces(root)
	c.insertGeneratedContent(root)
	c.rewriteResources(root)
	c.applyDisplay(root)
	separateAdjacentWords(root)
	c.applyListStyle(root)
	flattenLayoutTables(root)
	c.applyEmphasis(root, false, false)
}

// layoutSpaceRe matches the runs of no-break and typographic spaces that
// dictionaries use as padding. A converter must keep a no-break space, so
// left alone they survive as visible gaps and as indentation at line starts.
var layoutSpaceRe = regexp.MustCompile(`[\x{00a0}\x{2002}-\x{200a}\x{202f}]+`)

// normalizeSpaces also drops icon-font glyphs written straight into the
// text. Like the ones in generated content, they are Private Use Area code
// points that only the dictionary's own font can draw.
func (c *conversion) normalizeSpaces(root *html.Node) {
	walk(root, func(node *html.Node) bool {
		if node.Type == html.TextNode {
			node.Data = visibleContent(layoutSpaceRe.ReplaceAllString(node.Data, " "))
		}
		return node.Type != html.ElementNode || (node.Data != "pre" && node.Data != "code")
	})
}

// hidden reports whether an element and everything in it is not rendered.
// Only display does that: visibility is inherited, and a descendant can set
// it back to visible, so it is handled by invisible instead.
func (c *conversion) hidden(node *html.Node) bool {
	style := c.styles.of(node)
	return style != nil && style["display"] == "none"
}

// invisible returns the text and images inside root that inherit, or set,
// visibility hidden without a nearer ancestor setting it back to visible.
// Their elements stay, because a descendant may be visible again.
func (c *conversion) invisible(root *html.Node) []*html.Node {
	var out []*html.Node
	var visit func(node *html.Node, hidden bool)
	visit = func(node *html.Node, hidden bool) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			switch child.Type {
			case html.TextNode:
				if hidden {
					out = append(out, child)
				}
			case html.ElementNode:
				childHidden := hidden
				if style := c.styles.of(child); style != nil {
					switch style["visibility"] {
					case "hidden", "collapse":
						childHidden = true
					case "visible":
						childHidden = false
					}
				}
				if childHidden && voidElements[child.Data] {
					out = append(out, child)
					continue
				}
				visit(child, childHidden)
			}
		}
	}
	visit(root, false)
	return out
}

// insertGeneratedContent materialises ::before and ::after strings as text.
// Dictionaries use them for real content surprisingly often — the slashes
// around an IPA transcription, the quotes around a citation — and a reader
// without them sees `həˈləʊ` where the page shows `/həˈləʊ/`.
func (c *conversion) insertGeneratedContent(root *html.Node) {
	var elements []*html.Node
	walk(root, func(node *html.Node) bool {
		if node.Type == html.ElementNode {
			elements = append(elements, node)
		}
		return true
	})
	for _, node := range elements {
		if voidElements[node.Data] {
			continue
		}
		// Generated text hugs the content, as it does on the page: a trailing
		// space in "həˈləʊ " would otherwise print "/həˈləʊ /".
		if text, ok := generated(c.styles.before[node]); ok {
			if first := edgeText(node, false); first != nil {
				trimmed := strings.TrimLeftFunc(first.Data, unicode.IsSpace)
				text = first.Data[:len(first.Data)-len(trimmed)] + text
				first.Data = trimmed
			}
			node.InsertBefore(&html.Node{Type: html.TextNode, Data: text}, node.FirstChild)
		}
		if text, ok := generated(c.styles.after[node]); ok {
			if last := edgeText(node, true); last != nil {
				trimmed := strings.TrimRightFunc(last.Data, unicode.IsSpace)
				text += last.Data[len(trimmed):]
				last.Data = trimmed
			}
			node.AppendChild(&html.Node{Type: html.TextNode, Data: text})
		}
	}
}

func generated(style computedStyle) (string, bool) {
	if style == nil || style["display"] == "none" {
		return "", false
	}
	// Floated and absolutely positioned generated content sits outside the
	// text — a "TRANS" toggle badge, a "+" expander. Read inline, it would
	// be inserted into the middle of a sentence.
	if float := style["float"]; float == "left" || float == "right" {
		return "", false
	}
	if position := style["position"]; position == "absolute" || position == "fixed" {
		return "", false
	}
	text, ok := style["content"]
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

var voidElements = map[string]bool{
	"img": true, "br": true, "hr": true, "wbr": true, "area": true,
	"base": true, "col": true, "source": true, "track": true,
}

// rewriteResources turns MDict addressing into Markdown a reader can use.
//
//   - `sound://` links, and <audio> sources, become a 🔊 link to the resolved
//     recording, placed after whatever the dictionary printed there. Speaker
//     icons inside the link are dropped: the 🔊 is their replacement.
//   - `entry://` and `bword://` links become lookup links when
//     Options.LookupLink provides them, and text otherwise; in-page anchors
//     and script links are unwrapped to their text, since a link that goes
//     nowhere is worse than text.
//   - Images are shown only when the dictionary's MDD holds them; remote and
//     inline data images are dropped, so rendering a record never fetches
//     anything from the network.
func (c *conversion) rewriteResources(root *html.Node) {
	var anchors, images, media []*html.Node
	walk(root, func(node *html.Node) bool {
		switch {
		case isElement(node, "a"):
			anchors = append(anchors, node)
		case isElement(node, "img"):
			images = append(images, node)
		case isElement(node, "audio"), isElement(node, "video"):
			media = append(media, node)
		}
		return true
	})
	for _, node := range anchors {
		c.rewriteAnchor(node)
	}
	for _, node := range images {
		if node.Parent == nil {
			continue
		}
		url := ""
		if c.resources != nil {
			url = c.resources.Image(firstAttr(node, "src", "data-src"))
		}
		if url == "" {
			detach(node)
			continue
		}
		setAttr(node, "src", url)
	}
	for _, node := range media {
		ref := attr(node, "src")
		if ref == "" {
			walk(node, func(child *html.Node) bool {
				if ref == "" && isElement(child, "source") {
					ref = attr(child, "src")
				}
				return ref == ""
			})
		}
		replaceWith(node, c.audioLink(ref, regionLabel(node, ref)))
	}
}

func (c *conversion) rewriteAnchor(node *html.Node) {
	href := strings.TrimSpace(firstAttr(node, "href", "addr", "data-src-mp3"))
	lower := strings.ToLower(href)
	switch {
	case strings.HasPrefix(lower, "sound://") || (href != "" && isAudioPath(lower)):
		// The printed text inside a pronunciation link — often the IPA itself
		// — stays; the link is added after it. A link with no text is a
		// speaker icon, and whatever it holds, a blank or an <img>, is
		// replaced by the 🔊. The region is read first, while the icons that
		// often carry it are still there.
		label := regionLabel(node, href)
		if !hasText(node) {
			for node.FirstChild != nil {
				node.RemoveChild(node.FirstChild)
			}
		}
		walk(node, func(child *html.Node) bool {
			if isElement(child, "img") {
				defer detach(child)
				return false
			}
			return true
		})
		// The element stays, as a span, so the stylesheet's layout for it —
		// one pronunciation per line, say — still applies.
		rename(node, "span")
		node.Attr = nil
		if link := c.audioLink(href, label); link != nil {
			node.AppendChild(link)
		}
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		// An ordinary web link the dictionary chose to print.
		node.Attr = []html.Attribute{{Key: "href", Val: href}}
	case strings.HasPrefix(lower, "entry://") || strings.HasPrefix(lower, "bword://"):
		// A link to another entry: a lookup in Bob when that is available,
		// otherwise its text.
		link := ""
		if c.lookupLink != nil && hasText(node) {
			link = c.lookupLink(entryTarget(href))
		}
		if link == "" {
			unwrap(node, nil)
			return
		}
		node.Attr = []html.Attribute{{Key: "href", Val: link}}
		// "[, dummy run](…)" puts the separator inside the link.
		peelPunctuation(node)
	default:
		unwrap(node, nil)
	}
}

// entryTarget is the headword an `entry://` or `bword://` link names: the
// scheme and any in-page fragment removed, percent-encoding decoded. A link
// to an anchor in the same record has no target.
func entryTarget(href string) string {
	target := href[strings.Index(href, "://")+3:]
	if index := strings.IndexByte(target, '#'); index >= 0 {
		target = target[:index]
	}
	if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}
	return strings.TrimSpace(target)
}

// regionLabel is "UK" or "US" when a recording's own markup says which it is
// — its classes, its file path, the icons inside it, a short printed label —
// and "" otherwise. Neighbouring elements are not consulted: in a run of
// pronunciation links, each would borrow the others' regions.
func regionLabel(node *html.Node, ref string) string {
	var descriptor strings.Builder
	descriptor.WriteString(ref)
	walk(node, func(child *html.Node) bool {
		if child.Type != html.ElementNode {
			return true
		}
		for _, a := range child.Attr {
			switch strings.ToLower(a.Key) {
			case "class", "id", "title", "href", "src", "addr", "data-src-mp3", "alt":
				descriptor.WriteString(" ")
				descriptor.WriteString(a.Val)
			}
		}
		return true
	})
	if text := textOf(node); len([]rune(text)) <= 20 {
		descriptor.WriteString(" ")
		descriptor.WriteString(text)
	}
	switch parser.DetectRegion(descriptor.String()) {
	case entryir.RegionUK:
		return "UK"
	case entryir.RegionUS:
		return "US"
	}
	return ""
}

func textOf(node *html.Node) string {
	var builder strings.Builder
	walk(node, func(child *html.Node) bool {
		if child.Type == html.TextNode {
			builder.WriteString(child.Data)
		}
		return true
	})
	return strings.TrimSpace(builder.String())
}

// audioLink returns a 🔊 link element for a resolvable recording, labelled
// with its region when that is known, or nil.
func (c *conversion) audioLink(ref, region string) *html.Node {
	if c.resources == nil || strings.TrimSpace(ref) == "" {
		return nil
	}
	url := c.resources.Audio(ref)
	if url == "" {
		return nil
	}
	link := &html.Node{Type: html.ElementNode, Data: "a", DataAtom: atom.A,
		Attr: []html.Attribute{{Key: "href", Val: url}}}
	text := "🔊"
	if region != "" {
		text += " " + region
	}
	link.AppendChild(&html.Node{Type: html.TextNode, Data: text})
	// The icon it replaces was spaced by the stylesheet; a bare link would
	// run into the transcription before it and the example after it.
	group := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span}
	group.AppendChild(&html.Node{Type: html.TextNode, Data: " "})
	group.AppendChild(link)
	group.AppendChild(&html.Node{Type: html.TextNode, Data: " "})
	return group
}

var audioPathRe = regexp.MustCompile(`\.(mp3|wav|ogg|spx|m4a|aac|flac)([?#].*)?$`)

func isAudioPath(lower string) bool { return audioPathRe.MatchString(lower) }

// Display classes. What matters for Markdown is only whether an element starts
// a new block; everything that lays children out side by side is inline.
var blockDisplays = map[string]bool{
	"block": true, "list-item": true, "table": true, "table-row": true,
	"table-row-group": true, "table-header-group": true, "table-footer-group": true,
	"table-caption": true, "flex": true, "grid": true, "flow-root": true,
}

var inlineDisplays = map[string]bool{
	"inline": true, "inline-block": true, "inline-flex": true, "inline-grid": true,
	"inline-table": true, "table-cell": true, "contents": true, "ruby": true,
}

// genericInline tags carry no meaning a converter uses, so renaming one to
// <div> loses nothing. Semantic inline tags are wrapped instead.
var genericInline = map[string]bool{
	"span": true, "font": true, "label": true, "small": true, "big": true,
	"abbr": true, "cite": true, "dfn": true, "var": true, "samp": true,
	"kbd": true, "q": true, "time": true, "data": true, "bdi": true, "bdo": true,
	"mark": true, "ins": true, "nobr": true, "tt": true,
}

// flowBlocks can become <span> without losing anything a converter reads.
var flowBlocks = map[string]bool{
	"div": true, "p": true, "section": true, "article": true, "header": true,
	"footer": true, "aside": true, "main": true, "nav": true, "center": true,
	"address": true, "figure": true, "figcaption": true, "dt": true, "dd": true,
	"dl": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// knownInline are the inline tags html-to-markdown treats as such; unknown
// custom tags that dictionaries invent default to block in the converter,
// which is wrong for most of them.
var knownInline = map[string]bool{
	"a": true, "b": true, "strong": true, "i": true, "em": true, "u": true,
	"s": true, "strike": true, "del": true, "sup": true, "sub": true, "code": true,
	"img": true, "br": true,
}

// applyDisplay makes each element's tag agree with the display its
// stylesheet gives it, so the converter's tag-based layout matches the page.
func (c *conversion) applyDisplay(root *html.Node) {
	var elements []*html.Node
	walk(root, func(node *html.Node) bool {
		if node.Type == html.ElementNode && node != root {
			elements = append(elements, node)
		}
		return true
	})
	for _, node := range elements {
		display := ""
		if style := c.styles.of(node); style != nil {
			if fields := strings.Fields(style["display"]); len(fields) > 0 {
				display = fields[0]
			}
		}
		tag := node.Data
		custom := node.DataAtom == 0 && !knownInline[tag]
		switch {
		case blockDisplays[display]:
			if genericInline[tag] || custom {
				rename(node, "div")
			} else if knownInline[tag] && tag != "br" && tag != "img" {
				wrap(node, "div")
			}
		case inlineDisplays[display]:
			if flowBlocks[tag] || custom {
				rename(node, "span")
			}
		case custom:
			// No display declared: a custom element is inline by default in
			// a browser.
			rename(node, "span")
		}
		c.applySpacing(node)
	}
}

// applySpacing turns the horizontal margin or padding of an inline element
// into a space. Dictionaries separate a part of speech from the next one, or
// an inflection from its neighbour, with margins alone; without them two
// adjacent labels read as one word, and two adjacent bold runs collapse into
// broken Markdown.
func (c *conversion) applySpacing(node *html.Node) {
	style := c.styles.of(node)
	if style == nil || node.Parent == nil || blockTags[node.Data] || !hasText(node) {
		return
	}
	if positiveLength(style["margin-left"]) || positiveLength(style["padding-left"]) {
		node.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, node)
	}
	if positiveLength(style["margin-right"]) || positiveLength(style["padding-right"]) {
		node.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, node.NextSibling)
	}
}

// applyEmphasis wraps text a stylesheet bolds or italicises. It only applies
// emphasis where it starts — not where it is inherited — and only to elements
// whose content is inline, because Markdown emphasis cannot span blocks.
func (c *conversion) applyEmphasis(node *html.Node, bold, italic bool) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		childBold, childItalic := bold, italic
		switch child.Data {
		case "b", "strong", "h1", "h2", "h3", "h4", "h5", "h6", "th":
			childBold = true
		case "i", "em":
			childItalic = true
		}
		style := c.styles.of(child)
		wantBold := !childBold && style != nil && isBold(style["font-weight"])
		wantItalic := !childItalic && style != nil && isItalic(style["font-style"])
		if (wantBold || wantItalic) && hasText(child) && !hasBlock(child) && child.Data != "a" {
			if wantBold {
				peelPunctuation(wrapChildren(child, "strong"))
				childBold = true
			}
			if wantItalic {
				peelPunctuation(wrapChildren(child, "em"))
				childItalic = true
			}
		}
		c.applyEmphasis(child, childBold, childItalic)
	}
}

func isBold(weight string) bool {
	switch weight {
	case "bold", "bolder", "600", "700", "800", "900":
		return true
	}
	return false
}

func isItalic(style string) bool {
	return style == "italic" || strings.HasPrefix(style, "oblique")
}

// blockTags are the tags the converter lays out as blocks.
var blockTags = map[string]bool{
	"div": true, "p": true, "ul": true, "ol": true, "li": true, "table": true,
	"tr": true, "td": true, "th": true, "blockquote": true, "pre": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "header": true, "footer": true, "dl": true,
	"dt": true, "dd": true, "figure": true, "center": true, "br": true,
}

func hasBlock(node *html.Node) bool {
	found := false
	walk(node, func(child *html.Node) bool {
		if child != node && child.Type == html.ElementNode && blockTags[child.Data] {
			found = true
		}
		return !found
	})
	return found
}

func hasText(node *html.Node) bool {
	found := false
	walk(node, func(child *html.Node) bool {
		if child.Type == html.TextNode && strings.TrimSpace(child.Data) != "" {
			found = true
		}
		return !found
	})
	return found
}

var markdownConverter = converter.NewConverter(
	converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(
			commonmark.WithBulletListMarker("-"),
			commonmark.WithLinkEmptyContentBehavior(commonmark.LinkBehaviorSkip),
			// The separator comment between adjacent lists would be shown
			// verbatim by renderers that do not hide HTML.
			commonmark.WithListEndComment(false),
		),
		strikethrough.NewStrikethroughPlugin(),
		table.NewTablePlugin(
			table.WithSkipEmptyRows(true),
			table.WithHeaderPromotion(true),
			table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
			table.WithSpanCellBehavior(table.SpanBehaviorEmpty),
		),
	),
)

var blankLinesRe = regexp.MustCompile(`\n{3,}`)

// adjacentBoldRe finds two bold runs that touch, which CommonMark cannot
// parse as two runs. The page shows them as one bold stretch — "abandon" and
// its homograph number — so they are merged into one.
var adjacentBoldRe = regexp.MustCompile(`(\S)\*\*\*\*(\S)`)

func (c *conversion) render(root *html.Node) string {
	markdown, err := markdownConverter.ConvertNode(root)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(markdown), "\n")
	for index, line := range lines {
		trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
		// Two trailing spaces are a <br>, and a line break inside a
		// paragraph is content; at the end of a paragraph it is noise.
		if strings.HasSuffix(line, "  ") && trimmed != "" && index+1 < len(lines) && strings.TrimSpace(lines[index+1]) != "" {
			trimmed += "  "
		}
		lines[index] = trimmed
	}
	text := adjacentBoldRe.ReplaceAllString(strings.Join(lines, "\n"), "$1$2")
	text = blankLinesRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func walk(node *html.Node, visit func(*html.Node) bool) {
	if node == nil || !visit(node) {
		return
	}
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		walk(child, visit)
		child = next
	}
}

func isElement(node *html.Node, tag string) bool {
	return node.Type == html.ElementNode && node.Data == tag
}

func firstAttr(node *html.Node, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(attr(node, name)); value != "" {
			return value
		}
	}
	return ""
}

func setAttr(node *html.Node, key, value string) {
	for index := range node.Attr {
		if strings.EqualFold(node.Attr[index].Key, key) {
			node.Attr[index].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}

func detach(node *html.Node) {
	if node.Parent != nil {
		node.Parent.RemoveChild(node)
	}
}

func rename(node *html.Node, tag string) {
	node.Data = tag
	node.DataAtom = atom.Lookup([]byte(tag))
}

// wrap puts node inside a new element in its place.
func wrap(node *html.Node, tag string) {
	if node.Parent == nil {
		return
	}
	wrapper := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
	node.Parent.InsertBefore(wrapper, node)
	node.Parent.RemoveChild(node)
	wrapper.AppendChild(node)
}

// wrapChildren moves node's children into one new child element.
func wrapChildren(node *html.Node, tag string) *html.Node {
	wrapper := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
	for node.FirstChild != nil {
		child := node.FirstChild
		node.RemoveChild(child)
		wrapper.AppendChild(child)
	}
	node.AppendChild(wrapper)
	return wrapper
}

// peelPunctuation moves punctuation and spaces at the edges of an emphasis
// element outside it. CommonMark cannot open emphasis that starts with
// punctuation straight after a letter, so a bold label printed as ", → cut"
// after "running1" would come out as literal asterisks and unbalance the rest
// of the line. The punctuation was never the emphasised part anyway.
func peelPunctuation(wrapper *html.Node) {
	if text := edgeText(wrapper, false); text != nil {
		trimmed := strings.TrimLeftFunc(text.Data, isEdgeRune)
		if peeled := text.Data[:len(text.Data)-len(trimmed)]; peeled != "" {
			wrapper.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: peeled}, wrapper)
			text.Data = trimmed
		}
	}
	if text := edgeText(wrapper, true); text != nil {
		trimmed := strings.TrimRightFunc(text.Data, isEdgeRune)
		if peeled := text.Data[len(trimmed):]; peeled != "" {
			wrapper.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: peeled}, wrapper.NextSibling)
			text.Data = trimmed
		}
	}
	if !hasText(wrapper) {
		unwrap(wrapper, nil)
	}
}

// edgeText returns the first (or last) non-empty text node inside node,
// looking through inline wrappers, or nil when an image or a block comes
// first — moving text past either would change what it sits beside.
func edgeText(node *html.Node, last bool) *html.Node {
	child := node.FirstChild
	if last {
		child = node.LastChild
	}
	for ; child != nil; child = nextOf(child, last) {
		switch child.Type {
		case html.TextNode:
			if child.Data != "" {
				return child
			}
		case html.ElementNode:
			if voidElements[child.Data] || blockTags[child.Data] {
				return nil
			}
			if found := edgeText(child, last); found != nil {
				return found
			}
			if hasText(child) {
				return nil
			}
		}
	}
	return nil
}

// isEdgeRune is punctuation that emphasis should not start or end on. Closing
// brackets and quotes are kept inside, like the words they belong to.
func isEdgeRune(r rune) bool {
	return unicode.IsSpace(r) || r == ',' || r == ';' || r == ':' || r == '→' || r == '•' || r == '·'
}

// unwrap replaces node with its children, followed by after when non-nil.
func unwrap(node *html.Node, after *html.Node) {
	parent := node.Parent
	if parent == nil {
		return
	}
	for node.FirstChild != nil {
		child := node.FirstChild
		node.RemoveChild(child)
		parent.InsertBefore(child, node)
	}
	if after != nil {
		parent.InsertBefore(after, node)
	}
	parent.RemoveChild(node)
}

func replaceWith(node *html.Node, replacement *html.Node) {
	if node.Parent == nil {
		return
	}
	if replacement != nil {
		node.Parent.InsertBefore(replacement, node)
	}
	node.Parent.RemoveChild(node)
}

// adjacentRune returns the visible character next to node in reading order —
// before it when backward is set — without crossing into another block. It
// returns 0 when there is none.
func adjacentRune(node *html.Node, backward bool, skip map[*html.Node]bool) rune {
	for current := node; current != nil; current = current.Parent {
		if current != node && current.Type == html.ElementNode && blockTags[current.Data] {
			return 0
		}
		sibling := current.NextSibling
		if backward {
			sibling = current.PrevSibling
		}
		for ; sibling != nil; sibling = nextOf(sibling, backward) {
			if sibling.Type == html.ElementNode && blockTags[sibling.Data] {
				return 0
			}
			if r := edgeRune(sibling, backward, skip); r != 0 {
				return r
			}
		}
	}
	return 0
}

func nextOf(node *html.Node, backward bool) *html.Node {
	if backward {
		return node.PrevSibling
	}
	return node.NextSibling
}

// edgeRune is the last (backward) or first visible character inside node.
func edgeRune(node *html.Node, last bool, skip map[*html.Node]bool) rune {
	if skip[node] || (node.Type == html.ElementNode && chrome[node.Data]) {
		return 0
	}
	if node.Type == html.TextNode {
		if node.Data == "" {
			return 0
		}
		if last {
			r, _ := utf8.DecodeLastRuneInString(node.Data)
			return r
		}
		r, _ := utf8.DecodeRuneInString(node.Data)
		return r
	}
	child := node.FirstChild
	if last {
		child = node.LastChild
	}
	for ; child != nil; child = nextOf(child, last) {
		if r := edgeRune(child, last, skip); r != 0 {
			return r
		}
	}
	return 0
}

// separatesWords reports whether a hidden element held something that stood
// between words — a label, or only whitespace — rather than punctuation that
// belongs inside one.
func separatesWords(node *html.Node) bool {
	text := ""
	walk(node, func(child *html.Node) bool {
		if child.Type == html.TextNode {
			text += child.Data
		}
		return true
	})
	if strings.TrimSpace(text) == "" {
		return true
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// wordBoundary reports whether two characters would merge into one word
// without a space between them. Punctuation stays attached, and CJK needs no
// spaces.
func wordBoundary(left, right rune) bool {
	if left == 0 || right == 0 {
		return false
	}
	if !(unicode.IsLetter(left) || unicode.IsDigit(left)) || !(unicode.IsLetter(right) || unicode.IsDigit(right)) {
		return false
	}
	cjk := func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
	}
	return !cjk(left) && !cjk(right)
}

// applyListStyle removes list markers the page does not show. Dictionaries
// commonly reset lists with `list-style: none` and print their own sense
// numbers; kept as a Markdown list, every one-item list would be numbered
// "1." beside the dictionary's own "2".
func (c *conversion) applyListStyle(root *html.Node) {
	var lists []*html.Node
	walk(root, func(node *html.Node) bool {
		if isElement(node, "ul") || isElement(node, "ol") {
			lists = append(lists, node)
		}
		return true
	})
	for _, list := range lists {
		style := c.styles.of(list)
		if style == nil || !(hasWord(style["list-style-type"], "none") || hasWord(style["list-style"], "none")) {
			continue
		}
		for child := list.FirstChild; child != nil; child = child.NextSibling {
			if isElement(child, "li") {
				rename(child, "div")
			}
		}
		rename(list, "div")
	}
}

func hasWord(value, word string) bool {
	for _, field := range strings.Fields(value) {
		if field == word {
			return true
		}
	}
	return false
}

// flattenLayoutTables turns tables used for layout into lines of text.
//
// Dictionaries converted from web pages position a headword and a star
// rating, or a sub-sense label and its definition, with a table. As Markdown
// tables those come out as grids of empty cells under a blank header. Only a
// table that reads as data — a declared header row, or a full grid of simple
// cells — stays a table.
func flattenLayoutTables(root *html.Node) {
	var tables []*html.Node
	walk(root, func(node *html.Node) bool {
		if isElement(node, "table") {
			tables = append(tables, node)
		}
		return true
	})
	// Innermost first, so an outer table sees its nested tables already
	// flattened and judges only its own cells.
	for index := len(tables) - 1; index >= 0; index-- {
		table := tables[index]
		if isDataTable(table) {
			continue
		}
		walk(table, func(node *html.Node) bool {
			if node.Type != html.ElementNode {
				return true
			}
			switch node.Data {
			case "table", "tbody", "thead", "tfoot", "tr", "caption":
				rename(node, "div")
			case "td", "th":
				rename(node, "span")
				if node.Parent != nil {
					node.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, node.NextSibling)
				}
			}
			return true
		})
	}
}

func isDataTable(table *html.Node) bool {
	if strings.EqualFold(attr(table, "role"), "presentation") {
		return false
	}
	var rows [][]*html.Node
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			switch {
			case isElement(child, "tr"):
				var cells []*html.Node
				for cell := child.FirstChild; cell != nil; cell = cell.NextSibling {
					if isElement(cell, "td") || isElement(cell, "th") {
						cells = append(cells, cell)
					}
				}
				rows = append(rows, cells)
			case isElement(child, "thead"), isElement(child, "tbody"), isElement(child, "tfoot"):
				collect(child)
			}
		}
	}
	collect(table)
	if len(rows) == 0 {
		return false
	}
	columns := len(rows[0])
	header := false
	for _, row := range rows {
		for _, cell := range row {
			if cell.Data == "th" {
				header = true
			}
			if hasBlock(cell) {
				return false
			}
		}
	}
	if columns < 2 {
		return false
	}
	if header {
		return true
	}
	if len(rows) < 2 {
		return false
	}
	for _, row := range rows {
		if len(row) != columns {
			return false
		}
		for _, cell := range row {
			if !hasText(cell) {
				return false
			}
		}
	}
	return true
}

// separateAdjacentWords puts a space between two neighbouring inline elements
// whose text would otherwise merge into one word — a part of speech and the
// label after it, a sense number and a register label. On the page a margin
// keeps them apart; when the stylesheet is missing, or spaces them in a way
// this converter does not read, Markdown would print "nounPlural".
//
// Only element boundaries are considered. Text running into an element is
// usually one word split for styling — the stressed syllable of "hel|oʊ" —
// and is left alone.
func separateAdjacentWords(root *html.Node) {
	var pairs [][2]*html.Node
	walk(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode || blockTags[node.Data] || voidElements[node.Data] {
			return true
		}
		next := node.NextSibling
		if next != nil && next.Type == html.ElementNode && !blockTags[next.Data] && !voidElements[next.Data] {
			pairs = append(pairs, [2]*html.Node{node, next})
		}
		return true
	})
	for _, pair := range pairs {
		left, right := edgeRune(pair[0], true, nil), edgeRune(pair[1], false, nil)
		// A number straight after a word is a homograph or note mark: run¹.
		if unicode.IsLetter(left) && unicode.IsDigit(right) {
			continue
		}
		if wordBoundary(left, right) && pair[0].Parent != nil {
			pair[0].Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, pair[1])
		}
	}
}
