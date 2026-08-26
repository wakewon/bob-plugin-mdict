package validate_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
	"github.com/wakewon/bob-plugin-mdict/internal/validate"
)

// Every dictionary in this file is written from scratch: invented headwords,
// invented meanings, invented Chinese glosses, invented resource paths. The
// pipeline is being tested against dictionary *shapes*, which is all it needs.

func newService(t *testing.T, dictionaries map[string][]testmdx.Entry) *service.Service {
	t.Helper()
	root := t.TempDir()
	for name, entries := range dictionaries {
		path := filepath.Join(root, name, name+".mdx")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := testmdx.Write(path, entries); err != nil {
			t.Fatal(err)
		}
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

// bilingualEntries is the Priority A shape: English headwords, English
// definitions, Chinese glosses, translated examples.
func bilingualEntries(count int) []testmdx.Entry {
	var entries []testmdx.Entry
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("wexal%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: fmt.Sprintf(
			`<div class="entry"><span class="hw">%s</span>`+
				`<div class="sense"><span class="pos">noun</span>`+
				`<span class="def">a small hook used to fasten a sail</span>`+
				`<span class="chn">系帆小钩，用于固定帆布</span>`+
				`<span class="example">She reached for the %s.</span></div>`+
				`<div class="sense"><span class="def">the act of fastening such a hook</span>`+
				`<span class="chn">系钩的动作，通常在甲板上进行</span></div></div>`, key, key)})
	}
	return entries
}

// monolingualEntries is the Priority B shape.
func monolingualEntries(count int) []testmdx.Entry {
	var entries []testmdx.Entry
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("plimber%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: fmt.Sprintf(
			`<div class="entry"><span class="hw">%s</span>`+
				`<span class="pron">/ˈplɪmbə/</span>`+
				`<div class="sense"><span class="pos">noun</span>`+
				`<span class="def">a tool used for smoothing wet clay</span>`+
				`<span class="example">He set the %s down on the bench.</span></div>`+
				`<div class="sense"><span class="def">the act of smoothing something by hand</span></div></div>`, key, key)})
	}
	return entries
}

// articleEntries is the Priority E shape: long multi-word headwords, long
// prose, no part of speech, no transcription, no numbering.
func articleEntries(count int) []testmdx.Entry {
	body := strings.Repeat("Invented prose about an invented subject, at some length. ", 140)
	var entries []testmdx.Entry
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("The Invented Theory of Invented Things %02d", i)
		entries = append(entries, testmdx.Entry{Key: key,
			HTML: `<div><h1>` + key + `</h1><p>` + body + `</p></div>`})
	}
	return entries
}

func dictionaryNamed(t *testing.T, run *validate.Run, fragment string) validate.DictionaryResult {
	t.Helper()
	for _, dictionary := range run.Dictionaries {
		if strings.Contains(dictionary.Report.Container.Title, fragment) ||
			strings.Contains(dictionary.Report.Container.ID, fragment) {
			return dictionary
		}
	}
	// Titles come from the MDX header, which testmdx writes generically, so
	// fall back to the only dictionary present.
	if len(run.Dictionaries) == 1 {
		return run.Dictionaries[0]
	}
	t.Fatalf("no dictionary matching %q in the run", fragment)
	return validate.DictionaryResult{}
}

func TestChineseDictionaryIsPriorityA(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{"cn": bilingualEntries(24)})
	run := validate.Corpus(svc, validate.Options{})
	dictionary := dictionaryNamed(t, run, "cn")
	if dictionary.Language.Tier != validate.TierChinese {
		t.Errorf("tier = %s, want Chinese; evidence %v", dictionary.Language.Tier.Label(), dictionary.Language.Evidence)
	}
	if !dictionary.Language.Chinese {
		t.Error("a dictionary glossing every sense in Chinese was not recognised as one")
	}
}

func TestEnglishMonolingualIsPriorityB(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{"en": monolingualEntries(24)})
	run := validate.Corpus(svc, validate.Options{})
	dictionary := dictionaryNamed(t, run, "en")
	if dictionary.Language.Tier != validate.TierEnglishMono {
		t.Errorf("tier = %s, want English monolingual", dictionary.Language.Tier.Label())
	}
}

