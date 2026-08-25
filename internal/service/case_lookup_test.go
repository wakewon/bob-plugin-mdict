package service_test

import (
	"encoding/json"
	"errors"
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

func syntheticMultiHTML(headword, pos, ipa, definition string) string {
	return `<article><h1>` + headword + `</h1><span class="pron">/` + ipa +
		`/</span><div class="sense"><span class="pos">` + pos +
		`</span><span class="definition">` + definition + `</span>` +
		`<span class="example">synthetic ` + pos + ` example</span></div>` +
		`<span class="form">synthetic-` + pos + `</span>` +
		`<span class="xref">synthetic-` + pos + `-related</span></article>`
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
	if len(match.Records) == 0 || match.Records[0].Entry == nil || len(match.Records[0].Entry.Parts) == 0 || len(match.Records[0].Entry.Parts[0].Senses) == 0 {
		return ""
	}
	return match.Records[0].Entry.Parts[0].Senses[0].Definition
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
				entry := match.Records[0].Entry
				if entry.Source.MatchedKey != order.keys[i] || firstDefinition(match) != order.defs[i] {
					t.Errorf("lookup %d %q = key %q definition %q, want %q %q", i, query,
						entry.Source.MatchedKey, firstDefinition(match), order.keys[i], order.defs[i])
				}
				if i == 0 {
					firstEntry = entry
				}
				if i == 2 && firstEntry != entry {
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

	if match := lookupSynthetic(t, svc, "china", service.LookupOptions{}); match.Records[0].Entry.Source.MatchedKey != "China" {
		t.Fatalf("case fallback matched %q, want China", match.Records[0].Entry.Source.MatchedKey)
	}
	for _, tc := range []struct{ query, key string }{
		{"Café", "Café"}, {"Cafe\u0301", "Café"}, {"café", "café"}, {"cafe\u0301", "café"},
	} {
		if match := lookupSynthetic(t, svc, tc.query, service.LookupOptions{}); match.Records[0].Entry.Source.MatchedKey != tc.key {
			t.Errorf("Lookup(%q) matched %q, want %q", tc.query, match.Records[0].Entry.Source.MatchedKey, tc.key)
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
	if got := len(one.Records[0].Entry.Parts[0].Senses[0].Examples); got != 1 {
		t.Fatalf("MaxExamples=1 returned %d", got)
	}
	if got := len(two.Records[0].Entry.Parts[0].Senses[0].Examples); got != 2 {
		t.Fatalf("MaxExamples=2 returned %d", got)
	}
	if len(two.Records[0].Entry.Notes) != 0 || len(debug.Records[0].Entry.Notes) == 0 {
		t.Fatalf("debug cache isolation failed: normal=%v debug=%v", two.Records[0].Entry.Notes, debug.Records[0].Entry.Notes)
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
	if first.Matches[0].Records[0].Entry != second.Matches[0].Records[0].Entry {
		t.Fatal("Bob-only presentation options unnecessarily split the Entry cache")
	}
}

func TestServiceLookupBuildsStableMultiRecordEntrySet(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{
		{Key: "flimber", HTML: syntheticMultiHTML("flimber", "noun", "ˈælfə", "synthetic noun definition")},
		{Key: "flimber", HTML: `<div class="technical"></div>`},
		{Key: "flimber", HTML: syntheticMultiHTML("flimber", "verb", "ˈbeɪtə", "synthetic verb definition")},
		{Key: "flimber", HTML: syntheticMultiHTML("flimber", "adjective", "ˈɡæmə", "synthetic adjective definition")},
	})
	combined := bobadapter.DefaultOptions()
	combined.MultiRecordMode = bobadapter.MultiRecordCombined
	result, err := svc.Lookup("flimber", service.LookupOptions{
		Limit: 1, RenderBob: true, BobOptions: combined, Debug: true,
	})
	if err != nil || len(result.Matches) != 1 {
		t.Fatalf("Lookup: matches=%d err=%v", len(result.Matches), err)
	}
	match := &result.Matches[0]
	if match.Headword != "flimber" || len(match.Records) != 3 {
		t.Fatalf("entry set = %+v", match)
	}
	for index, wantPOS := range []string{"noun", "verb", "adjective"} {
		record := match.Records[index]
		if record.RecordOrdinal != index+1 || record.Entry.Parts[0].POS != wantPOS {
			t.Errorf("record %d = %+v, want visible ordinal %d %s", index, record, index+1, wantPOS)
		}
	}
	if match.Records[0].Entry.Source.RawRecordOrdinal != 1 ||
		match.Records[1].Entry.Source.RawRecordOrdinal != 3 ||
		match.Records[2].Entry.Source.RawRecordOrdinal != 4 {
		t.Fatalf("raw provenance was renumbered: %+v", match.Records)
	}
	if result.Bob == nil || len(result.Bob.Parts) != 3 ||
		result.Bob.Parts[0].Part != "¹ noun" || result.Bob.Parts[1].Part != "² verb" || result.Bob.Parts[2].Part != "³ adjective" {
		t.Fatalf("multi-record Bob parts = %+v", result.Bob)
	}
	if len(result.Bob.Phonetics) != 3 || result.Bob.Phonetics[0].Value != "ˈælfə · 未标口音 · ¹" ||
		result.Bob.Phonetics[1].Value != "ˈbeɪtə · 未标口音 · ²" {
		t.Fatalf("multi-record Bob phonetics = %+v", result.Bob.Phonetics)
	}
	names := make(map[string]bool)
	for _, addition := range result.Bob.Additions {
		names[addition.Name] = true
	}
	for _, name := range []string{"Examples · ¹ noun 1", "Examples · ² verb 1", "Examples · ³ adjective 1"} {
		if !names[name] {
			t.Errorf("missing %q in %+v", name, result.Bob.Additions)
		}
	}

	second, err := svc.Lookup("flimber", service.LookupOptions{Limit: 1, Debug: true})
	if err != nil || second.Matches[0].Records[0].Entry != match.Records[0].Entry ||
		second.Matches[0].Records[2].Entry != match.Records[2].Entry {
		t.Fatal("cache hit reparsed or lost the complete EntrySet")
	}
	for _, ordinal := range []int{2, 3, 1} {
		selectedOpts := bobadapter.DefaultOptions()
		selectedOpts.RecordOrdinal = ordinal
		selected, selectErr := svc.Lookup("flimber", service.LookupOptions{
			Limit: 1, RenderBob: true, BobOptions: selectedOpts, Debug: true,
		})
		if selectErr != nil || selected.Bob == nil || selected.Bob.Word != "flimber"+[]string{"", "¹", "²", "³"}[ordinal] {
			t.Fatalf("select ordinal %d: bob=%+v err=%v", ordinal, selected.Bob, selectErr)
		}
		if selected.Matches[0].Records[0].Entry != match.Records[0].Entry ||
			selected.Matches[0].Records[2].Entry != match.Records[2].Entry {
			t.Fatalf("select ordinal %d reparsed the cached EntrySet", ordinal)
		}
	}
	outOfRange := bobadapter.DefaultOptions()
	outOfRange.RecordOrdinal = 4
	_, err = svc.Lookup("flimber", service.LookupOptions{Limit: 1, RenderBob: true, BobOptions: outOfRange, Debug: true})
	if !errors.Is(err, service.ErrRecordNotFound) {
		t.Fatalf("out-of-range error = %v, want ErrRecordNotFound", err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Matches []map[string]json.RawMessage `json:"matches"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || len(envelope.Matches) != 1 {
		t.Fatalf("decode v2 payload: %v, %s", err, payload)
	}
	if _, legacy := envelope.Matches[0]["entry"]; legacy {
		t.Fatalf("v2 payload retained legacy matches[].entry: %s", payload)
	}
	if _, ok := envelope.Matches[0]["records"]; !ok {
		t.Fatalf("v2 payload omitted matches[].records: %s", payload)
	}
}

func TestServiceFiltersExactAndRedirectDuplicatesWithoutVisibleOrdinal(t *testing.T) {
	tests := []struct {
		name    string
		entries []testmdx.Entry
	}{
		{
			name: "byte-identical exact records",
			entries: []testmdx.Entry{
				{Key: "flimber", HTML: syntheticCaseHTML("flimber", "synthetic shared definition")},
				{Key: "flimber", HTML: syntheticCaseHTML("flimber", "synthetic shared definition")},
			},
		},
		{
			name: "two redirects to the same target",
			entries: []testmdx.Entry{
				{Key: "flimber", HTML: "@@@LINK=target\x00"},
				{Key: "flimber", HTML: "@@@LINK=target\x00"},
				{Key: "target", HTML: syntheticCaseHTML("target", "synthetic redirected definition")},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSyntheticCaseService(t, tc.entries)
			result, err := svc.Lookup("flimber", service.LookupOptions{
				Limit: 1, RenderBob: true, BobOptions: bobadapter.DefaultOptions(),
			})
			if err != nil || len(result.Matches) != 1 || len(result.Matches[0].Records) != 1 {
				t.Fatalf("filtered result = %+v err=%v", result, err)
			}
			payload, _ := json.Marshal(result.Bob)
			if strings.Contains(string(payload), "¹") {
				t.Fatalf("single visible record exposed ordinal: %s", payload)
			}
		})
	}
}

func TestServiceMultiRecordCaseResolutionDoesNotMixSpellings(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{
		{Key: "China", HTML: syntheticCaseHTML("China", "title definition A")},
		{Key: "China", HTML: syntheticCaseHTML("China", "title definition B")},
		{Key: "china", HTML: syntheticCaseHTML("china", "lower definition C")},
		{Key: "china", HTML: syntheticCaseHTML("china", "lower definition D")},
	})
	for _, tc := range []struct {
		query, matched, prefix string
	}{
		{"China", "China", "title"},
		{"china", "china", "lower"},
		{"CHINA", "china", "lower"},
	} {
		match := lookupSynthetic(t, svc, tc.query, service.LookupOptions{})
		if len(match.Records) != 2 {
			t.Fatalf("Lookup(%q) records = %d", tc.query, len(match.Records))
		}
		for _, record := range match.Records {
			if record.Entry.Source.MatchedKey != tc.matched || !strings.HasPrefix(record.Entry.Parts[0].Senses[0].Definition, tc.prefix) {
				t.Errorf("Lookup(%q) mixed spelling groups: %+v", tc.query, match.Records)
			}
		}
		selectedOpts := bobadapter.DefaultOptions()
		selectedOpts.RecordOrdinal = 2
		selected, err := svc.Lookup(tc.query, service.LookupOptions{Limit: 1, RenderBob: true, BobOptions: selectedOpts})
		if err != nil || selected.Bob == nil || !strings.HasPrefix(selected.Bob.Parts[0].Means[0], "1. "+tc.prefix) {
			t.Errorf("Lookup(%q) selector crossed case group: bob=%+v err=%v", tc.query, selected.Bob, err)
		}
	}
}

func TestServiceRecordSelectionKeepsUnicodeCanonicalLookup(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{
		{Key: "Café", HTML: syntheticCaseHTML("Café", "NFC first")},
		{Key: "Café", HTML: syntheticCaseHTML("Café", "NFC second")},
	})
	opts := bobadapter.DefaultOptions()
	opts.RecordOrdinal = 2
	result, err := svc.Lookup("Cafe\u0301", service.LookupOptions{Limit: 1, RenderBob: true, BobOptions: opts})
	if err != nil || result.Bob == nil {
		t.Fatalf("NFD selector base lookup: result=%+v err=%v", result, err)
	}
	if result.Query != "Café" || result.Bob.Word != "Café²" ||
		result.Matches[0].Records[1].Entry.Source.MatchedKey != "Café" {
		t.Fatalf("NFD selection changed canonical facts: %+v", result)
	}
}

func TestServiceNavigationAliasUsesCanonicalBaseQueryNotParsedTitle(t *testing.T) {
	svc := newSyntheticCaseService(t, []testmdx.Entry{
		{Key: "lead", HTML: syntheticCaseHTML("lead1", "first record")},
		{Key: "lead", HTML: syntheticCaseHTML("lead2", "second record")},
	})
	opts := bobadapter.DefaultOptions()
	opts.RecordOrdinal = 2
	result, err := svc.Lookup("lead", service.LookupOptions{Limit: 1, RenderBob: true, BobOptions: opts})
	if err != nil || result.Bob == nil {
		t.Fatalf("navigation alias lookup: result=%+v err=%v", result, err)
	}
	if result.Bob.Word != "lead²" || result.Matches[0].Records[1].Entry.Headword != "lead2" ||
		result.Matches[0].Records[1].Entry.Source.MatchedKey != "lead" {
		t.Fatalf("navigation alias polluted or ignored dictionary facts: %+v", result)
	}
}
