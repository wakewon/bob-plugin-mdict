package mdict

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

func syntheticDictionary(t *testing.T, entries []testmdx.Entry) *Dictionary {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case.mdx")
	if err := testmdx.Write(path, entries); err != nil {
		t.Fatal(err)
	}
	dict := &Dictionary{mdxPath: path, dirName: "synthetic", info: Info{ID: "synthetic-case"}}
	if err := dict.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dict.Close)
	return dict
}

func TestNormalizeExactKeyPreservesCaseAndCanonicalizesUnicode(t *testing.T) {
	if got := NormalizeExactKey("  China  "); got != "China" {
		t.Fatalf("NormalizeExactKey = %q", got)
	}
	if NormalizeExactKey("China") == NormalizeExactKey("china") {
		t.Fatal("exact identity folded letter case")
	}
	if got := NormalizeExactKey("Cafe\u0301"); got != "Café" {
		t.Fatalf("NFD normalization = %q, want NFC café spelling", got)
	}
}

func TestLookupPrefersExactCaseBeforeDeterministicFallback(t *testing.T) {
	dict := syntheticDictionary(t, []testmdx.Entry{
		{Key: "China", HTML: "synthetic country record"},
		{Key: "china", HTML: "synthetic material record"},
		{Key: "Café", HTML: "synthetic title-case accent record"},
		{Key: "café", HTML: "synthetic lowercase accent record"},
	})

	for _, tc := range []struct {
		query, key, content string
	}{
		{"China", "China", "synthetic country record"},
		{"china", "china", "synthetic material record"},
		// No exact ALL-CAPS key exists. The lowercase exact key is the stable,
		// deliberately simple fallback winner.
		{"CHINA", "china", "synthetic material record"},
		{"Cafe\u0301", "Café", "synthetic title-case accent record"},
		{"cafe\u0301", "café", "synthetic lowercase accent record"},
	} {
		result, err := dict.Lookup(tc.query)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", tc.query, err)
		}
		if result.MatchedKey != tc.key || string(result.HTML) != tc.content {
			t.Errorf("Lookup(%q) = key %q content %q, want %q %q",
				tc.query, result.MatchedKey, result.HTML, tc.key, tc.content)
		}
	}
}

func TestLookupStillFallsBackWhenOnlyOtherCaseExists(t *testing.T) {
	dict := syntheticDictionary(t, []testmdx.Entry{{Key: "China", HTML: "synthetic country record"}})
	result, err := dict.Lookup("china")
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedKey != "China" {
		t.Fatalf("fallback matched %q, want China", result.MatchedKey)
	}
}

func TestRedirectCyclesUseActualCaseSensitiveMatchedKeys(t *testing.T) {
	t.Run("case-distinct chain", func(t *testing.T) {
		dict := syntheticDictionary(t, []testmdx.Entry{
			{Key: "FOO", HTML: "synthetic final record"},
			{Key: "Foo", HTML: "@@@LINK=foo\x00"},
			{Key: "foo", HTML: "@@@LINK=FOO\x00"},
		})
		result, err := dict.Lookup("Foo")
		if err != nil {
			t.Fatal(err)
		}
		if result.MatchedKey != "FOO" || result.RedirectedFrom != "Foo" {
			t.Fatalf("redirect result = %+v", result)
		}
	})

	t.Run("true case-sensitive cycle", func(t *testing.T) {
		dict := syntheticDictionary(t, []testmdx.Entry{
			{Key: "Foo", HTML: "@@@LINK=foo\x00"},
			{Key: "foo", HTML: "@@@LINK=Foo\x00"},
		})
		if _, err := dict.Lookup("Foo"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cycle error = %v, want ErrNotFound", err)
		}
	})

	t.Run("fallback resolves to visited key", func(t *testing.T) {
		dict := syntheticDictionary(t, []testmdx.Entry{{Key: "A", HTML: "@@@LINK=a\x00"}})
		if _, err := dict.Lookup("A"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("fallback cycle error = %v, want ErrNotFound", err)
		}
	})
}

