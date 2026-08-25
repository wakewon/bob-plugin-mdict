package diagnose_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

// The warning signals are diagnostic, not verdicts. What these tests fix is
// that each one fires on the shape it was written for and stays quiet on an
// ordinary dictionary, so the flagged list stays worth reading.

func inspectEntries(t *testing.T, build func(index int) (string, string)) diagnose.Report {
	t.Helper()
	var entries []testmdx.Entry
	for i := 0; i < 8; i++ {
		key, markup := build(i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: markup})
	}
	dict := syntheticDictionary(t, entries)
	return diagnose.Inspect(dict, diagnose.Options{Sampling: diagnose.SampleOptions{Pool: 8, Keep: 5}})
}

func TestOrdinaryDictionaryRaisesNoSignals(t *testing.T) {
	report := inspectEntries(t, func(i int) (string, string) {
		key := fmt.Sprintf("glimmet%02d", i)
		return key, structuredEntry(key)
	})
	if len(report.Warnings) != 0 {
		t.Errorf("a well-parsed dictionary was flagged: %+v", report.Warnings)
	}
}

func TestRichMarkupWithoutDefinitionsIsFlagged(t *testing.T) {
	// Deeply nested markup carrying nothing the parser can name a definition.
	report := inspectEntries(t, func(i int) (string, string) {
		key := fmt.Sprintf("opaque%02d", i)
		var builder strings.Builder
		builder.WriteString(`<div class="a"><div class="b">` + key + `</div>`)
		for j := 0; j < 40; j++ {
			fmt.Fprintf(&builder, `<div class="c%d"><span class="d%d">%s</span></div>`,
				j, j, strings.Repeat("filler text with no structural meaning ", 2))
		}
		builder.WriteString(`</div>`)
		return key, builder.String()
	})
	if !hasWarning(report, "rich-html-no-definitions") {
		t.Errorf("expected rich-html-no-definitions, got %+v", report.Warnings)
	}
}

func TestOversizedDefinitionIsFlagged(t *testing.T) {
	report := inspectEntries(t, func(i int) (string, string) {
		key := fmt.Sprintf("verbose%02d", i)
		body := strings.Repeat("a very long run of explanatory prose that never ends. ", 30)
		return key, fmt.Sprintf(`<div class="entry"><h1 class="hw">%s</h1>`+
			`<div class="sense"><span class="def">%s</span></div>`+
			`<div class="sense"><span class="def">%s</span></div></div>`, key, body, body)
	})
	if !hasWarning(report, "oversized-definition") {
		t.Errorf("expected oversized-definition, got %+v", report.Warnings)
	}
}

func TestHeadwordMismatchIsFlagged(t *testing.T) {
	report := inspectEntries(t, func(i int) (string, string) {
		key := fmt.Sprintf("glimmet%02d", i)
		// The record names a completely different word from the key it is
		// stored under, which is worth a human look either way.
		return key, fmt.Sprintf(`<div class="entry"><h1 class="hw">unrelated</h1>`+
			`<div class="sense"><span class="def">An invented thing, number %d.</span></div>`+
			`<div class="sense"><span class="def">Another invented thing, number %d.</span></div></div>`, i, i)
	})
	if !hasWarning(report, "headword-unlike-lookup-key") {
		t.Errorf("expected headword-unlike-lookup-key, got %+v", report.Warnings)
	}
}

func TestBilingualWithoutTranslationsIsFlagged(t *testing.T) {
	report := inspectEntries(t, func(i int) (string, string) {
		key := fmt.Sprintf("glimmet%02d", i)
		// Two scripts fused into one text node: there is no element boundary
		// for the gloss to be lifted out of.
		return key, fmt.Sprintf(`<div class="entry"><h1 class="hw">%s</h1>`+
			`<div class="sense"><span class="def">An invented thing 一件虚构的东西</span></div>`+
			`<div class="sense"><span class="def">Another invented thing 另一件虚构的东西</span></div></div>`, key)
	})
	if !hasWarning(report, "bilingual-without-translations") {
		t.Errorf("expected bilingual-without-translations, got %+v", report.Warnings)
	}
}

// The same content, with the gloss in an element of its own, is extracted and
// must not be flagged.
func TestBilingualWithTranslationsIsNotFlagged(t *testing.T) {
	report := inspectEntries(t, func(i int) (string, string) {
		key := fmt.Sprintf("glimmet%02d", i)
		return key, fmt.Sprintf(`<div class="entry"><h1 class="hw">%s</h1>`+
			`<div class="sense"><span class="def">An invented thing<span class="zz">一件虚构的东西</span></span></div>`+
			`<div class="sense"><span class="def">Another invented thing<span class="zz">另一件虚构的东西</span></span></div></div>`, key)
	})
	if hasWarning(report, "bilingual-without-translations") {
		t.Errorf("a dictionary whose glosses were extracted was still flagged: %+v", report.Warnings)
	}
	if report.Coverage.Translations == 0 {
		t.Error("no translations were extracted from element-separated glosses")
	}
}
