package parser

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// parseOxfordCollocations handles the stable CK/JS/JX/GZ/LJ template used by
// the Oxford Collocations family. It remains inside the canonical parser and
// fills the existing lightweight IR: lexical senses stay senses, GZ groups
// become collocation strings or phrases, and only LJ blocks become examples.
func (s *parseState) parseOxfordCollocations() bool {
	partNodes := QueryAll(s.doc, ParseSelector(".JS"))
	if len(partNodes) == 0 {
		return false
	}
	for _, partNode := range partNodes {
		pos := CanonicalPOS(textAt(partNode, ".DX"))
		var senses []entryir.Sense
		currentSense := -1
		group := ""
		phraseGroup := false
		lastPhrase := -1
		looseExamples := 0

		root := Query(partNode, ParseSelector(".CX"))
		if root == nil {
			root = partNode
		}
		for node := root.FirstChild; node != nil; node = node.NextSibling {
			if node.Type != html.ElementNode {
				continue
			}
			switch {
			case classTokenMatches(node, []string{"YX", "DX"}):
				continue
			case classTokenMatches(node, []string{"JX"}):
				sense := s.oxfordCollocationSense(node)
				if sense.Definition != "" || sense.Translation != "" {
					senses = append(senses, sense)
					currentSense = len(senses) - 1
				}
				group, phraseGroup, lastPhrase = "", false, -1
			case classTokenMatches(node, []string{"GZ"}):
				bold := textAt(node, ".bold")
				if heading, ok := oxfordCollocationGroup(bold); ok {
					group = heading
					phraseGroup = ClassifySemanticLabel(heading) == LabelPhrase
					lastPhrase = -1
					continue
				}
				item, gloss := oxfordCollocationItem(node, bold)
				if item == "" && gloss == "" {
					continue
				}
				if phraseGroup {
					s.entry.Phrases = append(s.entry.Phrases, entryir.PhraseEntry{Phrase: item, Definition: gloss})
					lastPhrase = len(s.entry.Phrases) - 1
					continue
				}
				line := strings.TrimSpace(strings.Join(nonEmpty(pos, group, item), " · "))
				if gloss != "" {
					if line != "" {
						line += " — "
					}
					line += gloss
				}
				if line != "" {
					s.entry.Collocations = append(s.entry.Collocations, line)
				}
			case classTokenMatches(node, []string{"LJ"}):
				example := entryir.Example{Text: textAt(node, ".LY"), Translation: textAt(node, ".LS")}
				if example.Text == "" {
					continue
				}
				if phraseGroup && lastPhrase >= 0 {
					s.entry.Phrases[lastPhrase].Examples = append(s.entry.Phrases[lastPhrase].Examples, example)
				} else if currentSense >= 0 {
					if len(senses[currentSense].Examples) < s.opts.MaxExamplesPerSense {
						senses[currentSense].Examples = append(senses[currentSense].Examples, example)
					}
				} else {
					if looseExamples >= s.opts.MaxExamplesPerSense {
						continue
					}
					body := example.Text
					if example.Translation != "" {
						body += " — " + example.Translation
					}
					s.entry.Sections = append(s.entry.Sections, entryir.Section{Title: "Examples", Body: body})
					looseExamples++
				}
			}
		}
		if len(senses) > 0 {
			s.appendPart(entryir.Part{POS: pos, Senses: senses, Confidence: confidenceForPOS(pos), Rule: "profile:oxford-collocations"})
		}
	}
	s.partsHandled = true
	return len(s.entry.Parts) > 0 || len(s.entry.Collocations) > 0 || len(s.entry.Phrases) > 0
}

func (s *parseState) oxfordCollocationSense(node *html.Node) entryir.Sense {
	number := ParseSenseNumber(textAt(node, ".entryNum"))
	skip := map[*html.Node]struct{}{}
	for _, candidate := range QueryAll(node, ParseSelector(".entryNum, .entryDot")) {
		skip[candidate] = struct{}{}
	}
	text := Text(node, TextOptions{SkipHidden: true, SkipNodes: skip})
	definition, translation, ok := s.splitTextByScript(text)
	if !ok {
		definition = text
	}
	return entryir.Sense{Number: number, Definition: definition, Translation: translation, Confidence: 0.9, Rule: "profile:oxford-collocations:sense"}
}

func oxfordCollocationGroup(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	for _, prefix := range []string{"[搭配]", "【搭配】", "搭配"} {
		if strings.HasPrefix(text, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(text, prefix)), " :："), true
		}
	}
	return "", false
}

func oxfordCollocationItem(node *html.Node, bold string) (string, string) {
	item := strings.TrimSpace(bold)
	all := Normalize(Text(node, TextOptions{SkipHidden: true}))
	gloss := strings.TrimSpace(strings.TrimPrefix(all, item))
	gloss = strings.Trim(gloss, " :：")
	return item, gloss
}

func textAt(root *html.Node, selector string) string {
	if node := Query(root, ParseSelector(selector)); node != nil {
		return Normalize(Text(node, TextOptions{SkipHidden: true}))
	}
	return ""
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
