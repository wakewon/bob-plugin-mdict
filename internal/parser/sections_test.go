package parser_test

import (
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

func TestSeeAlsoIsCrossReferenceNotPOS(t *testing.T) {
	markup := []byte(`<article class="collinsbody">
		<header class="word_entry"><span class="word_key">flimber</span></header>
		<section class="collins_en_cn"><div class="caption">
			<span class="num">6</span><span class="st">See also</span><a>flimbered</a>
		</div></section></article>`)
	entry, err := parser.Parse(markup, parser.Options{Profile: profiles.ByID("collins-cobuild-overhaul")})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 0 {
		t.Fatalf("See also became POS: %+v", entry.Parts)
	}
	if len(entry.CrossReferences) != 1 || entry.CrossReferences[0] != "flimbered" {
		t.Fatalf("cross references = %+v", entry.CrossReferences)
	}
}

func TestPseudoPOSSectionsBecomeTypedIR(t *testing.T) {
	cases := []struct {
		label   string
		content string
		check   func(int, int, int) bool
	}{
		{"PHRASE", `<b>flimber about</b> waste time`, func(p, i, v int) bool { return p == 1 }},
		{"IDIOM", `<b>flimber the moon</b> attempt the impossible`, func(p, i, v int) bool { return i == 1 }},
		{"PHRASAL VERB", `<b>flimber down</b> become calmer`, func(p, i, v int) bool { return v == 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			markup := []byte(`<article class="collinsbody"><header class="word_entry"><span class="word_key">flimber</span></header>
				<section class="collins_en_cn"><div class="caption"><span class="st">` + tc.label + `</span>` + tc.content + `</div></section></article>`)
			entry, err := parser.Parse(markup, parser.Options{Profile: profiles.ByID("collins-cobuild-overhaul")})
			if err != nil {
				t.Fatal(err)
			}
			if len(entry.Parts) != 0 || !tc.check(len(entry.Phrases), len(entry.Idioms), len(entry.PhrasalVerbs)) {
				t.Fatalf("classification failed: parts=%+v phrases=%+v idioms=%+v phrasal=%+v", entry.Parts, entry.Phrases, entry.Idioms, entry.PhrasalVerbs)
			}
		})
	}
}

func TestUnknownGenuinePOSIsPreserved(t *testing.T) {
	markup := []byte(`<article class="collinsbody"><header class="word_entry"><span class="word_key">flimber</span></header>
		<section class="collins_en_cn"><div class="caption"><span class="st">PARTICLE-X</span>An invented grammatical category.</div></section></article>`)
	entry, err := parser.Parse(markup, parser.Options{Profile: profiles.ByID("collins-cobuild-overhaul")})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 1 || entry.Parts[0].POS != "PARTICLE-X" {
		t.Fatalf("unknown POS was not preserved: %+v", entry.Parts)
	}
}

func TestDerivativeAndSynonymHeadingsAreTyped(t *testing.T) {
	markup := []byte(`<article class="collinsbody"><header class="word_entry"><span class="word_key">flimber</span></header>
		<section class="collins_en_cn"><div class="caption"><span class="st">Derivative</span><b>flimberish</b> resembling a flimber</div></section>
		<section class="collins_en_cn"><div class="caption"><span class="st">Synonym / Antonym section</span>smooth, level</div></section></article>`)
	entry, err := parser.Parse(markup, parser.Options{Profile: profiles.ByID("collins-cobuild-overhaul")})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 0 || len(entry.Derivatives) != 1 || len(entry.Synonyms) != 2 {
		t.Fatalf("semantic headings were not typed: parts=%+v derivatives=%+v synonyms=%+v", entry.Parts, entry.Derivatives, entry.Synonyms)
	}
}

func TestUsageGrammarAndCollocationHeadingsAreTyped(t *testing.T) {
	markup := []byte(`<article class="collinsbody"><header class="word_entry"><span class="word_key">flimber</span></header>
		<section class="collins_en_cn"><div class="caption"><span class="st">Usage notes</span>Mostly used in careful speech.</div></section>
		<section class="collins_en_cn"><div class="caption"><span class="st">Grammar</span>Usually followed by a preposition.</div></section>
		<section class="collins_en_cn"><div class="caption"><span class="st">Collocations</span>secure a flimber, loose flimber</div></section></article>`)
	entry, err := parser.Parse(markup, parser.Options{Profile: profiles.ByID("collins-cobuild-overhaul")})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 0 || len(entry.UsageNotes) != 1 || len(entry.GrammarNotes) != 1 || len(entry.Collocations) != 2 {
		t.Fatalf("semantic headings were not typed: parts=%+v usage=%+v grammar=%+v collocations=%+v",
			entry.Parts, entry.UsageNotes, entry.GrammarNotes, entry.Collocations)
	}
}

func TestGenericStandaloneSemanticHeadingsCreateBoundaries(t *testing.T) {
	markup := []byte(`<article><h1>flimber</h1>
		<div class="sense"><span class="pos">noun</span><span class="definition">a synthetic tool</span>
			<span class="example">A normal example.</span></div>
		<strong>PHRASES</strong>
		<ul><li>flimber about: waste time</li><li>flimber on: continue slowly</li></ul>
		<h3>EDITORIAL NOTE</h3><p>A separately headed note.</p></article>`)
	entry, err := parser.Parse(markup, parser.Options{Headword: "flimber"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 1 || len(entry.Parts[0].Senses[0].Examples) != 1 {
		t.Fatalf("primary sense/examples changed: %+v", entry.Parts)
	}
	if len(entry.Phrases) != 2 || entry.Phrases[0].Phrase != "flimber about" {
		t.Fatalf("PHRASES was swallowed or misclassified: %+v", entry.Phrases)
	}
	if len(entry.Sections) != 1 || entry.Sections[0].Title != "EDITORIAL NOTE" || entry.Sections[0].Body != "A separately headed note." {
		t.Fatalf("unknown explicit heading was swallowed: %+v", entry.Sections)
	}
}
