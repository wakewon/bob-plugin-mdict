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
		"PHRASE": "", "exclamation": "interjection", "modal verb": "modal verb",
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

func TestClassifySemanticLabel(t *testing.T) {
	cases := map[string]SemanticLabel{
		"See also": LabelCrossReference, "Cross-reference": LabelCrossReference,
		"Related": LabelRelated, "PHRASE": LabelPhrase, "Idiom": LabelIdiom,
		"Phrasal verb": LabelPhrasalVerb, "Derivatives": LabelDerivative,
		"Synonym / Antonym section": LabelSynonyms, "noun": "", "PARTICLE-X": "",
	}
	for input, want := range cases {
		if got := ClassifySemanticLabel(input); got != want {
			t.Errorf("ClassifySemanticLabel(%q) = %q, want %q", input, got, want)
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
		// The IPA close front rounded vowel is also the twenty-fifth letter of
		// the English alphabet. Reading it as evidence turned every ordinary
		// word ending in one into a transcription, and with it every heading,
		// label and section title in a corpus of a hundred dictionaries.
		"necessary", "1.1 Subjectivity", "especially BrE", "Word Frequency",
		// The same story in French and Danish orthography.
		"façade", "encyclopædia", "Fußgängerübergang",
	}
	for _, value := range negative {
		if LooksLikeIPA(value) {
			t.Errorf("LooksLikeIPA(%q) = true, want false", value)
		}
	}

	// A transcription written only in letters that ordinary orthography also
	// uses is still recognisable when the dictionary delimits it as one.
	for _, value := range []string{"/føt/", "[çyː]"} {
		if !LooksLikeIPA(value) {
			t.Errorf("LooksLikeIPA(%q) = false, want true", value)
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
		{"uk path", []string{"sound://synthetic/uk/clip.mp3"}, entryir.RegionUK},
		{"us path", []string{"sound://synthetic/us/clip.mp3"}, entryir.RegionUS},
		{"breprons path", []string{"sound://synthetic/breprons/clip.mp3"}, entryir.RegionUK},
		{"ameprons path", []string{"sound://synthetic/ameprons/clip.mp3"}, entryir.RegionUS},
		{"collins type class", []string{"pron type_uk"}, entryir.RegionUK},
		// No evidence at all must stay unclassified rather than defaulting.
		{"no evidence", []string{"sound://synthetic/audio/clip.mp3"}, entryir.RegionOther},
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
