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
// out of a definition as though it were a gloss. CJK writes a whole word in
// two characters, so the floor there is two rather than four.
const (
	minTranslationRunes    = 4
	minCJKTranslationRunes = 2
)

// A translation says the same thing in another language, so the two sides stay
// in proportion — but the proportion depends on which way round they are. CJK
// writes in characters what Latin script writes in words, so a four-character
// gloss of a thirty-five-letter definition is ordinary, while four Latin
// letters beside a sixty-character CJK definition are not a translation at all:
// they are a romanized reading printed next to the character they belong to.
// Reading one of those as the gloss both invents a translation and truncates
// the definition it was taken from.
const (
	maxCJKGlossImbalance   = 20
	maxLatinGlossImbalance = 2
)

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
		if dominantScript(text) != want {
			return true
		}
		floor := minTranslationRunes
		if want == scriptCJK {
			floor = minCJKTranslationRunes
		}
		if len([]rune(text)) < floor {
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
	translated := Normalize(strings.Join(parts, " "))
	if !plausibleGloss(main, translated) {
		return "", "", false
	}
	return main, translated, true
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

// splitTextByScript separates a gloss that shares one text node with the text
// it glosses.
//
// Bilingual dictionaries do this constantly: "to include sth with sth else
// 把…加进去" is one string with no element boundary anywhere in it, so the
// node-level split has nothing to hold on to. The scripts still mark the seam.
//
// The rule is deliberately narrow. The text has to fall into exactly two runs
// — one in the entry's own script, one in the other — because a definition
// that alternates between scripts is quoting, not glossing, and splitting it
// anywhere would produce two halves of a sentence rather than a meaning and
// its translation.
func (s *parseState) splitTextByScript(text string) (string, string, bool) {
	want := s.entryScript.opposite()
	if want == scriptUnknown || text == "" {
		return "", "", false
	}
	runes := []rune(text)

	type run struct {
		kind  script
		start int
	}
	var runs []run
	for i, r := range runes {
		kind := scriptUnknown
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			kind = scriptCJK
		case unicode.IsLetter(r):
			kind = scriptLatin
		}
		// Digits, punctuation and spaces belong to whichever run they follow;
		// they are the one thing both scripts genuinely share.
		if kind == scriptUnknown {
			continue
		}
		if len(runs) == 0 || runs[len(runs)-1].kind != kind {
			runs = append(runs, run{kind: kind, start: i})
		}
	}
	if len(runs) != 2 {
		return "", "", false
	}
	first := strings.TrimSpace(string(runes[:runs[1].start]))
	second := strings.TrimSpace(string(runes[runs[1].start:]))

	own, translated := first, second
	if runs[0].kind == want {
		own, translated = second, first
	}
	if !plausibleGloss(own, translated) {
		return "", "", false
	}
	return Normalize(own), Normalize(translated), true
}

// plausibleGloss reports whether one side of a split is a translation of the
// other rather than an annotation attached to it.
func plausibleGloss(own, translated string) bool {
	source, gloss := len([]rune(own)), len([]rune(translated))
	if source < minSenseTextRunes {
		return false
	}
	floor, imbalance := minTranslationRunes, maxLatinGlossImbalance
	if dominantScript(translated) == scriptCJK {
		floor, imbalance = minCJKTranslationRunes, maxCJKGlossImbalance
	}
	if gloss < floor {
		return false
	}
	return gloss*imbalance >= source
}