func TestPrefixMatchesCaseInsensitivelyAndPreservesVariants(t *testing.T) {
	dict := syntheticDictionary(t, []testmdx.Entry{
		{Key: "Polish", HTML: "synthetic proper adjective"},
		{Key: "polish", HTML: "synthetic verb"},
		{Key: "policy", HTML: "synthetic unrelated prefix"},
	})
	if got := dict.Prefix("POLIS", 8); !reflect.DeepEqual(got, []string{"Polish", "polish"}) {
		t.Fatalf("Prefix = %v, want both original spellings", got)
	}
}

func TestLookupAllExpandsExactDuplicatesInSourceOrder(t *testing.T) {
	dict := syntheticDictionary(t, []testmdx.Entry{
		{Key: "flimber", HTML: "synthetic noun record"},
		{Key: "flimber", HTML: "@@@LINK=flimber-target\x00"},
		{Key: "flimber-target", HTML: "synthetic verb record"},
		{Key: "flimber-target", HTML: "synthetic adjective record"},
	})
	set, err := dict.LookupAll("flimber")
	if err != nil {
		t.Fatal(err)
	}
	if set.MatchedKey != "flimber" || len(set.Records) != 3 {
		t.Fatalf("LookupAll = %+v", set)
	}
	want := []struct {
		key, content, redirected string
		rawOrdinal               int
	}{
		{"flimber", "synthetic noun record", "", 1},
		{"flimber-target", "synthetic verb record", "flimber", 1},
		{"flimber-target", "synthetic adjective record", "flimber", 2},
	}
	for i, expected := range want {
		got := set.Records[i]
		if got.MatchedKey != expected.key || string(got.HTML) != expected.content ||
			got.RedirectedFrom != expected.redirected || got.RawRecordOrdinal != expected.rawOrdinal {
			t.Errorf("record %d = %+v, want %+v", i, got, expected)
		}
	}
	first, err := dict.Lookup("flimber")
	if err != nil || string(first.HTML) != "synthetic noun record" {
		t.Fatalf("Lookup convenience result = %+v, %v", first, err)
	}
}

func TestLookupAllDeduplicatesResolvedRedirectContentAndKeepsValidSiblings(t *testing.T) {
	dict := syntheticDictionary(t, []testmdx.Entry{
		{Key: "cycle", HTML: "@@@LINK=cycle\x00"},
		{Key: "cycle", HTML: "synthetic surviving sibling"},
		{Key: "duplicate", HTML: "@@@LINK=target\x00"},
		{Key: "duplicate", HTML: "@@@LINK=target\x00"},
		{Key: "target", HTML: "synthetic shared target"},
	})
	for _, tc := range []struct {
		query, content string
	}{
		{"duplicate", "synthetic shared target"},
		{"cycle", "synthetic surviving sibling"},
	} {
		set, err := dict.LookupAll(tc.query)
		if err != nil {
			t.Fatalf("LookupAll(%q): %v", tc.query, err)
		}
		if len(set.Records) != 1 || string(set.Records[0].HTML) != tc.content {
			t.Errorf("LookupAll(%q) = %+v", tc.query, set)
		}
	}
}

func TestLookupAllResolvesCaseBeforeDuplicateExpansion(t *testing.T) {
	dict := syntheticDictionary(t, []testmdx.Entry{
		{Key: "China", HTML: "title record A"},
		{Key: "China", HTML: "title record B"},
		{Key: "china", HTML: "lower record C"},
		{Key: "china", HTML: "lower record D"},
	})
	for _, tc := range []struct {
		query, matched string
		contents       []string
	}{
		{"China", "China", []string{"title record A", "title record B"}},
		{"china", "china", []string{"lower record C", "lower record D"}},
		{"CHINA", "china", []string{"lower record C", "lower record D"}},
	} {
		set, err := dict.LookupAll(tc.query)
		if err != nil {
			t.Fatalf("LookupAll(%q): %v", tc.query, err)
		}
		if set.MatchedKey != tc.matched || len(set.Records) != len(tc.contents) {
			t.Fatalf("LookupAll(%q) = %+v", tc.query, set)
		}
		for i, content := range tc.contents {
			if string(set.Records[i].HTML) != content {
				t.Errorf("LookupAll(%q) record %d = %q, want %q", tc.query, i, set.Records[i].HTML, content)
			}
		}
	}
}
