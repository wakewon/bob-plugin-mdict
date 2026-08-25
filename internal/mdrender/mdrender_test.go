package mdrender_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
)

var update = flag.Bool("update", false, "rewrite golden Markdown expectations")

// Every fixture here is invented. The renderer is checked against dictionary
// shapes, not against dictionary content, so nothing in this file needs to
// come from a real publisher — and nothing does.

func audio(ref string) *entryir.Audio {
	return &entryir.Audio{
		ResourceRef: ref,
		Token:       "TOKEN",
		URL:         "http://127.0.0.1:15321/v2/resource/TOKEN",
		MIMEType:    "audio/mpeg",
	}
}

// wexal is an English monolingual entry: two parts of speech, numbered senses,
// subsenses, UK/US pronunciation, forms, idioms and cross-references.
func wexal() *entryir.EntrySet {
	return &entryir.EntrySet{
		LookupKey: "wexal",
		Headword:  "wex·al",
		Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: &entryir.Entry{
			Headword: "wex·al",
			Pronunciations: []entryir.Pronunciation{
				{IPARegion: entryir.RegionUK, IPA: "ˈwɛksl", AudioRegion: entryir.RegionUK,
					Audio: audio("snd://synthetic/wexal_gb.spx"), Confidence: 0.95, Rule: "profile:pron"},
				{IPARegion: entryir.RegionUS, IPA: "ˈwɛksəl", AudioRegion: entryir.RegionUS,
					Audio: audio("snd://synthetic/wexal_us.spx"), Confidence: 0.95, Rule: "profile:pron"},
			},
			Parts: []entryir.Part{
				{POS: "noun", Confidence: 0.9, Rule: "generic:markerBlocks", Senses: []entryir.Sense{
					{Number: "1", Definition: "a small hook used to fasten a sail",
						Labels: []string{"nautical"}, Confidence: 0.7, Rule: "generic:markerBlocks",
						Examples: []entryir.Example{{Text: "tie the rope to the wexal"}},
						Subsenses: []entryir.Sense{
							{Number: "1.1", Definition: "the hook and its fitting together", Rule: "generic:markerBlocks"},
							{Definition: "an unnumbered subdivision", Rule: "generic:markerBlocks"},
						}},
					{Number: "2", Definition: "the act of fastening such a hook",
						Topic: "Sailing", Patterns: []string{"a wexal of something"}, Rule: "generic:markerBlocks"},
				}},
				{POS: "verb", Grammar: "transitive", Confidence: 0.9, Senses: []entryir.Sense{
					{Number: "1", Definition: "to fasten with a wexal",
						Synonyms: []string{"belay", "cleat"}, Antonyms: []string{"loosen"}},
				}},
			},
			Forms: []entryir.Form{
				{Name: "plural", Words: []string{"wexals", "wexalles"}},
				{Name: "past tense", Words: []string{"wexalled"}, Audio: audio("snd://synthetic/wexalled.spx")},
			},
			Idioms: []entryir.PhraseEntry{
				{Phrase: "on the wexal", Definition: "ready to be used at once",
					Examples: []entryir.Example{{Text: "the rescue boat was on the wexal all night"}}},
			},
			Phrases:         []entryir.PhraseEntry{{Phrase: "wexal line", Definition: "the rope a wexal takes"}},
			PhrasalVerbs:    []entryir.PhraseEntry{{Phrase: "wexal up", Definition: "to prepare for departure"}},
			Derivatives:     []entryir.PhraseEntry{{Phrase: "wexalage", Definition: "the fee paid for mooring"}},
			CrossReferences: []string{"grommet", "cleat"},
			Related:         []string{"halyard"},
			WordFamily:      []string{"wexaller"},
			Collocations:    []string{"secure a wexal"},
			Synonyms:        []string{"hook"},
			Antonyms:        []string{"release"},
			UsageNotes:      []entryir.Section{{Title: "Register", Body: "Chiefly used aboard small craft."}},
			GrammarNotes:    []entryir.Section{{Title: "Countability", Body: "Countable in both senses."}},
			Etymology:       "Invented for this test in 2026.",
		}}},
	}
}

