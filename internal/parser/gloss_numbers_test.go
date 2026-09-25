package parser_test

import (
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

// A number belongs to the translation only when it sits within a run of gloss
// elements. Beside source-language prose it is that prose's number, and it must
// neither leak into the translation nor be lost from the text.
func TestGlossNumbersStayWithTheirLanguage(t *testing.T) {
	cases := []struct {
		name        string
		example     string
		text        string
		translation string
	}{
		{
			name:        "between gloss spans",
			example:     `<p>Costs rise.</p><p><span class="chinese-text">涨了</span> 7<span class="chinese-text">倍。</span></p>`,
			text:        "Costs rise.",
			translation: "涨了 7倍。",
		},
		{
			name:        "leading and trailing",
			example:     `<p>Costs rise.</p><p>3<span class="chinese-text">月涨价</span>5</p>`,
			text:        "Costs rise.",
			translation: "3月涨价5",
		},
		{
			name:        "number beside source-language prose is not translation",
			example:     `<p>The <b>gate</b> 5<span class="chinese-text">号门</span></p>`,
			text:        "The gate 5",
			translation: "号门",
		},
		{
			name:        "absorbed number is not repeated in the own-language text",
			example:     `<p><span class="chinese-text">共</span>12<span class="chinese-text">人</span> people in total</p>`,
			text:        "people in total",
			translation: "共12人",
		},
		{
			name:        "words between gloss spans are not absorbed",
			example:     `<p>Costs rise.</p><p><span class="chinese-text">不要混淆</span> cost <span class="chinese-text">和价格</span></p>`,
			text:        "Costs rise.",
			translation: "不要混淆 和价格",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			markup := []byte(`<div class="collinsbody"><div class="word_entry"><span class="word_key">flimber</span></div>
				<div class="collins_content"><div class="collins_en_cn example">
				<div class="caption"><span class="num">1</span> <span class="st">N-COUNT </span>An invented thing.</div>
				<ul><li>` + tc.example + `</li></ul></div></div></div>`)
			entry, err := parser.Parse(markup, parser.Options{Profile: profiles.ByID("collins-cobuild-overhaul")})
			if err != nil {
				t.Fatal(err)
			}
			if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 1 || len(entry.Parts[0].Senses[0].Examples) != 1 {
				t.Fatalf("unexpected structure: %+v", entry.Parts)
			}
			example := entry.Parts[0].Senses[0].Examples[0]
			if example.Text != tc.text || example.Translation != tc.translation {
				t.Fatalf("example = %q / %q, want %q / %q", example.Text, example.Translation, tc.text, tc.translation)
			}
		})
	}
}

// A numeral glossed by its name has no source-language text but the number, so
// the number is that text, not part of the translation.
func TestNumeralDefinitionKeepsItsNumber(t *testing.T) {
	markup := []byte(`<div class="h-g"><span class="top-g"><span class="h">flimbeen</span></span>
		<span class="block-g"><span class="pos-g"><span class="pos">number</span></span></span></div>
		<span class="n-g"><span class="def-g"><span class="d oalecd8e_switch_lang">14 <span class="oalecd8e_chn">十四</span></span></span></span>`)
	entry, err := parser.Parse(markup, parser.Options{Headword: "flimbeen", Profile: profiles.ByID("oald8")})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 1 {
		t.Fatalf("unexpected structure: %+v", entry.Parts)
	}
	sense := entry.Parts[0].Senses[0]
	if sense.Definition != "14" || sense.Translation != "十四" {
		t.Fatalf("definition/translation = %q/%q, want 14/十四", sense.Definition, sense.Translation)
	}
}