// An article collection is not a lexicon, and forcing senses onto it would
// both fail and distort every average the corpus reports.
func TestArticleCollectionIsDeprioritised(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{"articles": articleEntries(24)})
	run := validate.Corpus(svc, validate.Options{})
	dictionary := dictionaryNamed(t, run, "articles")
	if dictionary.Language.Tier != validate.TierReference {
		t.Errorf("tier = %s, want reference; evidence %v", dictionary.Language.Tier.Label(), dictionary.Language.Evidence)
	}
	if dictionary.Language.Lexical {
		t.Error("an article collection was classified as a lexicon")
	}
	// Three records is enough to prove the container opens and the parser does
	// not panic, which is all this tier is sampled for.
	if len(dictionary.Snapshots) > 4 {
		t.Errorf("a reference work was sampled %d times, which is a waste of the budget", len(dictionary.Snapshots))
	}
	for _, snapshot := range dictionary.Snapshots {
		if snapshot.Fields.Senses > 0 {
			t.Errorf("article prose was forced into %d senses", snapshot.Fields.Senses)
		}
		if snapshot.Fields.Fallback == 0 {
			t.Error("article prose should survive as untyped content")
		}
	}
}

// The queue exists to spend a reviewer's attention where the product needs it.
func TestReviewQueueFavoursChineseDictionaries(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{
		"cn":        bilingualEntries(40),
		"en":        monolingualEntries(40),
		"articles1": articleEntries(40),
		"articles2": articleEntries(40),
	})
	run := validate.Corpus(svc, validate.Options{QueueSize: 20})
	if len(run.Queue) == 0 {
		t.Fatal("the queue is empty")
	}

	tiers := map[string]int{}
	for _, snapshot := range run.Queue {
		tiers[snapshot.Tier]++
	}
	if tiers["A"]*2 < len(run.Queue) {
		t.Errorf("Chinese records are %d of %d queue slots, want at least half: %v",
			tiers["A"], len(run.Queue), tiers)
	}
	if tiers["E"] > len(run.Queue)/4 {
		t.Errorf("reference works took %d of %d queue slots", tiers["E"], len(run.Queue))
	}
}

// Retention is the measure closest to "did anything go missing".
func TestRetentionNoticesDroppedContent(t *testing.T) {
	// The definitions are inside a class the parser recognises; the long
	// commentary beside them is not, and has nowhere to go.
	var entries []testmdx.Entry
	commentary := strings.Repeat("Commentary that belongs to no sense and no section whatsoever. ", 20)
	for i := 0; i < 16; i++ {
		key := fmt.Sprintf("wexal%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: fmt.Sprintf(
			`<div class="entry"><span class="hw">%s</span>`+
				`<div class="sense"><span class="def">a small hook used to fasten a sail</span></div>`+
				`<div class="sense"><span class="def">the act of fastening such a hook</span></div>`+
				`<blockquote>%s</blockquote></div>`, key, commentary)})
	}
	svc := newService(t, map[string][]testmdx.Entry{"lossy": entries})
	run := validate.Corpus(svc, validate.Options{})
	dictionary := dictionaryNamed(t, run, "lossy")
	if dictionary.MeanRetention > 0.5 {
		t.Errorf("mean retention %.2f, expected the dropped commentary to show", dictionary.MeanRetention)
	}
	flagged := false
	for _, snapshot := range dictionary.Snapshots {
		for _, signal := range snapshot.Signals {
			if signal == validate.SignalLowRetention {
				flagged = true
			}
		}
	}
	if !flagged {
		t.Error("no record was flagged for low retention")
	}
}

// Every invariant between parser, service, Bob adapter and Markdown renderer
// has to hold on an ordinary dictionary, or the ones that fail on a real one
// mean nothing.
func TestBackendParityHoldsOnOrdinaryDictionaries(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{
		"cn": bilingualEntries(24),
		"en": monolingualEntries(24),
	})
	run := validate.Corpus(svc, validate.Options{})
	if run.Aggregate.Records == 0 {
		t.Fatal("nothing was validated")
	}
	for _, dictionary := range run.Dictionaries {
		for _, snapshot := range dictionary.Snapshots {
			if len(snapshot.Failures) > 0 {
				t.Errorf("%s / %s: %v", snapshot.DictionaryTitle, snapshot.Key, snapshot.Failures)
			}
		}
	}
	if len(run.Failures) != 0 {
		t.Errorf("corpus-level parity failures: %+v", run.Failures)
	}
}