// bilingual is the Priority A shape: an English headword with Chinese glosses
// on the definition and on the example.
func bilingual() *entryir.EntrySet {
	return &entryir.EntrySet{
		LookupKey: "wexal",
		Headword:  "wexal",
		Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: &entryir.Entry{
			Headword: "wexal",
			Pronunciations: []entryir.Pronunciation{
				{IPARegion: entryir.RegionNeutral, IPA: "ˈwɛksl", Confidence: 0.6, Rule: "generic:ipa"},
			},
			Parts: []entryir.Part{{POS: "noun", Rule: "generic:senseHints", Senses: []entryir.Sense{
				{Number: "1", Definition: "a small hook used to fasten a sail", Translation: "系帆小钩",
					Rule: "generic:senseHints", Confidence: 0.75,
					Examples: []entryir.Example{
						{Text: "tie the rope to the wexal", Translation: "把绳子系在小钩上"},
						{Text: "the wexal held fast", Translation: "小钩牢牢固定住了"},
					}},
				{Number: "2", Translation: "系钩的动作", Rule: "generic:senseHints"},
			}}},
		}}},
	}
}

// duplicates is the multi-record shape: one key, two records the dictionary
// chose to keep apart, plus a record that recovered no structure at all.
func duplicates() *entryir.EntrySet {
	return &entryir.EntrySet{
		LookupKey: "wexal",
		Headword:  "wexal",
		Records: []entryir.EntryRecord{
			{RecordOrdinal: 1, Entry: &entryir.Entry{
				Headword: "wexal",
				Parts: []entryir.Part{{POS: "noun", Senses: []entryir.Sense{
					{Number: "1", Definition: "a small hook"},
				}}},
			}},
			{RecordOrdinal: 2, Entry: &entryir.Entry{
				Headword: "wexal",
				Parts: []entryir.Part{{POS: "verb", Senses: []entryir.Sense{
					{Number: "1", Definition: "to fasten with a hook"},
				}}},
			}},
			{RecordOrdinal: 3, Entry: &entryir.Entry{
				Headword: "wexal",
				Sections: []entryir.Section{{Title: "Entry", Body: "A third record whose structure was not recovered."}},
			}},
		},
	}
}

func fixtures() map[string]*entryir.EntrySet {
	return map[string]*entryir.EntrySet{
		"english-monolingual": wexal(),
		"chinese-bilingual":   bilingual(),
		"multi-record":        duplicates(),
	}
}

