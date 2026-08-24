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
			{IPARegion: entryir.RegionUK, IPA: "ˈflɪmbə", AudioRegion: entryir.RegionUK, Audio: audio("http://127.0.0.1:15321/v1/resource/UK")},
			{IPARegion: entryir.RegionUS, IPA: "ˈflɪmbər", AudioRegion: entryir.RegionUS, Audio: audio("http://127.0.0.1:15321/v1/resource/US")},
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
	dict := Render(sampleEntry(), DefaultOptions())
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

	if len(dict.Parts) != 2 || dict.Parts[0].Part != "verb [transitive]" || dict.Parts[1].Part != "verb [transitive]" {
		t.Fatalf("parts = %+v", dict.Parts)
	}
	means := dict.Parts[0].Means
	if len(means) != 2 {
		t.Fatalf("first sense means = %d lines, want parent plus subsense: %v", len(means), means)
	}
	if !strings.Contains(means[0], "1.") || !strings.Contains(means[0], "(informal)") ||
		!strings.Contains(means[0], "[SHAPING]") || !strings.Contains(means[0], "抹平黏土") {
		t.Errorf("sense line lost detail: %q", means[0])
	}
	// The hierarchy has to survive being flattened into strings: a subsense
	// follows its own parent, indented, not the whole sense list.
	if !strings.HasPrefix(means[1], "1.1.") {
		t.Errorf("subsense has no generated hierarchical number: %q", means[1])
	}
	if got := dict.Parts[1].Means; len(got) != 1 || !strings.HasPrefix(got[0], "2.") {
		t.Errorf("sense 2 should be a separate repeated Bob part: %+v", dict.Parts[1])
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
		Pronunciations: []entryir.Pronunciation{{IPARegion: entryir.RegionOther, IPA: "ˈflɪmbə", AudioRegion: entryir.RegionOther, Audio: audio("http://127.0.0.1:1/x")}},
		Parts:          []entryir.Part{{POS: "noun", Senses: []entryir.Sense{{Definition: "a tool"}}}},
	}
	dict := Render(entry, DefaultOptions())
	if len(dict.Phonetics) != 1 || dict.Phonetics[0].Type != "uk" {
		t.Fatalf("phonetics = %+v", dict.Phonetics)
	}
	if dict.Phonetics[0].Value != "ˈflɪmbə · 未标口音" {
		t.Fatalf("unknown annotation should follow IPA: %+v", dict.Phonetics[0])
	}
	if entry.Pronunciations[0].IPARegion != entryir.RegionOther || entry.Pronunciations[0].AudioRegion != entryir.RegionOther {
		t.Fatal("Bob carrier mapping mutated unknown provenance in the IR")
	}
}

// TestRegionalPronunciationIsNeverOverwritten guards against the unlabelled
// fallback stealing a slot that has real evidence behind it.
func TestRegionalPronunciationIsNeverOverwritten(t *testing.T) {
	entry := &entryir.Entry{
		Headword: "flimber",
		Pronunciations: []entryir.Pronunciation{
			{IPARegion: entryir.RegionOther, IPA: "WRONG", AudioRegion: entryir.RegionOther, Audio: audio("http://127.0.0.1:1/other")},
			{IPARegion: entryir.RegionUK, IPA: "RIGHT", AudioRegion: entryir.RegionUK, Audio: audio("http://127.0.0.1:1/uk")},
		},
		Parts: []entryir.Part{{POS: "noun", Senses: []entryir.Sense{{Definition: "a tool"}}}},
	}
	dict := Render(entry, DefaultOptions())
	if len(dict.Phonetics) != 2 || dict.Phonetics[0].Value != "RIGHT" {
		t.Fatalf("phonetics = %+v", dict.Phonetics)
	}
}

func TestDisplayNumbersResetPerPOSWithoutMutatingSource(t *testing.T) {
	entry := &entryir.Entry{Headword: "flimber", Parts: []entryir.Part{
		{POS: "verb", Senses: []entryir.Sense{{Number: "3", Definition: "first"}, {Number: "4", Definition: "second"}}},
		{POS: "noun", Senses: []entryir.Sense{{Number: "5", Definition: "tool"}}},
	}}
	dict := Render(entry, DefaultOptions())
	if len(dict.Parts) != 3 {
		t.Fatalf("parts = %+v, want one Bob part per top-level sense", dict.Parts)
	}
	want := []struct {
		part   string
		prefix string
	}{{"verb", "1."}, {"verb", "2."}, {"noun", "1."}}
	for i, expected := range want {
		if dict.Parts[i].Part != expected.part || len(dict.Parts[i].Means) != 1 || !strings.HasPrefix(dict.Parts[i].Means[0], expected.prefix) {
			t.Errorf("part %d = %+v, want %s %s", i, dict.Parts[i], expected.part, expected.prefix)
		}
	}
	if entry.Parts[0].Senses[0].Number != "3" || entry.Parts[1].Senses[0].Number != "5" {
		t.Fatal("source numbering was mutated by presentation rendering")
	}
}