func TestValidationUsesRuntimeProfileAsAuthority(t *testing.T) {
	var entries []testmdx.Entry
	for i := 0; i < 24; i++ {
		key := fmt.Sprintf("wexal%02d", i)
		entries = append(entries, testmdx.Entry{Key: key, HTML: `<article class="ldoceEntry"><span class="Head"><span class="HWD">` + key +
			`</span></span><div class="Sense"><span class="DEF">a small invented hook</span></div></article>`})
	}
	svc := newService(t, map[string][]testmdx.Entry{"profiled": entries})
	svc.SetParserOverride("generic")
	run := validate.Corpus(svc, validate.Options{})
	dictionary := dictionaryNamed(t, run, "profiled")
	if dictionary.Report.Profile.Selected != "ldoce5pp" {
		t.Fatalf("fixture did not produce independent diagnostic evidence: %+v", dictionary.Report.Profile)
	}
	if dictionary.RuntimeProfile != "generic" {
		t.Fatalf("runtime profile = %q, want forced generic", dictionary.RuntimeProfile)
	}
	for _, snapshot := range dictionary.Snapshots {
		if snapshot.Parser != "generic" {
			t.Fatalf("snapshot metrics claimed parser %q", snapshot.Parser)
		}
	}
}

// A run is only half useful without the previous one.
func TestBaselineComparisonSeparatesImprovementFromRegression(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{"en": monolingualEntries(24)})
	first := validate.Corpus(svc, validate.Options{})

	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	if err := validate.SaveBaseline(path, first); err != nil {
		t.Fatal(err)
	}
	baseline, err := validate.LoadBaseline(path)
	if err != nil || baseline == nil {
		t.Fatalf("reload baseline: %v", err)
	}

	second := validate.Corpus(svc, validate.Options{Baseline: baseline})
	if second.Comparison == nil {
		t.Fatal("no comparison was produced")
	}
	for _, item := range second.Comparison.Counts {
		if item.Name != string(validate.ChangeUnchanged) {
			t.Errorf("an unchanged parser reported %d records as %q", item.Count, item.Name)
		}
	}

	// A missing baseline is not an error: the first run has nothing to compare
	// against and should say so rather than fail.
	absent, err := validate.LoadBaseline(filepath.Join(dir, "not-there.json"))
	if err != nil || absent != nil {
		t.Errorf("a missing baseline should read as nil, got %v / %v", absent, err)
	}
}

// The review artifacts are the deliverable, so their entry point has to exist
// and to link to what it lists.
func TestWriteProducesALinkedReviewSet(t *testing.T) {
	svc := newService(t, map[string][]testmdx.Entry{"cn": bilingualEntries(24)})
	run := validate.Corpus(svc, validate.Options{QueueSize: 5})
	dir := t.TempDir()
	if err := validate.Write(dir, run); err != nil {
		t.Fatal(err)
	}

	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# MDX validation run", "Review queue", "Quality by tier"} {
		if !strings.Contains(string(index), want) {
			t.Errorf("index is missing %q", want)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(run.Queue) {
		t.Errorf("wrote %d snapshots for a queue of %d", len(entries), len(run.Queue))
	}
	for _, entry := range entries {
		if !strings.Contains(string(index), "snapshots/"+entry.Name()) {
			t.Errorf("snapshot %s is not linked from the index", entry.Name())
		}
		body, err := os.ReadFile(filepath.Join(dir, "snapshots", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, section := range []string{"## Metadata", "## Automated consistency", "## Backend parity",
			"## Source record", "## Experimental Markdown rendering", "## Parsed EntrySet",
			"## Simulated Bob result"} {
			if !strings.Contains(string(body), section) {
				t.Errorf("%s is missing %q", entry.Name(), section)
			}
		}
	}

	var reloaded validate.Run
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("run.json does not round-trip: %v", err)
	}
	if reloaded.Aggregate.Records != run.Aggregate.Records {
		t.Errorf("run.json lost records: %d vs %d", reloaded.Aggregate.Records, run.Aggregate.Records)
	}
}
