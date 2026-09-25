package htmlmd

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"

	"github.com/andybalholm/cascadia"
	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
	"golang.org/x/net/html"
)

// Stylesheet is the part of a dictionary stylesheet that changes what a
// Markdown reader should see.
//
// Dictionary HTML says almost nothing through its tags: a sense, an example
// and a translation are all <span>s, and the stylesheet is what turns them
// into separate lines, hides the language variant the reader did not pick,
// and bolds a label. Only the properties that decide that survive parsing —
// display, visibility, font weight and style, horizontal spacing, and the text
// of ::before and ::after with enough positioning to tell decoration from
// content. Colour, size and the rest of layout have no Markdown equivalent.
type Stylesheet struct {
	rules []rule
}

type rule struct {
	sel    cascadia.Sel
	pseudo string
	spec   cascadia.Specificity
	// key is the class, id or tag the selector's subject must carry, used to
	// avoid testing every rule against every element.
	key   selectorKey
	decls []declaration
}

type declaration struct {
	property  string
	value     string
	important bool
}

type selectorKind uint8

const (
	keyUniversal selectorKind = iota
	keyClass
	keyID
	keyTag
)

type selectorKey struct {
	kind selectorKind
	name string
}

// properties are the only declarations kept. Everything else is presentation
// Markdown has no way to express.
var properties = map[string]bool{
	"display":         true,
	"visibility":      true,
	"font-weight":     true,
	"font-style":      true,
	"content":         true,
	"float":           true,
	"position":        true,
	"margin-left":     true,
	"margin-right":    true,
	"padding-left":    true,
	"padding-right":   true,
	"list-style":      true,
	"list-style-type": true,
}

// boxShorthands expand into the horizontal longhands above. Vertical spacing
// is irrelevant: whether an element starts a line is display's business.
var boxShorthands = map[string][2]string{
	"margin":  {"margin-left", "margin-right"},
	"padding": {"padding-left", "padding-right"},
}

// ParseStylesheet reads a CSS file. Rules it cannot interpret — selectors
// cascadia does not support, conditional @media blocks, nested rules — are
// skipped rather than failing the sheet: one exotic selector in a 70 KB
// stylesheet must not cost the reader every other rule.
func ParseStylesheet(src []byte) *Stylesheet {
	sheet := &Stylesheet{}
	parser := css.NewParser(parse.NewInputBytes(src), false)
	// skipDepth counts open blocks whose contents do not apply: conditional
	// @media, @supports with a condition, @font-face, keyframes and nesting.
	skipDepth := 0
	var current []rule
	for {
		grammar, _, data := parser.Next()
		switch grammar {
		case css.ErrorGrammar:
			return sheet
		case css.BeginAtRuleGrammar:
			if skipDepth > 0 || !appliesAtRule(string(data), parser.Values()) {
				skipDepth++
			}
		case css.EndAtRuleGrammar:
			if skipDepth > 0 {
				skipDepth--
			}
		case css.BeginRulesetGrammar:
			if skipDepth > 0 || current != nil {
				// A ruleset inside another is CSS nesting; its selector is
				// relative to the parent and not worth resolving here.
				skipDepth++
				continue
			}
			current = compileRules(tokensText(parser.Values()))
			if current == nil {
				skipDepth++
			}
		case css.EndRulesetGrammar:
			if skipDepth > 0 {
				skipDepth--
				continue
			}
			for _, compiled := range current {
				if len(compiled.decls) > 0 {
					sheet.rules = append(sheet.rules, compiled)
				}
			}
			current = nil
		case css.DeclarationGrammar:
			if skipDepth > 0 || current == nil {
				continue
			}
			decls := newDeclarations(string(data), parser.Values())
			for index := range current {
				current[index].decls = append(current[index].decls, decls...)
			}
		}
	}
}

// appliesAtRule reports whether the rules inside an at-rule block apply to a
// dictionary popup. Unconditional screen and all media do; anything that
// depends on width, orientation or print does not, because there is no
// viewport to evaluate them against.
func appliesAtRule(name string, values []css.Token) bool {
	if !strings.EqualFold(name, "@media") {
		return false
	}
	query := strings.ToLower(tokensText(values))
	if strings.Contains(query, "(") || strings.Contains(query, "print") ||
		strings.Contains(query, "speech") || strings.Contains(query, "not") {
		return false
	}
	return true
}

