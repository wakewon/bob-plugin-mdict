package parser

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// Bilingual dictionaries put the translation right next to the text it
// translates, and the reader tells them apart at a glance because they are
// written in different scripts. A profile can name the element that holds the
// gloss; without one, the script itself is the evidence.
//
// Which side is the translation follows from the headword: in a dictionary
// whose headwords are English, the Chinese is the gloss, and in one whose
// headwords are Chinese, the English is. Nothing here needs to know which
// languages are involved, only that two scripts are in play and which of them
// the entry is keyed by.

// script names the writing system a run of text is mostly written in.
type script string

const (
	scriptUnknown script = ""
	scriptLatin   script = "latin"
	scriptCJK     script = "cjk"
	scriptMixed   script = "mixed"
)

// scriptPurity is how one-sided a run of text must be before it counts as
// belonging to one script rather than being a mixture.
const scriptPurity = 0.8

// minTranslationRunes keeps a stray loanword or abbreviation from being lifted
// out of a definition as though it were a gloss.
const minTranslationRunes = 4

// dominantScript classifies text by the script most of its letters are in.
func dominantScript(text string) script {
	latin, cjk := 0, 0
	for _, r := range text {
		switch {
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			latin++
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			cjk++
		case unicode.IsLetter(r) && r > unicode.MaxASCII:
			// Accented Latin, Cyrillic and Greek all behave like Latin here:
			// what matters is only that they are not CJK.
			latin++
		}
	}
	total := latin + cjk
	if total == 0 {
		return scriptUnknown
	}
	switch {
	case float64(latin)/float64(total) >= scriptPurity:
		return scriptLatin
	case float64(cjk)/float64(total) >= scriptPurity:
		return scriptCJK
	default:
		return scriptMixed
	}
}

// opposite returns the script a translation would be written in.
func (s script) opposite() script {
	switch s {
	case scriptLatin:
		return scriptCJK
	case scriptCJK:
		return scriptLatin
	default:
		return scriptUnknown
	}
}

// foreignScriptNodes finds the outermost elements under root that are written
// entirely in the other script.
//
// Outermost, because a gloss wrapped in two spans should be lifted once; and
// only elements that are wholly in the other script, because a definition that
// merely quotes a foreign word is not a translation of anything.
func (s *parseState) foreignScriptNodes(root *html.Node) map[*html.Node]struct{} {
	want := s.entryScript.opposite()
	if want == scriptUnknown || root == nil {
		return nil
	}
	found := make(map[*html.Node]struct{})
	Walk(root, func(node *html.Node) bool {
		if node == root {
			return true
		}
		text := Normalize(Text(node, TextOptions{SkipHidden: true}))
		if len([]rune(text)) < minTranslationRunes {
			return true
		}
		if dominantScript(text) != want {
			return true
		}
		found[node] = struct{}{}
		return false
	})
	if len(found) == 0 {
		return nil
	}
	return found
}

// splitByScript separates a node's own-language text from the glosses inside
// it. It returns ok=false when there is nothing to separate, or when removing
// the glosses would leave nothing behind — a node written entirely in the
// other script is the text, not a translation of it.
func (s *parseState) splitByScript(node *html.Node) (string, string, bool) {
	foreign := s.foreignScriptNodes(node)
	if len(foreign) == 0 {
		return "", "", false
	}
	main := Text(node, TextOptions{SkipHidden: true, SkipNodes: foreign})
	if len([]rune(main)) < minSenseTextRunes {
		return "", "", false
	}
	var parts []string
	for child := range foreign {
		if text := Normalize(Text(child, TextOptions{SkipHidden: true})); text != "" {
			parts = append(parts, text)
		}
	}
	// Iteration order over a map is random; document order is what a reader
	// expects, so sort the pieces back into the order they appear in.
	sortByDocumentOrder(node, foreign, &parts)
	dedupeStrings(&parts)
	if len(parts) == 0 {
		return "", "", false
	}
	return main, Normalize(strings.Join(parts, " ")), true
}

// sortByDocumentOrder rewrites parts in the order their nodes appear.
func sortByDocumentOrder(root *html.Node, nodes map[*html.Node]struct{}, parts *[]string) {
	ordered := make([]string, 0, len(nodes))
	Walk(root, func(node *html.Node) bool {
		if _, ok := nodes[node]; !ok {
			return true
		}
		if text := Normalize(Text(node, TextOptions{SkipHidden: true})); text != "" {
			ordered = append(ordered, text)
		}
		return false
	})
	if len(ordered) == len(*parts) {
		*parts = ordered
	}
}
