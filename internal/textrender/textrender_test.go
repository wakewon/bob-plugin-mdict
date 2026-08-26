package textrender

import (
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

func richEntry(definition string) *entryir.Entry {
	return &entryir.Entry{
		Headword:       "wound",
		Pronunciations: []entryir.Pronunciation{{IPARegion: entryir.RegionUK, IPA: "wuːnd"}},
		Parts: []entryir.Part{{POS: "verb", Grammar: "[with object]", Senses: []entryir.Sense{{
			Number: "1", Definition: definition, Translation: "伤害", Grammar: "transitive",
			Examples:  []entryir.Example{{Text: "It wounded him.", Translation: "这伤害了他。"}},
			Subsenses: []entryir.Sense{{Number: "1.1", Definition: "injure a feeling", Translation: "伤感情"}},
		}}}},
		Forms:           []entryir.Form{{Name: "past tense", Words: []string{"wounded"}}},
		Phrases:         []entryir.PhraseEntry{{Phrase: "wound up", Definition: "tense"}},
		Collocations:    []string{"deep wound"},
		CrossReferences: []string{"injure"},
		Related:         []string{"damage"},
		Sections: []entryir.Section{{Title: "Entry", Body: "flat compatibility body", Blocks: []entryir.RichBlock{
			{Kind: entryir.RichText, Text: "First paragraph."},
			{Kind: entryir.RichText, Text: "Second paragraph."},
			{Kind: entryir.RichTable, Header: []string{"A", "B"}, Rows: [][]string{{"one", "two"}}},
		}}},
	}
}

func TestPlainTextRendersIRWithoutMarkdownSyntax(t *testing.T) {
	out := RenderEntry(richEntry("cause bodily harm"), DefaultOptions())
	for _, want := range []string{
		"wound", "Pronunciation", "UK /wuːnd/", "verb · [with object]",
		"1. transitive cause bodily harm — 伤害", "Example: It wounded him. — 这伤害了他。",
		"  1.1. injure a feeling — 伤感情", "Forms", "Phrases", "Collocations",
		"See also: injure", "Related: damage", "First paragraph.\n\nSecond paragraph.", "A | B", "one | two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"# ", "**", "`injure`", "---"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("plain output contains Markdown token %q:\n%s", forbidden, out)
		}
	}
	if strings.Contains(out, "flat compatibility body") {
		t.Fatal("ordered blocks should take precedence over flattened Section.Body")
	}
}

func TestPlainTextMultiRecordModesKeepRecordsSeparate(t *testing.T) {
	set := &entryir.EntrySet{LookupKey: "wound", Records: []entryir.EntryRecord{
		{RecordOrdinal: 1, Entry: richEntry("first record definition")},
		{RecordOrdinal: 2, Entry: richEntry("second record definition")},
		{RecordOrdinal: 3, Entry: richEntry("third record definition")},
	}}

	combinedOpts := DefaultOptions()
	combined := RenderEntrySet(set, combinedOpts)
	if !strings.Contains(combined, "Record 1 of 3") || !strings.Contains(combined, "Record 3 of 3") ||
		strings.Count(combined, "====================") != 2 {
		t.Fatalf("combined boundaries are not explicit:\n%s", combined)
	}
	first := strings.Index(combined, "first record definition")
	second := strings.Index(combined, "second record definition")
	third := strings.Index(combined, "third record definition")
	if !(first >= 0 && first < second && second < third) {
		t.Fatalf("records were interleaved:\n%s", combined)
	}

	separateOpts := UserOptions()
	separateOpts.RecordOrdinal = 2
	separate := RenderEntrySet(set, separateOpts)
	if !strings.Contains(separate, "wound²") || !strings.Contains(separate, "second record definition") ||
		strings.Contains(separate, "first record definition") || strings.Contains(separate, "third record definition") {
		t.Fatalf("explicit selection did not show exactly record 2:\n%s", separate)
	}
	if !strings.Contains(separate, "Other entries\nwound¹\nwound³") {
		t.Fatalf("plain sibling selectors are not copyable:\n%s", separate)
	}
}

func TestOptionsLimitExamplesAndExtras(t *testing.T) {
	entry := richEntry("definition")
	entry.Parts[0].Senses[0].Examples = append(entry.Parts[0].Senses[0].Examples,
		entryir.Example{Text: "Second example."})
	opts := DefaultOptions()
	opts.MaxExamplesPerSense = 1
	opts.IncludeExtras = false
	out := RenderEntry(entry, opts)
	if strings.Contains(out, "Second example") || strings.Contains(out, "Phrases") || strings.Contains(out, "See also") {
		t.Fatalf("options were not honoured:\n%s", out)
	}
}

func TestTextRenderGrammarVisibility(t *testing.T) {
	entry := &entryir.Entry{
		Parts: []entryir.Part{
			{
				POS:     "adj.",
				Grammar: "[used especially in negatives]",
				Senses: []entryir.Sense{
					{Definition: "test definition", Grammar: "[with object]"},
				},
			},
		},
	}
	opts := UserOptions()
	opts.IncludeGrammar = true
	out := RenderEntrySet(&entryir.EntrySet{
		LookupKey: "test",
		Records:   []entryir.EntryRecord{{RecordOrdinal: 1, Entry: entry}},
	}, opts)
	if !strings.Contains(out, "adj. · [used especially in negatives]") {
		t.Errorf("Part grammar should be visible when enabled:\n%s", out)
	}
	if !strings.Contains(out, "[with object]") {
		t.Errorf("Sense grammar should be visible when enabled:\n%s", out)
	}

	opts.IncludeGrammar = false
	out = RenderEntrySet(&entryir.EntrySet{
		LookupKey: "test",
		Records:   []entryir.EntryRecord{{RecordOrdinal: 1, Entry: entry}},
	}, opts)
	if strings.Contains(out, "negatives") || strings.Contains(out, "object") {
		t.Errorf("Grammar should be hidden when disabled:\n%s", out)
	}
	if !strings.Contains(out, "adj.") {
		t.Errorf("POS should remain visible:\n%s", out)
	}
}