func TestGoldenMarkdown(t *testing.T) {
	opts := mdrender.DefaultOptions()
	opts.MaxExamplesPerSense = 4
	for name, set := range fixtures() {
		t.Run(name, func(t *testing.T) {
			got := mdrender.RenderEntrySet(set, opts)
			path := filepath.Join("testdata", name+".md")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing expectation (run with -update): %v", err)
			}
			if got != string(want) {
				t.Errorf("Markdown mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// Snapshot comparison is the whole reason this renderer exists, so identical
// input producing identical bytes is a property worth asserting outright.
func TestRenderingIsDeterministic(t *testing.T) {
	for name, set := range fixtures() {
		first := mdrender.RenderEntrySet(set, mdrender.DefaultOptions())
		for i := 0; i < 8; i++ {
			if again := mdrender.RenderEntrySet(set, mdrender.DefaultOptions()); again != first {
				t.Fatalf("%s: rendering is not stable across runs", name)
			}
		}
	}
}

// A resolved recording's URL carries a per-process token. Printing it by
// default would make every run of the validation pipeline report every entry
// as changed.
func TestAudioURLsAreOmittedByDefault(t *testing.T) {
	out := mdrender.RenderEntrySet(wexal(), mdrender.DefaultOptions())
	if strings.Contains(out, "TOKEN") || strings.Contains(out, "127.0.0.1") {
		t.Errorf("default rendering leaked a resource token:\n%s", out)
	}
	if !strings.Contains(out, "🔊 audio UK") {
		t.Errorf("an available recording should still be reported:\n%s", out)
	}

	opts := mdrender.DefaultOptions()
	opts.AudioLinks = true
	linked := mdrender.RenderEntrySet(wexal(), opts)
	if !strings.Contains(linked, "http://127.0.0.1:15321/v2/resource/TOKEN") {
		t.Errorf("AudioLinks should emit the playable URL:\n%s", linked)
	}
}

// Distinct records are distinct things the dictionary said. Losing that
// boundary is the failure this renderer must not have.
func TestMultiRecordSetsKeepTheirBoundaries(t *testing.T) {
	out := mdrender.RenderEntrySet(duplicates(), mdrender.DefaultOptions())
	for _, want := range []string{"Record 1 of 3", "Record 2 of 3", "Record 3 of 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	single := mdrender.RenderEntrySet(&entryir.EntrySet{
		LookupKey: "wexal",
		Records:   duplicates().Records[:1],
	}, mdrender.DefaultOptions())
	if strings.Contains(single, "Record 1") {
		t.Errorf("a single-record set should not be labelled as one of several:\n%s", single)
	}
	if !strings.Contains(single, "\n## noun\n") {
		t.Errorf("a single-record set should raise its parts one heading level:\n%s", single)
	}
}

// The renderer reports the IR and nothing else. A field the parser did not
// produce must not acquire a value on the way to Markdown.
func TestEmptyStructuresAreNotInvented(t *testing.T) {
	out := mdrender.RenderEntrySet(&entryir.EntrySet{
		LookupKey: "wexal",
		Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: &entryir.Entry{
			Headword: "wexal",
			Parts: []entryir.Part{{Senses: []entryir.Sense{
				{Definition: "a small hook"},
			}}},
		}}},
	}, mdrender.DefaultOptions())
	for _, absent := range []string{"Idioms", "Forms", "See also", "Origin", "*headword:*"} {
		if strings.Contains(out, absent) {
			t.Errorf("rendered %q for an entry that has none:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "## (unlabelled)") {
		t.Errorf("a part with no POS should say so rather than guess:\n%s", out)
	}
}

func TestNilInputsRenderNothing(t *testing.T) {
	if out := mdrender.RenderEntrySet(nil, mdrender.DefaultOptions()); out != "" {
		t.Errorf("nil set rendered %q", out)
	}
	if out := mdrender.RenderEntry(nil, mdrender.DefaultOptions()); out != "" {
		t.Errorf("nil entry rendered %q", out)
	}
	empty := &entryir.EntrySet{LookupKey: "wexal"}
	if out := mdrender.RenderEntrySet(empty, mdrender.DefaultOptions()); out != "" {
		t.Errorf("record-less set rendered %q", out)
	}
}

// Dictionary text contains asterisks, underscores and angle brackets. They
// must not turn into emphasis or be eaten as tags.
func TestMarkdownSyntaxInContentIsEscaped(t *testing.T) {
	out := mdrender.RenderEntrySet(&entryir.EntrySet{
		LookupKey: "wexal",
		Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: &entryir.Entry{
			Headword: "wexal",
			Parts: []entryir.Part{{POS: "noun", Senses: []entryir.Sense{
				{Number: "1", Definition: "used with *emphasis* and <angle> and a_b"},
			}}},
		}}},
	}, mdrender.DefaultOptions())
	if !strings.Contains(out, `\*emphasis\*`) || !strings.Contains(out, `\<angle\>`) || !strings.Contains(out, `a\_b`) {
		t.Errorf("Markdown syntax survived unescaped:\n%s", out)
	}
}

func TestExampleLimitIsHonoured(t *testing.T) {
	sense := entryir.Sense{Number: "1", Definition: "a small hook"}
	for i := 0; i < 10; i++ {
		sense.Examples = append(sense.Examples, entryir.Example{Text: "example sentence " + string(rune('a'+i))})
	}
	opts := mdrender.DefaultOptions()
	opts.MaxExamplesPerSense = 3
	out := mdrender.RenderEntrySet(&entryir.EntrySet{
		LookupKey: "wexal",
		Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: &entryir.Entry{
			Parts: []entryir.Part{{POS: "noun", Senses: []entryir.Sense{sense}}},
		}}},
	}, opts)
	if got := strings.Count(out, "example sentence"); got != 3 {
		t.Errorf("rendered %d examples, want 3:\n%s", got, out)
	}
}