// joinValue writes a declaration's tokens back out with whitespace between
// components preserved, so a shorthand can be split into its fields.
func joinValue(values []css.Token) string {
	var builder strings.Builder
	for _, token := range values {
		if token.TokenType == css.WhitespaceToken {
			builder.WriteByte(' ')
			continue
		}
		builder.Write(token.Data)
	}
	return strings.TrimSpace(builder.String())
}

func tokensText(values []css.Token) string {
	var builder strings.Builder
	for _, token := range values {
		builder.Write(token.Data)
	}
	return strings.TrimSpace(builder.String())
}

// compileRules splits a selector list into one rule per selector. A list is
// kept even when some of its selectors do not compile, which is the browser
// behaviour for :is() but not for a plain list; dictionaries are written for
// forgiving WebViews and this matches what their authors saw.
func compileRules(selectors string) []rule {
	var out []rule
	for _, text := range splitTopLevel(selectors, ',') {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		sel, err := cascadia.ParseWithPseudoElement(text)
		if err != nil {
			continue
		}
		pseudo := strings.ToLower(sel.PseudoElement())
		if pseudo != "" && pseudo != "before" && pseudo != "after" {
			continue
		}
		if dynamicPseudoClass(text) {
			// :hover and friends describe states a popup never enters.
			continue
		}
		out = append(out, rule{sel: sel, pseudo: pseudo, spec: sel.Specificity(), key: subjectKey(text)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dynamicPseudoClass(selector string) bool {
	lower := strings.ToLower(selector)
	for _, state := range []string{":hover", ":active", ":focus", ":visited", ":target", ":checked"} {
		if strings.Contains(lower, state) {
			return true
		}
	}
	return false
}

// splitTopLevel splits on sep outside brackets, parentheses and strings.
func splitTopLevel(text string, sep rune) []string {
	var parts []string
	depth := 0
	var quote rune
	start := 0
	for index, r := range text {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			if depth > 0 {
				depth--
			}
		case r == sep && depth == 0:
			parts = append(parts, text[start:index])
			start = index + len(string(r))
		}
	}
	return append(parts, text[start:])
}

// subjectKey finds something the element a selector applies to must carry:
// a class, else an id, else a tag name. It reads only the last compound
// selector and only outside :not(...) and attribute brackets, so it never
// claims a requirement the selector does not have. Anything it cannot read is
// universal, which is merely slower.
func subjectKey(selector string) selectorKey {
	compound := lastCompound(selector)
	var tag string
	var id string
	depth := 0
	for index := 0; index < len(compound); index++ {
		c := compound[index]
		switch {
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case c == '.':
			if name := identAt(compound, index+1); name != "" {
				return selectorKey{kind: keyClass, name: name}
			}
		case c == '#':
			if id == "" {
				id = identAt(compound, index+1)
			}
		case c == ':':
			// Skip the pseudo name so its letters are not read as a tag.
			for index+1 < len(compound) && (compound[index+1] == ':' || isIdentByte(compound[index+1])) {
				index++
			}
		case index == 0 && isIdentByte(c):
			tag = strings.ToLower(identAt(compound, 0))
			index += len(tag) - 1
		}
	}
	if id != "" {
		return selectorKey{kind: keyID, name: id}
	}
	if tag != "" {
		return selectorKey{kind: keyTag, name: tag}
	}
	return selectorKey{kind: keyUniversal}
}

func lastCompound(selector string) string {
	selector = strings.TrimSpace(selector)
	depth := 0
	start := 0
	for index := 0; index < len(selector); index++ {
		switch c := selector[index]; {
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
		case depth == 0 && (c == ' ' || c == '>' || c == '+' || c == '~' || c == '\t' || c == '\n'):
			start = index + 1
		}
	}
	return strings.TrimSpace(selector[start:])
}

func identAt(text string, start int) string {
	end := start
	for end < len(text) && isIdentByte(text[end]) {
		end++
	}
	return text[start:end]
}

func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func newDeclarations(property string, values []css.Token) []declaration {
	property = strings.ToLower(strings.TrimSpace(property))
	if sides, ok := boxShorthands[property]; ok {
		decl, ok := newDeclaration(property, values)
		if !ok {
			return nil
		}
		// margin: top right bottom left, with the usual repetition rules.
		fields := strings.Fields(decl.value)
		right, left := "", ""
		switch len(fields) {
		case 1:
			right, left = fields[0], fields[0]
		case 2, 3:
			right, left = fields[1], fields[1]
		case 4:
			right, left = fields[1], fields[3]
		default:
			return nil
		}
		return []declaration{
			{property: sides[0], value: left, important: decl.important},
			{property: sides[1], value: right, important: decl.important},
		}
	}
	if !properties[property] {
		return nil
	}
	decl, ok := newDeclaration(property, values)
	if !ok {
		return nil
	}
	return []declaration{decl}
}

func newDeclaration(property string, values []css.Token) (declaration, bool) {
	var parts []css.Token
	important := false
	for index := 0; index < len(values); index++ {
		token := values[index]
		if token.TokenType == css.DelimToken && string(token.Data) == "!" &&
			index+1 < len(values) && strings.EqualFold(string(values[index+1].Data), "important") {
			important = true
			index++
			continue
		}
		parts = append(parts, token)
	}
	var value string
	if property == "content" {
		var ok bool
		value, ok = contentText(parts)
		if !ok {
			return declaration{}, false
		}
	} else {
		value = strings.ToLower(joinValue(parts))
	}
	return declaration{property: property, value: value, important: important}, true
}

// contentText resolves a `content` value made only of string literals. Counters,
// attr() and images depend on state this converter does not model, and a
// partial string would be worse than none. "none" and "normal" resolve to an
// explicit empty value so they can still override an inherited rule.
func contentText(values []css.Token) (string, bool) {
	var builder strings.Builder
	for _, token := range values {
		switch token.TokenType {
		case css.WhitespaceToken:
		case css.StringToken:
			builder.WriteString(unquoteCSS(token.Data))
		case css.IdentToken:
			ident := strings.ToLower(string(token.Data))
			if ident != "none" && ident != "normal" {
				return "", false
			}
			return "", true
		default:
			return "", false
		}
	}
	return visibleContent(builder.String()), true
}

// visibleContent drops icon-font glyphs. They live in the Private Use Area and
// render as empty boxes without the dictionary's font.
func visibleContent(text string) string {
	var builder strings.Builder
	for _, r := range text {
		if unicode.In(r, unicode.Co) || r == '�' {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// unquoteCSS removes the quotes from a CSS string token and resolves its
// escapes: `\2022 ` is a code point, `\"` a literal and an escaped newline a
// line continuation.
func unquoteCSS(raw []byte) string {
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') {
		raw = raw[1 : len(raw)-1]
	}
	if !bytes.ContainsRune(raw, '\\') {
		return string(raw)
	}
	var builder strings.Builder
	for index := 0; index < len(raw); index++ {
		c := raw[index]
		if c != '\\' || index+1 >= len(raw) {
			builder.WriteByte(c)
			continue
		}
		index++
		if raw[index] == '\n' {
			continue
		}
		end := index
		for end < len(raw) && end-index < 6 && isHex(raw[end]) {
			end++
		}
		if end == index {
			builder.WriteByte(raw[index])
			continue
		}
		code, err := strconv.ParseUint(string(raw[index:end]), 16, 32)
		if err == nil && code > 0 && code <= unicode.MaxRune {
			builder.WriteRune(rune(code))
		}
		if end < len(raw) && (raw[end] == ' ' || raw[end] == '\t' || raw[end] == '\n') {
			end++
		}
		index = end - 1
	}
	return builder.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// positiveLength reports whether a CSS length leaves visible space: a number
// above zero in any unit. Negative margins pull content together and auto
// centres it; neither separates two words.
func positiveLength(value string) bool {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && (value[end] == '.' || (value[end] >= '0' && value[end] <= '9')) {
		end++
	}
	if end == 0 {
		return false
	}
	number, err := strconv.ParseFloat(value[:end], 64)
	return err == nil && number > 0
}

// computedStyle is the winning value of every kept property for one element
// or one of its pseudo-elements. Empty means no rule set it.
type computedStyle map[string]string

type winner struct {
	value     string
	important bool
	inline    bool
	spec      cascadia.Specificity
	order     int
}

// wins reports whether a candidate declaration overrides the current winner,
// by the ordinary cascade: !important, then inline style, then specificity,
// then source order.
func (w winner) losesTo(candidate winner) bool {
	if w.important != candidate.important {
		return candidate.important
	}
	if w.inline != candidate.inline {
		return candidate.inline
	}
	if w.spec != candidate.spec {
		return w.spec.Less(candidate.spec)
	}
	return w.order <= candidate.order
}

// styles holds the cascade result for a whole document.
type styles struct {
	element map[*html.Node]computedStyle
	before  map[*html.Node]computedStyle
	after   map[*html.Node]computedStyle
}

func (s *styles) of(node *html.Node) computedStyle { return s.element[node] }

// cascade resolves every stylesheet, then inline style attributes, against the
// document as it was authored. It must run before any mutation: selectors like
// `li p:last-child` and `.cf + .def-g` depend on the original siblings.
func cascade(doc *html.Node, sheets []*Stylesheet) *styles {
	type slot struct {
		node   *html.Node
		pseudo string
	}
	winners := make(map[slot]map[string]winner)
	set := func(target slot, decl declaration, candidate winner) {
		props := winners[target]
		if props == nil {
			props = make(map[string]winner)
			winners[target] = props
		}
		candidate.value = decl.value
		candidate.important = decl.important
		if current, ok := props[decl.property]; ok && !current.losesTo(candidate) {
			return
		}
		props[decl.property] = candidate
	}

	// Index rules by the class, id or tag their subject needs.
	byClass := make(map[string][]int)
	byID := make(map[string][]int)
	byTag := make(map[string][]int)
	var universal []int
	var all []rule
	for _, sheet := range sheets {
		if sheet == nil {
			continue
		}
		for _, compiled := range sheet.rules {
			index := len(all)
			all = append(all, compiled)
			switch compiled.key.kind {
			case keyClass:
				byClass[compiled.key.name] = append(byClass[compiled.key.name], index)
			case keyID:
				byID[compiled.key.name] = append(byID[compiled.key.name], index)
			case keyTag:
				byTag[compiled.key.name] = append(byTag[compiled.key.name], index)
			default:
				universal = append(universal, index)
			}
		}
	}

	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			candidates := append([]int(nil), universal...)
			candidates = append(candidates, byTag[node.Data]...)
			for _, class := range strings.Fields(attr(node, "class")) {
				candidates = append(candidates, byClass[class]...)
			}
			if id := attr(node, "id"); id != "" {
				candidates = append(candidates, byID[id]...)
			}
			seen := make(map[int]bool, len(candidates))
			for _, index := range candidates {
				if seen[index] {
					continue
				}
				seen[index] = true
				compiled := all[index]
				if !compiled.sel.Match(node) {
					continue
				}
				for _, decl := range compiled.decls {
					set(slot{node, compiled.pseudo}, decl, winner{spec: compiled.spec, order: index})
				}
			}
			if style, ok := attrOK(node, "style"); ok {
				for _, decl := range inlineDeclarations(style) {
					set(slot{node, ""}, decl, winner{inline: true})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)

	out := &styles{
		element: make(map[*html.Node]computedStyle),
		before:  make(map[*html.Node]computedStyle),
		after:   make(map[*html.Node]computedStyle),
	}
	for target, props := range winners {
		style := make(computedStyle, len(props))
		for property, w := range props {
			style[property] = w.value
		}
		switch target.pseudo {
		case "before":
			out.before[target.node] = style
		case "after":
			out.after[target.node] = style
		default:
			out.element[target.node] = style
		}
	}
	return out
}

func inlineDeclarations(style string) []declaration {
	parser := css.NewParser(parse.NewInputString(style), true)
	var out []declaration
	for {
		grammar, _, data := parser.Next()
		if grammar == css.ErrorGrammar {
			return out
		}
		if grammar != css.DeclarationGrammar {
			continue
		}
		out = append(out, newDeclarations(string(data), parser.Values())...)
	}
}

func attr(node *html.Node, name string) string {
	value, _ := attrOK(node, name)
	return value
}

func attrOK(node *html.Node, name string) (string, bool) {
	for _, a := range node.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}
