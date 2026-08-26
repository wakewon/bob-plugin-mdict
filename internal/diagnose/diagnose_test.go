package diagnose_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

// Every fixture in this file is written from scratch with invented headwords,
// invented definitions and invented resource paths. Nothing here is derived
// from a real dictionary.

func syntheticDictionary(t *testing.T, entries []testmdx.Entry) *mdict.Dictionary {
	t.Helper()
	root := t.TempDir()
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), entries); err != nil {
		t.Fatal(err)
	}
	registry := mdict.NewRegistry(root)
	if err := registry.Scan(); err != nil {
		t.Fatal(err)
	}
	all := registry.All()
	if len(all) != 1 {
		t.Fatalf("expected one dictionary, found %d", len(all))
	}
	if err := all[0].Load(); err != nil {
		t.Fatal(err)
	}
	return all[0]
}

// structuredEntry is a record with class vocabulary, repeated senses, a
// cross-reference and a pronunciation reference: everything the sampler is
// supposed to notice.
func structuredEntry(headword string) string {
	return fmt.Sprintf(`<div class="entry"><h1 class="hw">%s</h1>`+
		`<a class="speaker" href="sound://invented/%s.mp3">audio</a>`+
		`<span class="pron">/ˈ%sə/</span>`+
		`<div class="sense"><span class="pos">noun</span>`+
		`<span class="def">An invented thing of the first kind.</span>`+
		`<span class="example">A sentence using %s.</span></div>`+
		`<div class="sense"><span class="def">An invented thing of the second kind.</span></div>`+
		`<div class="xref">see also <a href="entry://other">other</a></div></div>`,
		headword, headword, headword, headword)
}

// plainEntry is a one-line record: valid, but with nothing to learn from.
func plainEntry(headword string) string {
	return "<div>" + headword + " — a short unstructured note that carries no markup at all.</div>"
}

func TestSamplingIsDeterministicAndPrefersStructure(t *testing.T) {
	var entries []testmdx.Entry
	for i := 0; i < 40; i++ {
		entries = append(entries, testmdx.Entry{
			Key:  fmt.Sprintf("plain%02d", i),
			HTML: plainEntry(fmt.Sprintf("plain%02d", i)),
		})
	}
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("rich%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: structuredEntry(key)})
	}
	dict := syntheticDictionary(t, entries)

	first := diagnose.Samples(dict, diagnose.SampleOptions{Pool: 40, Keep: 6})
	second := diagnose.Samples(dict, diagnose.SampleOptions{Pool: 40, Keep: 6})
	if len(first) == 0 {
		t.Fatal("no samples were selected")
	}

	keysOf := func(samples []diagnose.Sample) []string {
		out := make([]string, 0, len(samples))
		for _, sample := range samples {
			out = append(out, sample.Key)
		}
		return out
	}
	if !reflect.DeepEqual(keysOf(first), keysOf(second)) {
		t.Errorf("sampling is not reproducible: %v then %v", keysOf(first), keysOf(second))
	}
	for _, key := range keysOf(first) {
		if !strings.HasPrefix(key, "rich") {
			t.Errorf("sampler kept %q; structurally informative records were available", key)
		}
	}
}

// Sampling must work on a dictionary with no Latin characters anywhere: the
// point of striding the key index is that no probe word list is involved.
func TestSamplingIsLanguageIndependent(t *testing.T) {
	headwords := []string{"格尔特", "ゲルト", "결과", "снег", "χιόνι", "ثلج", "หิมะ", "לשלג"}
	var entries []testmdx.Entry
	for i, headword := range headwords {
		key := fmt.Sprintf("%s%d", headword, i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: structuredEntry(key)})
	}
	dict := syntheticDictionary(t, entries)

	samples := diagnose.Samples(dict, diagnose.SampleOptions{Pool: 8, Keep: 4})
	if len(samples) != 4 {
		t.Fatalf("selected %d samples from a non-Latin dictionary, want 4", len(samples))
	}
	report := diagnose.Inspect(dict, diagnose.Options{Sampling: diagnose.SampleOptions{Pool: 8, Keep: 4}})
	if report.Coverage.Definitions == 0 {
		t.Error("no definitions were extracted from a non-Latin dictionary")
	}
}

func TestScoreRecordPrefersRicherMarkup(t *testing.T) {
	rich := diagnose.ScoreRecord([]byte(structuredEntry("glimmet")))
	plain := diagnose.ScoreRecord([]byte(plainEntry("glimmet")))
	if rich <= plain {
		t.Errorf("structured record scored %d, plain record %d", rich, plain)
	}
}

func TestInspectReportsContainerAndCoverage(t *testing.T) {
	var entries []testmdx.Entry
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("glimmet%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: structuredEntry(key)})
	}
	dict := syntheticDictionary(t, entries)
	report := diagnose.Inspect(dict, diagnose.Options{Sampling: diagnose.SampleOptions{Pool: 8, Keep: 5}})

	if report.Container.Health != "ok" || report.Container.EntryCount != 8 {
		t.Errorf("container reported as %+v", report.Container)
	}
	if report.Coverage.Samples == 0 || report.Coverage.StructuredRate != 1 {
		t.Errorf("coverage = %+v, want every sample structured", report.Coverage)
	}
	if report.Coverage.FallbackRate != 0 {
		t.Errorf("fallback rate = %v, want 0", report.Coverage.FallbackRate)
	}
	if report.Coverage.CrossRefs == 0 {
		t.Error("the cross-reference block in every sample was not reported")
	}
	// An MDX with no MDD beside it references audio without resolving it, and
	// the two facts must stay apart.
	if report.Pronunciation.AudioRefSamples == 0 {
		t.Error("audio references in the markup were not detected")
	}
	if report.Pronunciation.MDDVolumes != 0 || report.Pronunciation.AudioResolved != 0 {
		t.Error("audio was reported as resolved for a dictionary with no MDD")
	}
	if report.Pronunciation.IPASamples == 0 {
		t.Error("IPA in the markup was not detected")
	}
	if len(report.DOM.Classes) == 0 || report.DOM.FamilyKey == "" {
		t.Errorf("DOM summary is empty: %+v", report.DOM)
	}
}

