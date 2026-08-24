package bobadapter

import (
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

func audio(url string) *entryir.Audio {
	return &entryir.Audio{ResourceRef: "sound://x.mp3", URL: url, MIMEType: "audio/mpeg"}
}

func sampleEntry() *entryir.Entry {
	return &entryir.Entry{
		Headword: "flimber",
		Pronunciations: []entryir.Pronunciation{
			{Region: entryir.RegionUK, IPA: "ˈflɪmbə", Audio: audio("http://127.0.0.1:15321/v1/resource/UK")},
			{Region: entryir.RegionUS, IPA: "ˈflɪmbər", Audio: audio("http://127.0.0.1:15321/v1/resource/US")},
		},
		Parts: []entryir.Part{{
			POS:     "verb",
			Grammar: "[transitive]",
			Senses: []entryir.Sense{
				{
					Number: "1", Definition: "to smooth clay", Translation: "抹平黏土",
					Labels: []string{"informal"}, Topic: "SHAPING",
					Examples: []entryir.Example{{Text: "She flimbered it.", Translation: "她抹平了它。"}},
					Subsenses: []entryir.Sense{
						{Number: "1.1", Definition: "to work slowly"},
					},
				},
				{Number: "2", Definition: "to delay"},
			},
		}},
		Forms:        []entryir.Form{{Name: "past tense", Words: []string{"flimbered"}}},
		Idioms:       []entryir.PhraseEntry{{Phrase: "flimber about", Definition: "to waste time"}},
		PhrasalVerbs: []entryir.PhraseEntry{{Phrase: "flimber down"}},
		Synonyms:     []string{"smooth", "level"},
		Etymology:    "Invented for testing.",
	}
}

func TestRenderProducesBobShape(t *testing.T) {
	dict := Render([]Source{{DictionaryTitle: "Test Dictionary", Entry: sampleEntry()}}, DefaultOptions())
	if dict == nil {
		t.Fatal("Render returned nil")
	}
	if dict.Word != "flimber" {
		t.Errorf("word = %q", dict.Word)
	}

	// Bob defines exactly two phonetic types; anything else is invalid.
	if len(dict.Phonetics) != 2 {
		t.Fatalf("phonetics = %d, want 2", len(dict.Phonetics))
	}
	for _, phonetic := range dict.Phonetics {
		if phonetic.Type != "uk" && phonetic.Type != "us" {
			t.Errorf("phonetic type %q is not one Bob accepts", phonetic.Type)
		}
		if phonetic.TTS == nil || phonetic.TTS.Type != "url" {
			t.Errorf("phonetic %q has no url tts", phonetic.Type)
		}
		if !strings.HasPrefix(phonetic.TTS.Value, "http://127.0.0.1:") {
			t.Errorf("tts url %q does not point at the loopback service", phonetic.TTS.Value)
		}
	}
	if dict.Phonetics[0].TTS.Value == dict.Phonetics[1].TTS.Value {
		t.Error("uk and us share one audio url")
	}

	if len(dict.Parts) != 1 || dict.Parts[0].Part != "verb [transitive]" {
		t.Fatalf("parts = %+v", dict.Parts)
	}
	means := dict.Parts[0].Means
	if len(means) != 3 {
		t.Fatalf("means = %d lines, want 3 (two senses plus one subsense): %v", len(means), means)
	}
	if !strings.Contains(means[0], "1.") || !strings.Contains(means[0], "(informal)") ||
		!strings.Contains(means[0], "[SHAPING]") || !strings.Contains(means[0], "抹平黏土") {
		t.Errorf("sense line lost detail: %q", means[0])
	}
	// The hierarchy has to survive being flattened into strings: a subsense
	// follows its own parent, indented, not the whole sense list.
	if !strings.HasPrefix(means[1], "    1.1.") {
		t.Errorf("subsense is not indented and numbered: %q", means[1])
	}
	if !strings.HasPrefix(means[2], "2.") {
		t.Errorf("sense 2 should follow the subsense: %q", means[2])
	}

	if len(dict.Exchanges) != 1 || dict.Exchanges[0].Name != "past tense" {
		t.Errorf("exchanges = %+v", dict.Exchanges)
	}

	names := map[string]string{}
	for _, addition := range dict.Additions {
		names[addition.Name] = addition.Value
	}
	for _, want := range []string{"Examples · verb [transitive]", "Idioms", "Phrasal verbs", "Synonyms", "Origin"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing addition %q; have %v", want, keys(names))
		}
	}
	if !strings.Contains(names["Examples · verb [transitive]"], "她抹平了它。") {
		t.Error("example translation was dropped")
	}
}

