package validate

import (
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
)

func TestParityKeepsShortCJKSemanticFields(t *testing.T) {
	set := &entryir.EntrySet{Records: []entryir.EntryRecord{{Entry: &entryir.Entry{
		Parts: []entryir.Part{{Senses: []entryir.Sense{{Definition: "discard something", Translation: "放弃", Labels: []string{"正式"}}}}},
	}}}}
	fields := irFields(set)
	found := map[string]bool{}
	for _, field := range fields {
		found[field.text] = true
	}
	if !found["放弃"] || !found["正式"] {
		t.Fatalf("short CJK fields were ignored: %+v", fields)
	}
}

func TestRichSectionParityUsesRenderedBlockUnits(t *testing.T) {
	entry := &entryir.Entry{UsageNotes: []entryir.Section{{
		Title:  "Register",
		Body:   "formal register spoken register",
		Blocks: []entryir.RichBlock{{Kind: entryir.RichTable, Rows: [][]string{{"formal register"}, {"spoken register"}}}},
	}}, Sections: []entryir.Section{{
		Title: "Entry",
		Body:  "alpha beta gamma delta",
		Blocks: []entryir.RichBlock{{
			Kind: entryir.RichTable,
			Rows: [][]string{{"alpha beta"}, {"gamma delta"}},
		}},
	}}}
	fields := markdownFields(&entryir.EntrySet{Records: []entryir.EntryRecord{{Entry: entry}}})
	if len(fields) != 5 || fields[0].text != "Register" || fields[1].text != "formal register" ||
		fields[2].text != "spoken register" || fields[3].text != "alpha beta" || fields[4].text != "gamma delta" {
		t.Fatalf("rich section fields = %+v", fields)
	}
	bobFields := irFields(&entryir.EntrySet{Records: []entryir.EntryRecord{{Entry: entry}}})
	if len(bobFields) != 3 || bobFields[0].text != "Register" || bobFields[1].text != "formal register spoken register" ||
		bobFields[2].text != "alpha beta gamma delta" {
		t.Fatalf("Bob section fields = %+v", bobFields)
	}
}

func TestAggregateMetricsWeightsEveryRecord(t *testing.T) {
	items := []Metrics{
		{SourceTokens: 10, OutputTokens: 10, RecordTokens: 20, Retention: 1, Duplication: 0},
		{SourceTokens: 30, OutputTokens: 20, RecordTokens: 30, Retention: 0.5, Duplication: 0.25,
			RepeatedDefinitions: 2, LargestFieldShare: 0.7, LargestFieldKind: "definition"},
	}
	got := aggregateMetrics(items)
	if got.SourceTokens != 40 || got.OutputTokens != 30 || got.RecordTokens != 50 ||
		got.Retention != 0.625 || got.Duplication != 1.0/6.0 || got.Scope != 0.8 ||
		got.RepeatedDefinitions != 2 || got.LargestFieldKind != "definition" {
		t.Fatalf("aggregate = %+v", got)
	}
}

func TestHonestFallbackAfterSuspiciousStructureIsNotRegression(t *testing.T) {
	before := Snapshot{
		RecordHash: "same", EntryHash: "before",
		Fields:  Fields{Senses: 50},
		Metrics: Metrics{Retention: 0.82, Duplication: 0.42},
		Signals: []string{SignalHighSenseCount, SignalHighDuplication},
	}
	after := Snapshot{
		RecordHash: "same", EntryHash: "after",
		Fields:  Fields{Fallback: 1},
		Metrics: Metrics{Retention: 0.84, Duplication: 0.02},
	}
	change, _ := classify(before, after, true)
	if change == ChangeRegression {
		t.Fatalf("honest fallback classified as %q", change)
	}
}

// A navigation target is written inside a code span, where Markdown syntax is
// already inert, so it is not escaped the way prose is. The parity check has to
// know that, or every cross-reference containing Markdown punctuation — and
// "[sense 2]" is a real one — is reported as a field the renderer dropped.
func TestParityAcceptsUnescapedNavigationTargetsInCodeSpans(t *testing.T) {
	set := &entryir.EntrySet{
		LookupKey: "analogue",
		Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: &entryir.Entry{
			Headword:        "analogue",
			Parts:           []entryir.Part{{POS: "noun", Senses: []entryir.Sense{{Definition: "a thing seen as comparable to another"}}}},
			CrossReferences: []string{"[sense 2]"},
			Related:         []string{"digital*"},
		}}},
	}
	markdown := mdrender.RenderEntrySet(set, mdrender.DefaultOptions())

	var c checker
	c.checkMarkdown(parityInput{set: set, markdown: markdown, irJSON: encodeJSON(set)})
	for _, check := range c.checks {
		if check.Name == "markdown-preserves-semantic-fields" && !check.OK {
			t.Fatalf("code-spanned navigation target reported missing: %s\n%s", check.Detail, markdown)
		}
	}

	// The check must still catch a target that really is gone, or it has been
	// weakened into always passing.
	stripped := strings.ReplaceAll(markdown, mdrender.NavigationTarget("[sense 2]"), "")
	var after checker
	after.checkMarkdown(parityInput{set: set, markdown: stripped, irJSON: encodeJSON(set)})
	failed := false
	for _, check := range after.checks {
		if check.Name == "markdown-preserves-semantic-fields" && !check.OK {
			failed = true
		}
	}
	if !failed {
		t.Fatal("removing a navigation target did not fail the parity check")
	}
}