// A dictionary the parser cannot structure at all should say so plainly rather
// than reporting invented structure.
func TestInspectFlagsFallbackHeavyDictionary(t *testing.T) {
	var entries []testmdx.Entry
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("plain%02d", i)
		entries = append(entries, testmdx.Entry{
			Key: key,
			HTML: "<div>" + key + " " + strings.Repeat(
				"An unstructured paragraph with no markup of any kind whatsoever. ", 40) + "</div>",
		})
	}
	dict := syntheticDictionary(t, entries)
	report := diagnose.Inspect(dict, diagnose.Options{Sampling: diagnose.SampleOptions{Pool: 8, Keep: 5}})

	if report.Coverage.FallbackRate != 1 {
		t.Errorf("fallback rate = %v, want 1", report.Coverage.FallbackRate)
	}
	if !hasWarning(report, "high-fallback-rate") {
		t.Errorf("expected a high-fallback-rate signal, got %+v", report.Warnings)
	}
	if report.Profile.Selected != diagnose.GenericProfileID {
		t.Errorf("profile = %q, want generic", report.Profile.Selected)
	}
}

// A forced parser must be reported as forced. A report that presented an
// override as though detection had produced it would be worse than useless
// during a comparison run.
func TestReportRecordsAParserOverride(t *testing.T) {
	var entries []testmdx.Entry
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("glimmet%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: structuredEntry(key)})
	}
	dict := syntheticDictionary(t, entries)

	auto := diagnose.Inspect(dict, diagnose.Options{Sampling: diagnose.SampleOptions{Pool: 6, Keep: 4}})
	if auto.Profile.Selected != diagnose.GenericProfileID || auto.Profile.Override != "" {
		t.Fatalf("automatic run reported %+v", auto.Profile)
	}

	forced := diagnose.Inspect(dict, diagnose.Options{
		Sampling:        diagnose.SampleOptions{Pool: 6, Keep: 4},
		ProfileOverride: "oald8",
	})
	if forced.Profile.Selected != "oald8" || forced.Profile.Override != "oald8" {
		t.Errorf("forced run reported %+v, want oald8 marked as an override", forced.Profile)
	}
}

func hasWarning(report diagnose.Report, code string) bool {
	for _, warning := range report.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func TestUnavailableDictionaryIsReportedNotCrashed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.mdx"), []byte("not an mdx file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := mdict.NewRegistry(root)
	if err := registry.Scan(); err != nil {
		t.Fatal(err)
	}
	all := registry.All()
	if len(all) != 1 {
		t.Fatalf("expected one dictionary, found %d", len(all))
	}
	report := diagnose.Inspect(all[0], diagnose.Options{})
	if report.Container.Health == "ok" {
		t.Error("a corrupt file was reported as healthy")
	}
	if report.Profile.Selected != diagnose.GenericProfileID {
		t.Errorf("profile = %q, want generic", report.Profile.Selected)
	}
}

func TestCorpusSummarizesEveryDictionary(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one", "two"} {
		var entries []testmdx.Entry
		for i := 0; i < 6; i++ {
			key := fmt.Sprintf("%s%02d", name, i)
			entries = append(entries, testmdx.Entry{Key: key, HTML: structuredEntry(key)})
		}
		if err := testmdx.Write(filepath.Join(root, name+".mdx"), entries); err != nil {
			t.Fatal(err)
		}
	}
	registry := mdict.NewRegistry(root)
	if err := registry.Scan(); err != nil {
		t.Fatal(err)
	}
	registry.LoadAll()

	report := diagnose.Corpus(registry, diagnose.Options{
		Sampling: diagnose.SampleOptions{Pool: 6, Keep: 4},
	}, nil)
	if report.Total != 2 || report.Healthy != 2 {
		t.Errorf("corpus totals = %d/%d, want 2/2", report.Healthy, report.Total)
	}
	if report.Aggregate.WithDefinitions != 2 {
		t.Errorf("definitions found in %d dictionaries, want 2", report.Aggregate.WithDefinitions)
	}
	// Two dictionaries built from one template are one family.
	if len(report.Families) != 1 || len(report.Families[0].Members) != 2 {
		t.Errorf("families = %+v, want one family of two", report.Families)
	}
	markdown := report.Markdown()
	for _, want := range []string{"MDX corpus diagnostics", "Parser selection", "Per dictionary"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown report is missing %q", want)
		}
	}
	// The report is meant to be safe to keep: it must carry structure, never
	// the dictionary's own words.
	if strings.Contains(markdown, "An invented thing of the first kind") {
		t.Error("the corpus report reproduced dictionary text")
	}
}