func TestSubsensesRemainWithTheirTopLevelSense(t *testing.T) {
	entry := &entryir.Entry{Headword: "flimber", Parts: []entryir.Part{{POS: "verb", Senses: []entryir.Sense{
		{Definition: "parent", Subsenses: []entryir.Sense{{Definition: "first child"}, {Definition: "second child"}}},
		{Definition: "another parent"},
	}}}}
	dict := Render(entry, DefaultOptions())
	if len(dict.Parts) != 2 {
		t.Fatalf("parts = %+v, want two top-level sense blocks", dict.Parts)
	}
	if got := dict.Parts[0].Means; len(got) != 3 || !strings.HasPrefix(got[0], "1.") ||
		!strings.HasPrefix(got[1], "1.1.") || !strings.HasPrefix(got[2], "1.2.") {
		t.Fatalf("subsenses were split away from parent: %v", got)
	}
	if got := dict.Parts[1].Means; len(got) != 1 || !strings.HasPrefix(got[0], "2.") {
		t.Fatalf("second parent = %v", got)
	}
}

func TestExamplesAreGroupedOncePerDisplaySense(t *testing.T) {
	entry := &entryir.Entry{Headword: "flimber", Parts: []entryir.Part{{POS: "verb", Senses: []entryir.Sense{
		{Number: "7", Definition: "first", Examples: []entryir.Example{{Text: "Example A", Translation: "译文甲"}, {Text: "Example B"}}},
		{Number: "9", Definition: "second", Examples: []entryir.Example{{Text: "Example C"}}},
	}}}}
	dict := Render(entry, DefaultOptions())
	if len(dict.Additions) != 1 {
		t.Fatalf("additions = %+v", dict.Additions)
	}
	value := dict.Additions[0].Value
	for _, line := range strings.Split(value, "\n") {
		if line == "1" || line == "2" {
			t.Fatalf("example group still uses a standalone number: %q", value)
		}
	}
	for _, want := range []string{"释义 1", "释义 2", "• Example A\n  — 译文甲", "• Example B", "• Example C"} {
		if !strings.Contains(value, want) {
			t.Errorf("grouped examples missing %q: %q", want, value)
		}
	}
	if strings.Contains(value, "义项") || strings.Contains(value, "Sense ") || strings.Contains(value, "【") || strings.Contains(value, "〔") {
		t.Fatalf("example headings use unwanted terminology or decoration: %q", value)
	}
	if strings.Contains(value, "1. Example") || strings.Contains(value, "2. Example") {
		t.Fatalf("examples still look individually misnumbered: %q", value)
	}
}

func TestSharedIPAUsesRegionalAudioWithoutChangingProvenance(t *testing.T) {
	entry := &entryir.Entry{Headword: "flimber", Pronunciations: []entryir.Pronunciation{
		{IPARegion: entryir.RegionNeutral, IPA: "ˈflɪmbə"},
		{AudioRegion: entryir.RegionUK, Audio: audio("http://127.0.0.1/uk")},
		{AudioRegion: entryir.RegionUS, Audio: audio("http://127.0.0.1/us")},
	}}
	dict := Render(entry, DefaultOptions())
	if len(dict.Phonetics) != 2 {
		t.Fatalf("phonetics = %+v", dict.Phonetics)
	}
	for _, phonetic := range dict.Phonetics {
		if phonetic.Value != "ˈflɪmbə · 共用音标" || phonetic.TTS == nil {
			t.Errorf("shared IPA carrier lost information: %+v", phonetic)
		}
	}
	if entry.Pronunciations[0].IPARegion != entryir.RegionNeutral {
		t.Fatal("neutral IPA was promoted inside IR")
	}
}

func TestAudioOnlyUnknownUsesResultLevelNote(t *testing.T) {
	entry := &entryir.Entry{Headword: "flimber", Pronunciations: []entryir.Pronunciation{{AudioRegion: entryir.RegionOther, Audio: audio("http://127.0.0.1/audio")}}}
	dict := Render(entry, DefaultOptions())
	if len(dict.Phonetics) != 1 || dict.Phonetics[0].TTS == nil || dict.Phonetics[0].Value != "" {
		t.Fatalf("audio-only pronunciation = %+v", dict.Phonetics)
	}
	if len(dict.Additions) != 1 || dict.Additions[0].Name != "发音说明" || !strings.Contains(dict.Additions[0].Value, "未标注") {
		t.Fatalf("pronunciation note = %+v", dict.Additions)
	}
}

func TestExtrasCanBeDisabled(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludeExtras = false
	opts.IncludeExamples = false
	dict := Render(sampleEntry(), opts)
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