// TestSingleUnlabelledPronunciationSurfaces documents the one case where an
// unclassified pronunciation is presented under "uk": Bob has no neutral slot,
// and dropping real dictionary audio entirely would be worse.
func TestSingleUnlabelledPronunciationSurfaces(t *testing.T) {
	entry := &entryir.Entry{
		Headword:       "flimber",
		Pronunciations: []entryir.Pronunciation{{Region: entryir.RegionOther, IPA: "ˈflɪmbə", Audio: audio("http://127.0.0.1:1/x")}},
		Parts:          []entryir.Part{{POS: "noun", Senses: []entryir.Sense{{Definition: "a tool"}}}},
	}
	dict := Render([]Source{{DictionaryTitle: "T", Entry: entry}}, DefaultOptions())
	if len(dict.Phonetics) != 1 || dict.Phonetics[0].Type != "uk" {
		t.Fatalf("phonetics = %+v", dict.Phonetics)
	}
}

// TestRegionalPronunciationIsNeverOverwritten guards against the unlabelled
// fallback stealing a slot that has real evidence behind it.
func TestRegionalPronunciationIsNeverOverwritten(t *testing.T) {
	entry := &entryir.Entry{
		Headword: "flimber",
		Pronunciations: []entryir.Pronunciation{
			{Region: entryir.RegionOther, IPA: "WRONG", Audio: audio("http://127.0.0.1:1/other")},
			{Region: entryir.RegionUK, IPA: "RIGHT", Audio: audio("http://127.0.0.1:1/uk")},
		},
		Parts: []entryir.Part{{POS: "noun", Senses: []entryir.Sense{{Definition: "a tool"}}}},
	}
	dict := Render([]Source{{DictionaryTitle: "T", Entry: entry}}, DefaultOptions())
	if len(dict.Phonetics) != 1 || dict.Phonetics[0].Value != "RIGHT" {
		t.Fatalf("phonetics = %+v", dict.Phonetics)
	}
}

// TestMultipleDictionariesStayLabelled checks that senses from two sources are
// never merged into one undifferentiated list.
func TestMultipleDictionariesStayLabelled(t *testing.T) {
	first := &entryir.Entry{Headword: "flimber", Parts: []entryir.Part{{POS: "verb", Senses: []entryir.Sense{{Definition: "first"}}}}}
	second := &entryir.Entry{Headword: "flimber", Parts: []entryir.Part{{POS: "verb", Senses: []entryir.Sense{{Definition: "second"}}}}}

	opts := DefaultOptions()
	opts.MultipleDictionaries = true
	dict := Render([]Source{
		{DictionaryTitle: "Dictionary One", Entry: first},
		{DictionaryTitle: "Dictionary Two", Entry: second},
	}, opts)

	if len(dict.Parts) != 2 {
		t.Fatalf("parts = %d, want 2 (one per dictionary): %+v", len(dict.Parts), dict.Parts)
	}
	for i, want := range []string{"Dictionary One · verb", "Dictionary Two · verb"} {
		if dict.Parts[i].Part != want {
			t.Errorf("part %d = %q, want %q", i, dict.Parts[i].Part, want)
		}
	}
}

func TestExtrasCanBeDisabled(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludeExtras = false
	opts.IncludeExamples = false
	dict := Render([]Source{{DictionaryTitle: "T", Entry: sampleEntry()}}, opts)
	if len(dict.Additions) != 0 {
		t.Errorf("additions = %+v, want none", dict.Additions)
	}
	if len(dict.Parts) == 0 {
		t.Error("disabling extras must not remove the definitions themselves")
	}
}

func TestRenderEmptyInput(t *testing.T) {
	if Render(nil, DefaultOptions()) != nil {
		t.Error("Render(nil) should be nil")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
