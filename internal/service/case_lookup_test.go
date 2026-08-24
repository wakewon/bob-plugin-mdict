package service_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

func syntheticCaseHTML(headword, definition string, examples ...string) string {
	var builder strings.Builder
	builder.WriteString("<article><h1>")
	builder.WriteString(headword)
	builder.WriteString(`</h1><div class="sense"><span class="pos">noun</span><span class="definition">`)
	builder.WriteString(definition)
	builder.WriteString("</span>")
	for _, example := range examples {
		builder.WriteString(`<span class="example">`)
		builder.WriteString(example)
		builder.WriteString("</span>")
	}
	builder.WriteString("</div></article>")
	return builder.String()
}

func newSyntheticCaseService(t *testing.T, entries []testmdx.Entry) *service.Service {
	t.Helper()
	root := t.TempDir()
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), entries); err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(config.Config{DictionaryDir: root, CacheDir: t.TempDir(), Port: 15321})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		t.Fatal(err)
	}
	return svc
}

func lookupSynthetic(t *testing.T, svc *service.Service, query string, opts service.LookupOptions) *service.Match {
	t.Helper()
	opts.Limit = 1
	result, err := svc.Lookup(query, opts)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", query, err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("Lookup(%q) matches = %d", query, len(result.Matches))
	}
	return &result.Matches[0]
}

func firstDefinition(match *service.Match) string {
	if len(match.Entry.Parts) == 0 || len(match.Entry.Parts[0].Senses) == 0 {
		return ""
	}
	return match.Entry.Parts[0].Senses[0].Definition
}

func TestServiceCachePreservesCaseSensitiveEntryIdentity(t *testing.T) {
	entries := []testmdx.Entry{
		{Key: "China", HTML: syntheticCaseHTML("China", "synthetic country definition")},
		{Key: "china", HTML: syntheticCaseHTML("china", "synthetic material definition")},
	}
	orders := []struct {
		name    string
		queries []string
		keys    []string
		defs    []string
	}{
		{
			name: "title case first", queries: []string{"China", "china", "China"},
			keys: []string{"China", "china", "China"},
			defs: []string{"synthetic country definition", "synthetic material definition", "synthetic country definition"},
		},
		{
			name: "lowercase first", queries: []string{"china", "China", "china"},
			keys: []string{"china", "China", "china"},
			defs: []string{"synthetic material definition", "synthetic country definition", "synthetic material definition"},
		},
	}
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			svc := newSyntheticCaseService(t, entries)
			var firstEntry any
			for i, query := range order.queries {
				match := lookupSynthetic(t, svc, query, service.LookupOptions{Mode: service.ModeExact})
				if match.Entry.Source.MatchedKey != order.keys[i] || firstDefinition(match) != order.defs[i] {
					t.Errorf("lookup %d %q = key %q definition %q, want %q %q", i, query,
						match.Entry.Source.MatchedKey, firstDefinition(match), order.keys[i], order.defs[i])
				}
				if i == 0 {
					firstEntry = match.Entry
				}
				if i == 2 && firstEntry != match.Entry {
					t.Error("third lookup did not return its own cached Entry")
				}
			}
		})
	}
}

func TestServiceCaseFallbackAndUnicodeCanonicalIdentity(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{
		{Key: "China", HTML: syntheticCaseHTML("China", "synthetic country definition")},
		{Key: "Café", HTML: syntheticCaseHTML("Café", "synthetic title-case accent definition")},
		{Key: "café", HTML: syntheticCaseHTML("café", "synthetic lowercase accent definition")},
	})

	if match := lookupSynthetic(t, svc, "china", service.LookupOptions{}); match.Entry.Source.MatchedKey != "China" {
		t.Fatalf("case fallback matched %q, want China", match.Entry.Source.MatchedKey)
	}
	for _, tc := range []struct{ query, key string }{
		{"Café", "Café"}, {"Cafe\u0301", "Café"}, {"café", "café"}, {"cafe\u0301", "café"},
	} {
		if match := lookupSynthetic(t, svc, tc.query, service.LookupOptions{}); match.Entry.Source.MatchedKey != tc.key {
			t.Errorf("Lookup(%q) matched %q, want %q", tc.query, match.Entry.Source.MatchedKey, tc.key)
		}
	}
}

func TestSmartSuggestionsPreserveCaseDistinctHeadwords(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{
		{Key: "Polish", HTML: syntheticCaseHTML("Polish", "synthetic proper adjective")},
		{Key: "polish", HTML: syntheticCaseHTML("polish", "synthetic verb")},
		{Key: "Café", HTML: syntheticCaseHTML("Café", "synthetic NFC")},
		{Key: "Cafe\u0301", HTML: syntheticCaseHTML("Cafe\u0301", "synthetic NFD")},
	})
	result, err := svc.Lookup("polis", service.LookupOptions{Mode: service.ModeSmart})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 0 || len(result.Suggestions) != 2 || result.Suggestions[0] != "Polish" || result.Suggestions[1] != "polish" {
		t.Fatalf("case-distinct suggestions = %+v", result)
	}

	result, err = svc.Lookup("caf", service.LookupOptions{Mode: service.ModeSmart})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("canonically equivalent suggestions were not deduplicated: %v", result.Suggestions)
	}
}

func TestEntryCacheStillSeparatesParserOptions(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{{
		Key: "China", HTML: syntheticCaseHTML("China", "synthetic country definition", "Example A", "Example B", "Example C"),
	}})
	one := lookupSynthetic(t, svc, "China", service.LookupOptions{MaxExamples: 1})
	two := lookupSynthetic(t, svc, "China", service.LookupOptions{MaxExamples: 2})
	debug := lookupSynthetic(t, svc, "China", service.LookupOptions{MaxExamples: 2, Debug: true})
	if got := len(one.Entry.Parts[0].Senses[0].Examples); got != 1 {
		t.Fatalf("MaxExamples=1 returned %d", got)
	}
	if got := len(two.Entry.Parts[0].Senses[0].Examples); got != 2 {
		t.Fatalf("MaxExamples=2 returned %d", got)
	}
	if len(two.Entry.Notes) != 0 || len(debug.Entry.Notes) == 0 {
		t.Fatalf("debug cache isolation failed: normal=%v debug=%v", two.Entry.Notes, debug.Entry.Notes)
	}
}

func TestBobPresentationOptionsDoNotBelongInEntryCacheIdentity(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{{
		Key: "China", HTML: syntheticCaseHTML("China", "synthetic country definition", "Example A"),
	}})
	hidden := bobadapter.DefaultOptions()
	hidden.IncludeExamples = false
	visible := bobadapter.DefaultOptions()

	first, err := svc.Lookup("China", service.LookupOptions{Limit: 1, RenderBob: true, BobOptions: hidden})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Lookup("China", service.LookupOptions{Limit: 1, RenderBob: true, BobOptions: visible})
	if err != nil {
		t.Fatal(err)
	}
	if first.Bob == nil || second.Bob == nil || len(first.Bob.Additions) != 0 || len(second.Bob.Additions) != 1 {
		t.Fatalf("Bob-only options polluted cached Entry rendering: hidden=%+v visible=%+v", first.Bob, second.Bob)
	}
	if first.Matches[0].Entry != second.Matches[0].Entry {
		t.Fatal("Bob-only presentation options unnecessarily split the Entry cache")
	}
}
