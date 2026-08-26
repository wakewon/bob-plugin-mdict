package parser_test

import (
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

func TestOxfordCollocationsProfileKeepsGroupsOutOfExamplesAndPOS(t *testing.T) {
	markup := []byte(`<span class="CK"><span class="DC">test</span>
		<span class="JS"><span class="CY"><span class="CX"><span class="YX">test</span><span class="DX">noun</span>
			<span class="JX"><span class="entryNum">1.</span><span class="entryDot">■</span> examination of ability 能力考查</span>
			<span class="GZ"><span class="bold">[搭配]ADJ.</span></span>
			<span class="GZ"><span class="bold">demanding, difficult</span><br>高难度的</span>
			<span class="LJ"><span class="LY">This is a demanding test.</span><span class="LS">这是一次高难度测试。</span></span>
			<span class="GZ"><span class="bold">[搭配]QUANT.</span></span>
			<span class="GZ"><span class="bold">number, series</span><br>若干；一系列</span>
			<span class="JX"><span class="entryNum">2.</span><span class="entryDot">■</span> a real trial 真正考验</span>
			<span class="GZ"><span class="bold">[搭配]NUMBER</span></span>
			<span class="GZ"><span class="bold">two</span><br>两个</span>
			<span class="GZ"><span class="bold">[搭配]PHRASES</span></span>
			<span class="GZ"><span class="bold">the acid test</span><br>决定性考验</span>
			<span class="LJ"><span class="LY">It was the acid test.</span><span class="LS">这是决定性考验。</span></span>
		</span></span></span></span>`)
	entry, err := parser.Parse(markup, parser.Options{Headword: "test", Profile: profiles.ByID("oxford-collocations"), MaxExamplesPerSense: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 1 || entry.Parts[0].POS != "noun" || len(entry.Parts[0].Senses) != 2 {
		t.Fatalf("lexical senses/POS = %+v", entry.Parts)
	}
	for _, part := range entry.Parts {
		if part.POS == "number" {
			t.Fatalf("collocation category NUMBER became a POS: %+v", entry.Parts)
		}
	}
	if got := entry.Parts[0].Senses[0].Examples; len(got) != 1 || got[0].Text != "This is a demanding test." || got[0].Translation != "这是一次高难度测试。" {
		t.Fatalf("genuine example = %+v", got)
	}
	for _, example := range entry.Parts[0].Senses[0].Examples {
		if example.Text == "ADJ." || strings.Contains(example.Text, "demanding, difficult") {
			t.Fatalf("collocation group/item became an example: %+v", entry.Parts[0].Senses[0].Examples)
		}
	}
	joined := strings.Join(entry.Collocations, "\n")
	for _, want := range []string{"noun · ADJ. · demanding, difficult — 高难度的", "noun · QUANT. · number, series — 若干；一系列", "noun · NUMBER · two — 两个"} {
		if !strings.Contains(joined, want) {
			t.Errorf("collocations missing %q: %v", want, entry.Collocations)
		}
	}
	if len(entry.Phrases) != 1 || entry.Phrases[0].Phrase != "the acid test" ||
		entry.Phrases[0].Definition != "决定性考验" || len(entry.Phrases[0].Examples) != 1 {
		t.Fatalf("PHRASES group = %+v", entry.Phrases)
	}
	if strings.Contains(joined, "[搭配]") {
		t.Fatalf("publisher marker leaked into IR: %v", entry.Collocations)
	}
}

func TestOxfordCollocationsProfileHandlesUnnumberedEntries(t *testing.T) {
	markup := []byte(`<span class="CK"><span class="DC">good</span><span class="JS"><span class="CY"><span class="CX">
		<span class="YX">good</span><span class="DX">adj.</span>
		<span class="GZ"><span class="bold">[搭配]VERBS</span></span>
		<span class="GZ"><span class="bold">be, feel, look</span><br>很好；感觉不错</span>
		<span class="LJ"><span class="LY">She feels good.</span><span class="LS">她感觉不错。</span></span>
	</span></span></span></span>`)
	entry, err := parser.Parse(markup, parser.Options{Headword: "good", Profile: profiles.ByID("oxford-collocations")})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Parts) != 0 || len(entry.Collocations) != 1 || entry.Collocations[0] != "adjective · VERBS · be, feel, look — 很好；感觉不错" {
		t.Fatalf("un-numbered collocation entry = parts=%+v collocations=%+v", entry.Parts, entry.Collocations)
	}
}
