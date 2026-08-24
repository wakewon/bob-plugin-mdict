package parser

import (
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

func TestCanonicalPOS(t *testing.T) {
	cases := map[string]string{
		"verb": "verb", "VERB ": "verb", "v.": "verb", "vt.": "transitive verb",
		"noun": "noun", "N-COUNT": "noun", "N-SING": "noun", "V-ERG": "verb",
		"ADJ-GRADED": "adjective", "adj.": "adjective", "ADV": "adverb",
		"名词": "noun", "动词": "verb", "形容词": "adjective",
		"PHRASE": "phrase", "exclamation": "interjection", "modal verb": "modal verb",
		// Prose is not a part of speech and must not be coerced into one.
		"to leave someone behind": "",
		"":                        "",
		"See also":                "",
	}
	for input, want := range cases {
		if got := CanonicalPOS(input); got != want {
			t.Errorf("CanonicalPOS(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLooksLikeIPA(t *testing.T) {
	positive := []string{"əˈbændən", "/ˈflɪmbə/", "rʌn", "[ˌʌndəˈstænd]", "ˈband(ə)n"}
	for _, value := range positive {
		if !LooksLikeIPA(value) {
			t.Errorf("LooksLikeIPA(%q) = false, want true", value)
		}
	}
	negative := []string{
		"abandon", "to leave someone", "",
		// A long sentence containing one IPA-ish character is prose.
		"the symbol ə is called schwa and appears in many unstressed syllables",
	}
	for _, value := range negative {
		if LooksLikeIPA(value) {
			t.Errorf("LooksLikeIPA(%q) = true, want false", value)
		}
	}
}

func TestDetectRegion(t *testing.T) {
	cases := []struct {
		name        string
		descriptors []string
		want        entryir.Region
	}{
		{"british class", []string{"class=speaker brefile"}, entryir.RegionUK},
		{"american class", []string{"class=speaker amefile"}, entryir.RegionUS},
		{"uk path", []string{"sound://uk/hello__gb_1.mp3"}, entryir.RegionUK},
		{"us path", []string{"sound://us/hello__us_1.mp3"}, entryir.RegionUS},
		{"breprons path", []string{"sound://media/english/breprons/a.mp3"}, entryir.RegionUK},
		{"ameprons path", []string{"sound://media/english/ameprons/a.mp3"}, entryir.RegionUS},
		{"collins type class", []string{"pron type_uk"}, entryir.RegionUK},
		// No evidence at all must stay unclassified rather than defaulting.
		{"no evidence", []string{"sound://COLmp3/00016.mp3"}, entryir.RegionOther},
		{"empty", []string{""}, entryir.RegionOther},
		// "us" inside an unrelated word must not count as an American marker.
		{"false positive guard", []string{"thesaurus because campus"}, entryir.RegionOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectRegion(tc.descriptors...); got != tc.want {
				t.Errorf("DetectRegion(%v) = %q, want %q", tc.descriptors, got, tc.want)
			}
		})
	}
}

func TestCanonicalForm(t *testing.T) {
	cases := map[string]string{
		"plural": "plural", "past tense": "past tense", "past participle": "past participle",
		"3rd person singular present tense": "third person singular",
		"comparative":                       "comparative", "superlative": "superlative",
		"过去式": "past tense", "复数": "plural",
		"something else": "",
	}
	for input, want := range cases {
		if got := CanonicalForm(input); got != want {
			t.Errorf("CanonicalForm(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseSenseNumber(t *testing.T) {
	cases := map[string]string{
		"1": "1", "1. ": "1", "(3)": "3", "2.3": "2.3", "1.1": "1.1",
		"": "", "one": "", "1 to leave": "",
	}
	for input, want := range cases {
		if got := ParseSenseNumber(input); got != want {
			t.Errorf("ParseSenseNumber(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsKnownLabel(t *testing.T) {
	for _, value := range []string{"informal", "(literary)", "formal", "informal, disapproving"} {
		if !IsKnownLabel(value) {
			t.Errorf("IsKnownLabel(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "to leave someone behind", "abandon ship"} {
		if IsKnownLabel(value) {
			t.Errorf("IsKnownLabel(%q) = true, want false", value)
		}
	}
}
