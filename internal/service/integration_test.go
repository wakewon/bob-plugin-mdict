package service_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/httpapi"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// dictionaryDir returns the real dictionary library to test against.
//
// These are the developer's own licensed dictionaries. They are never committed
// to the repository, so CI and any other checkout simply skip these tests
// instead of failing.
// dictionaryDir locates the real dictionaries the integration tests read.
//
// Short mode skips them. These tests are corpus-scale by nature — the
// development library is a hundred dictionaries and every test parses a fresh
// service over all of them — which is fine at full speed and hopeless under
// the race detector, where the package cannot finish inside an hour. The race
// suite runs `-short`, where it exercises the synthetic concurrency fixtures in
// concurrency_test.go instead: contention is what race detection needs, and
// volume is what it cannot afford.
func dictionaryDir(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping corpus-scale integration test in short mode")
	}
	dir := os.Getenv("BOB_MDICT_TEST_DICTIONARIES")
	if dir == "" {
		t.Skip("real dictionary corpus is opt-in; set BOB_MDICT_TEST_DICTIONARIES to run integration tests")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("BOB_MDICT_TEST_DICTIONARIES is set to %q but it does not exist", dir)
	}
	if !containsMDX(dir) {
		t.Skipf("%q contains no .mdx files", dir)
	}
	return dir
}

// containsMDX reports whether a directory tree holds at least one .mdx file.
func containsMDX(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".mdx") {
			found = true
		}
		return nil
	})
	return found
}

func newService(t *testing.T) *service.Service {
	t.Helper()
	svc, err := service.New(config.Config{
		DictionaryDir: dictionaryDir(t),
		CacheDir:      t.TempDir(),
		Port:          15321,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		t.Fatal(err)
	}
	return svc
}

func primaryEntry(match *service.Match) *entryir.Entry {
	if match == nil || len(match.Records) == 0 {
		return nil
	}
	return match.Records[0].Entry
}

func matchEntrySet(match *service.Match) *entryir.EntrySet {
	if match == nil {
		return nil
	}
	return &entryir.EntrySet{LookupKey: match.LookupKey, Headword: match.Headword, Records: match.Records}
}

// TestRealDictionariesAreDiscoveredAndHealthy checks discovery and, more
// importantly, containment.
//
// A real library contains files this project cannot open — an MDX whose key
// block info is not zlib-compressed is a newer container revision or an
// encryption scheme that is not implemented. That is a known boundary, not a
// test failure. What must hold is that such a file is *isolated*: reported
// unavailable, carrying a diagnostic that says why, and leaving every other
// dictionary working. A file that failed silently, or took the registry down
// with it, is the defect this guards against.
func TestRealDictionariesAreDiscoveredAndHealthy(t *testing.T) {
	svc := newService(t)
	total, healthy := svc.Registry().Counts()
	if total == 0 {
		t.Fatal("no dictionaries discovered")
	}
	if healthy == 0 {
		t.Fatalf("none of %d discovered dictionaries is usable", total)
	}

	unavailable := 0
	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		t.Logf("%-46s entries=%-8d mdd=%d profile=%s health=%s",
			truncate(info.Title, 46), info.EntryCount, info.MDDVolumes, svc.ProfileID(dict), info.Health)

		if info.Health != "ok" {
			unavailable++
			// Silent breakage is the failure mode. An unusable dictionary must
			// say what went wrong, so the user can act on it.
			if len(info.Diagnostics) == 0 {
				t.Errorf("%s is unavailable but carries no diagnostic", info.Title)
			}
			continue
		}
		// A healthy dictionary with no keys is broken while claiming not to be.
		if info.EntryCount == 0 {
			t.Errorf("%s reports health ok but zero entries", info.Title)
		}
	}
	t.Logf("%d dictionaries discovered, %d healthy, %d isolated as unavailable",
		total, healthy, unavailable)
}

func TestDictionarySelectionContract(t *testing.T) {
	svc := newService(t)
	word := "abandon"

	first, err := svc.Lookup(word, service.LookupOptions{Limit: 1, Mode: service.ModeExact})
	if err != nil || len(first.Matches) != 1 {
		t.Fatalf("blank ID first-match lookup failed: matches=%d err=%v", len(first.Matches), err)
	}
	wantID := first.Matches[0].DictionaryID
	explicit, err := svc.Lookup(word, service.LookupOptions{DictionaryIDs: []string{wantID}, Limit: 1, Mode: service.ModeExact})
	if err != nil || len(explicit.Matches) != 1 || explicit.Matches[0].DictionaryID != wantID {
		t.Fatalf("explicit ID lookup escaped selection: %+v err=%v", explicit, err)
	}
	if _, err := svc.Lookup(word, service.LookupOptions{DictionaryIDs: []string{"does-not-exist"}, Limit: 1}); !errors.Is(err, service.ErrDictionaryNotFound) {
		t.Fatalf("invalid ID error = %v, want ErrDictionaryNotFound", err)
	}
	missing, err := svc.Lookup("flimber-does-not-exist", service.LookupOptions{DictionaryIDs: []string{wantID}, Limit: 1, Mode: service.ModeExact})
	if err != nil || len(missing.Matches) != 0 {
		t.Fatalf("normal headword miss should not be a selection error: %+v err=%v", missing, err)
	}
}

func TestBobRenderingNeverAggregatesMultipleDictionaries(t *testing.T) {
	svc := newService(t)
	opts := bobadapter.DefaultOptions()
	result, err := svc.Lookup("abandon", service.LookupOptions{
		Mode: service.ModeExact, RenderBob: true, BobOptions: opts,
	})
	if err != nil || len(result.Matches) < 2 {
		t.Skipf("need two matching dictionaries for aggregation guard: matches=%d err=%v", len(result.Matches), err)
	}
	want := bobadapter.RenderEntrySet(matchEntrySet(&result.Matches[0]), opts)
	if !reflect.DeepEqual(result.Bob, want) {
		t.Fatal("Bob output was not rendered exclusively from the first dictionary match")
	}
}

func TestRealEntrySourceNumbersAndBobPresentationNumbersStaySeparate(t *testing.T) {
	svc := newService(t)
	result, err := svc.Lookup("abandon", service.LookupOptions{Mode: service.ModeExact})
	if err != nil {
		t.Fatal(err)
	}

	for _, match := range result.Matches {
		entry := primaryEntry(&match)
		if len(entry.Parts) < 2 {
			continue
		}
		hasGlobalSourceNumber := false
		for partIndex, part := range entry.Parts {
			if partIndex > 0 && len(part.Senses) > 0 && part.Senses[0].Number != "" && part.Senses[0].Number != "1" {
				hasGlobalSourceNumber = true
			}
		}
		if !hasGlobalSourceNumber {
			continue
		}

		rendered := bobadapter.Render(entry, bobadapter.DefaultOptions())
		type expectedPart struct {
			label  string
			number string
		}
		var expected []expectedPart
		var appendSense func(label string, sense entryir.Sense, path []int)
		appendSense = func(label string, sense entryir.Sense, path []int) {
			numbers := make([]string, len(path))
			for i, value := range path {
				numbers[i] = strconv.Itoa(value)
			}
			expected = append(expected, expectedPart{label: label, number: strings.Join(numbers, ".")})
			for i, sub := range sense.Subsenses {
				appendSense(label, sub, append(append([]int(nil), path...), i+1))
			}
		}
		for _, sourcePart := range entry.Parts {
			label := bobadapter.CompactPOS(sourcePart.POS)
			for senseIndex, sense := range sourcePart.Senses {
				appendSense(label, sense, []int{senseIndex + 1})
			}
		}
		if len(rendered.Parts) < len(expected) {
			t.Fatalf("%s rendered %d Bob parts, want at least %d recursive senses",
				shortName(match.DictionaryTitle), len(rendered.Parts), len(expected))
		}
		for partIndex, want := range expected {
			part := rendered.Parts[partIndex]
			if part.Part != want.label {
				t.Fatalf("%s Bob part %d label = %q, want repeated label %q",
					shortName(match.DictionaryTitle), partIndex, part.Part, want.label)
			}
			wantPrefix := want.number + "."
			if len(part.Means) == 0 || !strings.HasPrefix(part.Means[0], wantPrefix) {
				t.Fatalf("%s Bob part %d first meaning = %q, want prefix %q",
					shortName(match.DictionaryTitle), partIndex, strings.Join(part.Means, " | "), wantPrefix)
			}
		}
		for _, part := range rendered.Parts {
			if strings.EqualFold(strings.TrimSpace(part.Part), "see also") {
				t.Fatalf("%s still rendered See also as a Bob part", shortName(match.DictionaryTitle))
			}
		}
		if len(entry.CrossReferences) > 0 {
			foundStructured := false
			for _, relatedPart := range rendered.RelatedWordParts {
				if relatedPart.Part == "See also" && len(relatedPart.Words) > 0 {
					foundStructured = true
				}
			}
			if !foundStructured {
				t.Fatalf("%s has cross-references but no structured See also group", shortName(match.DictionaryTitle))
			}
			for _, addition := range rendered.Additions {
				if addition.Name == "See also" {
					t.Fatalf("%s duplicated structured See also as an addition", shortName(match.DictionaryTitle))
				}
			}
		}
		t.Logf("%s preserves source-global numbering while Bob numbering resets per POS",
			shortName(match.DictionaryTitle))
		return
	}

	t.Skip("no real matching entry exposed source-global numbering across multiple POS groups")
}

func TestRealAbandonBobPresentationV110(t *testing.T) {
	svc := newService(t)
	result, err := svc.Lookup("abandon", service.LookupOptions{Mode: service.ModeExact})
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range result.Matches {
		if !strings.Contains(strings.ToLower(match.DictionaryTitle), "collins") {
			continue
		}
		dict := bobadapter.RenderEntrySet(matchEntrySet(&match), bobadapter.DefaultOptions())
		wantParts := []struct {
			label  string
			prefix string
		}{{"v.", "1."}, {"v.", "2."}, {"v.", "3."}, {"v.", "4."}, {"n.", "1."}}
		if len(dict.Parts) < len(wantParts) {
			t.Fatalf("abandon parts = %+v", dict.Parts)
		}
		for i, want := range wantParts {
			if dict.Parts[i].Part != want.label || len(dict.Parts[i].Means) == 0 || !strings.HasPrefix(dict.Parts[i].Means[0], want.prefix) {
				t.Errorf("abandon part %d = %+v, want %s %s", i, dict.Parts[i], want.label, want.prefix)
			}
		}

		additionNames := make(map[string]bool)
		for _, addition := range dict.Additions {
			additionNames[addition.Name] = true
			if addition.Name == "See also" || strings.Contains(addition.Value, "释义 ") {
				t.Errorf("obsolete Bob presentation survived: %+v", addition)
			}
		}
		for _, want := range []string{"Examples · v. 1", "Examples · v. 2", "Examples · n. 1"} {
			if !additionNames[want] {
				t.Errorf("abandon missing addition %q; have %+v", want, additionNames)
			}
		}
		foundPhrasePart := false
		for _, part := range dict.Parts {
			if part.Part == "phr." && len(part.Means) > 0 {
				foundPhrasePart = true
			}
		}
		if !foundPhrasePart {
			t.Errorf("abandon phrases were not surfaced as independent Bob parts: %+v", dict.Parts)
		}
		foundSeeAlso := false
		for _, relatedPart := range dict.RelatedWordParts {
			if relatedPart.Part == "See also" && len(relatedPart.Words) > 0 {
				foundSeeAlso = true
			}
		}
		if !foundSeeAlso {
			t.Fatalf("abandon relatedWordParts = %+v", dict.RelatedWordParts)
		}
		t.Logf("abandon: %d definition parts, %d example/extra additions, %d related-word groups",
			len(dict.Parts), len(dict.Additions), len(dict.RelatedWordParts))
		return
	}
	t.Skip("Collins abandon entry is not installed")
}

// TestRealLookupsProduceStructure is the anti-"pseudo-completion" check.
//
// It deliberately does not require every dictionary to yield senses. A quarter
// of a real library is terminology banks, name lists, etymology dictionaries
// and article-style references whose records are one prose body under a
// headword: there is nothing to divide, and reporting the record honestly as
// untyped content is the correct answer, not a failure. Nor are the English
// probe words evidence about a Japanese-only dictionary that happens to hold
// one of them.
//
// What is a defect, in any dictionary: producing sense structure that carries
// no meaning — parts and senses with neither definition nor translation, which
// is exactly what "pseudo-completion" looks like. That is asserted per
// dictionary. And a parser regression would not show up one dictionary at a
// time, so the share of the library that yields structure at all is asserted
// across the set.
func TestRealLookupsProduceStructure(t *testing.T) {
	svc := newService(t)
	words := []string{"abandon", "run", "good", "book", "take", "quickly", "water", "light"}

	answering, structured, definitions := 0, 0, 0
	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		t.Run(shortName(info.Title), func(t *testing.T) {
			hits, withSenses, withDefinitions := 0, 0, 0
			for _, word := range words {
				result, err := svc.Lookup(word, service.LookupOptions{
					DictionaryIDs: []string{info.ID},
					Mode:          service.ModeExact,
					MaxExamples:   4,
				})
				if err != nil || len(result.Matches) == 0 {
					continue
				}
				hits++
				entry := primaryEntry(&result.Matches[0])
				if entry.SenseCount() > 0 {
					withSenses++
				}
				for _, part := range entry.Parts {
					for _, sense := range part.Senses {
						if strings.TrimSpace(sense.Definition) != "" || strings.TrimSpace(sense.Translation) != "" {
							withDefinitions++
						}
					}
				}
			}
			if withSenses > 0 && withDefinitions == 0 {
				t.Errorf("%d lookups produced sense structure but not one definition or translation", withSenses)
			}
			if hits > 0 {
				answering++
				definitions += withDefinitions
			}
			if withSenses > 0 {
				structured++
			}
			t.Logf("%d/%d words hit, %d produced senses, %d senses carried definitions",
				hits, len(words), withSenses, withDefinitions)
		})
	}

	if answering == 0 {
		t.Skip("no installed dictionary answered the probe words")
	}
	share := float64(structured) / float64(answering)
	t.Logf("%d of %d answering dictionaries produced structure (%.0f%%), %d definitions total",
		structured, answering, share*100, definitions)
	// Measured at 77% over the development corpus. The bound is loose enough
	// that adding unparseable reference works cannot trip it, and tight enough
	// that a parser regression across the library would.
	if share < 0.5 {
		t.Errorf("only %d of %d answering dictionaries produced structure (%.0f%%)",
			structured, answering, share*100)
	}
	if definitions == 0 {
		t.Error("no definitions were extracted from any dictionary")
	}
}

// TestRealAudioResolvesFromMDD verifies the whole pronunciation chain against
// the user's own files: entry HTML to MDD key to decoded bytes.
func TestRealAudioResolvesFromMDD(t *testing.T) {
	svc := newService(t)
	checked := 0

	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		if !info.HasMDD {
			t.Logf("%s has no MDD; audio is correctly unavailable", shortName(info.Title))
			continue
		}
		result, err := svc.Lookup("abandon", service.LookupOptions{DictionaryIDs: []string{info.ID}})
		if err != nil || len(result.Matches) == 0 {
			continue
		}
		entry := primaryEntry(&result.Matches[0])

		var withAudio int
		for _, pronunciation := range entry.Pronunciations {
			if pronunciation.Audio == nil {
				continue
			}
			withAudio++
			data, contentType, err := svc.ResolveResource(pronunciation.Audio.Token)
			if err != nil {
				t.Errorf("%s %s: resolving %q failed: %v",
					shortName(info.Title), pronunciation.AudioRegion, pronunciation.Audio.ResourceRef, err)
				continue
			}
			if len(data) < 256 {
				t.Errorf("%s %s: %d bytes is too small to be a recording",
					shortName(info.Title), pronunciation.AudioRegion, len(data))
			}
			if !strings.HasPrefix(contentType, "audio/") {
				t.Errorf("%s %s: content type %q is not audio", shortName(info.Title), pronunciation.AudioRegion, contentType)
			}
			checked++
			t.Logf("%-26s %-3s %-46s %6d bytes %s",
				shortName(info.Title), pronunciation.AudioRegion, pronunciation.Audio.ResourceRef, len(data), contentType)
		}
		if withAudio == 0 {
			t.Errorf("%s has an MDD but produced no audio for 'abandon'", shortName(info.Title))
		}
	}
	if checked == 0 {
		t.Fatal("no audio was resolved from any dictionary")
	}
}

// TestRealImagesResolveFromMDD verifies the complete user path against only
// the known complete local dictionaries. CI has no licensed files and skips.
func TestRealImagesResolveFromMDD(t *testing.T) {
	svc := newService(t)
	handler := httpapi.New(svc, slog.Default()).Handler()
	tests := []struct {
		title string
		word  string
	}{
		{"Collins COBUILD overhaul", "American chameleon"},
		{"LDOCE5++ En-Cn V2.15", "LDOCE4 Page A1"},
		{"牛津高阶英汉双解词典(第8版)", "apple"},
	}
	checked := 0
	for _, tc := range tests {
		var id string
		for _, dict := range svc.Registry().All() {
			if strings.Contains(dict.Info().Title, tc.title) && dict.Info().HasMDD {
				id = dict.ID()
				break
			}
		}
		if id == "" {
			continue
		}
		opts := mdrender.UserOptions()
		result, err := svc.Lookup(tc.word, service.LookupOptions{
			DictionaryIDs: []string{id}, Mode: service.ModeExact,
			RenderMarkdown: true, MarkdownOptions: opts,
		})
		if err != nil || len(result.Matches) == 0 {
			t.Errorf("%s/%s lookup: %v", tc.title, tc.word, err)
			continue
		}
		var images []*entryir.Image
		for _, record := range result.Matches[0].Records {
			for _, sections := range [][]entryir.Section{record.Entry.Sections, record.Entry.UsageNotes, record.Entry.GrammarNotes} {
				for _, section := range sections {
					for _, block := range section.Blocks {
						if block.Image != nil {
							images = append(images, block.Image)
						}
					}
				}
			}
		}
		if len(images) == 0 {
			t.Errorf("%s/%s produced no resolved inline image", tc.title, tc.word)
			continue
		}
		image := images[0]
		if !strings.Contains(result.Markdown, "![") || !strings.Contains(result.Markdown, image.URL) {
			t.Errorf("%s/%s Markdown omitted image URL", tc.title, tc.word)
		}
		parsed, err := url.Parse(image.URL)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
		request.RemoteAddr = "127.0.0.1:54321"
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "image/") || recorder.Body.Len() < 256 {
			t.Errorf("%s/%s resource response: status=%d type=%q bytes=%d",
				tc.title, tc.word, recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.Len())
			continue
		}
		checked++
		t.Logf("%s/%s: %s, %d bytes", tc.title, tc.word, recorder.Header().Get("Content-Type"), recorder.Body.Len())
	}
	if checked == 0 {
		t.Fatal("no complete real dictionary image was validated")
	}
}

// TestUKAndUSAudioDifferOnRealDictionaries guards the mislabelling failure mode
// against real data rather than a fixture.
func TestUKAndUSAudioDifferOnRealDictionaries(t *testing.T) {
	svc := newService(t)
	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		if !info.HasMDD {
			continue
		}
		for _, word := range []string{"abandon", "run", "book"} {
			result, err := svc.Lookup(word, service.LookupOptions{DictionaryIDs: []string{info.ID}})
			if err != nil || len(result.Matches) == 0 {
				continue
			}
			var uk, us *entryir.Audio
			for _, pronunciation := range primaryEntry(&result.Matches[0]).Pronunciations {
				switch pronunciation.AudioRegion {
				case entryir.RegionUK:
					uk = pronunciation.Audio
				case entryir.RegionUS:
					us = pronunciation.Audio
				}
			}
			if uk != nil && us != nil && uk.ResourceRef == us.ResourceRef {
				t.Errorf("%s/%s: UK and US both play %q", shortName(info.Title), word, uk.ResourceRef)
			}
		}
	}
}

// TestRedirectsAreFollowed exercises @@@LINK, which several dictionaries use
// heavily and which the bundled engine does not resolve on its own.
func TestRedirectsAreFollowed(t *testing.T) {
	svc := newService(t)
	// "hello" is an @@@LINK stub pointing at "Hello!" in at least one of the
	// test dictionaries; a failure here means the stub leaked to the user.
	result, err := svc.Lookup("hello", service.LookupOptions{Mode: service.ModeExact})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) == 0 {
		t.Fatal("no dictionary resolved 'hello'")
	}
	sawRedirect := false
	for _, match := range result.Matches {
		payload, _ := json.Marshal(match.Records)
		if strings.Contains(string(payload), "@@@LINK") || strings.Contains(string(payload), "@@LINK") {
			t.Errorf("%s leaked a redirect stub into the entry", shortName(match.DictionaryTitle))
		}
		for _, record := range match.Records {
			entry := record.Entry
			if entry.Source.RedirectedFrom != "" {
				sawRedirect = true
				t.Logf("%s: %q redirected to %q",
					shortName(match.DictionaryTitle), entry.Source.RedirectedFrom, entry.Source.MatchedKey)
			}
			if entry.IsEmpty() {
				t.Errorf("%s produced an empty entry for 'hello'", shortName(match.DictionaryTitle))
			}
		}
	}
	if !sawRedirect {
		t.Log("no redirect encountered for 'hello' in this library")
	}
}

// TestBobRenderingIsValid checks the shape Bob actually receives.
func TestBobRenderingIsValid(t *testing.T) {
	svc := newService(t)
	result, err := svc.Lookup("abandon", service.LookupOptions{
		Mode:       service.ModeExact,
		RenderBob:  true,
		BobOptions: bobadapter.DefaultOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Bob == nil {
		t.Fatal("no toDict was rendered")
	}
	dict := result.Bob
	if dict.Word == "" {
		t.Error("toDict.word is empty")
	}
	// Bob requires at least one of toParagraphs or toDict to carry content.
	if len(dict.Parts) == 0 {
		t.Fatal("toDict.parts is empty, so Bob would show nothing")
	}
	for _, phonetic := range dict.Phonetics {
		if phonetic.Type != "uk" && phonetic.Type != "us" {
			t.Errorf("phonetic type %q is not a value Bob accepts", phonetic.Type)
		}
		if phonetic.TTS != nil {
			if phonetic.TTS.Type != "url" {
				t.Errorf("tts type %q, want url", phonetic.TTS.Type)
			}
			if !strings.HasPrefix(phonetic.TTS.Value, "http://127.0.0.1:") {
				t.Errorf("tts url %q is not a loopback service url", phonetic.TTS.Value)
			}
		}
	}
	for _, part := range dict.Parts {
		if part.Part == "" || len(part.Means) == 0 {
			t.Errorf("part %+v is empty", part)
		}
	}
	for _, relatedPart := range dict.RelatedWordParts {
		if len(relatedPart.Words) == 0 {
			t.Errorf("related word part %+v has no words", relatedPart)
		}
		for _, word := range relatedPart.Words {
			if strings.TrimSpace(word.Word) == "" {
				t.Errorf("related word part %+v contains an empty word", relatedPart)
			}
		}
	}
	payload, _ := json.MarshalIndent(dict, "", "  ")
	t.Logf("toDict: %d phonetics, %d parts, %d exchanges, %d related groups, %d additions, %d bytes",
		len(dict.Phonetics), len(dict.Parts), len(dict.Exchanges), len(dict.RelatedWordParts), len(dict.Additions), len(payload))
}

// TestLookupLatency records the interactive-path timings the product promises.
//
// The measured request is the one the plugin actually sends: Limit 1, because
// Bob shows a single result card and the plugin has always asked for exactly
// one match. An unbounded sweep is measured too, but only per dictionary — the
// development corpus in local_assets holds a hundred of them, and no user
// installs a hundred dictionaries to look up one word, so a total for that
// fixture would be a fact about this repository rather than about the product.
func TestLookupLatency(t *testing.T) {
	svc := newService(t)
	words := []string{"abandon", "run", "good", "book", "take", "water", "light", "make"}
	installed, _ := svc.Registry().Counts()

	interactive := service.LookupOptions{Mode: service.ModeExact, MaxExamples: 4, Limit: 1}

	started := time.Now()
	count := 0
	for _, word := range words {
		result, err := svc.Lookup(word, interactive)
		if err == nil {
			count += len(result.Matches)
		}
	}
	cold := time.Since(started)

	started = time.Now()
	for i := 0; i < 5; i++ {
		for _, word := range words {
			_, _ = svc.Lookup(word, interactive)
		}
	}
	warm := time.Since(started) / time.Duration(5*len(words))

	t.Logf("interactive (limit 1, %d dictionaries installed): cold %v for %d words (%d matches); warm %v per lookup",
		installed, cold.Round(time.Millisecond), len(words), count, warm.Round(time.Microsecond))

	// An interactive popup has to feel instant. This bound is deliberately
	// loose so the test reports a real regression, not machine noise.
	if warm > 100*time.Millisecond {
		t.Errorf("warm interactive lookup took %v, which is too slow for interactive use", warm)
	}

	// The unbounded sweep is what an API client asking for every dictionary
	// pays. Its cost is linear in dictionaries, so that is how it is bounded.
	sweep := service.LookupOptions{Mode: service.ModeExact, MaxExamples: 4}
	for _, word := range words {
		_, _ = svc.Lookup(word, sweep)
	}
	started = time.Now()
	for _, word := range words {
		_, _ = svc.Lookup(word, sweep)
	}
	perDictionary := time.Since(started) / time.Duration(len(words)*max(installed, 1))
	t.Logf("unbounded sweep: %v per dictionary per word", perDictionary.Round(time.Microsecond))
	// Generous headroom over the observed cost: this is wall-clock time on a
	// shared machine, and a bound tight enough to flake would get ignored.
	if perDictionary > 50*time.Millisecond {
		t.Errorf("unbounded lookup cost %v per dictionary, which is too slow", perDictionary)
	}
}

// TestNoDictionaryContentLeavesTheRepository asserts the licensing boundary.
func TestNoDictionaryContentLeavesTheRepository(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			// local_assets is git-ignored and is where the developer's own
			// licensed dictionaries live.
			if name == "local_assets" || name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mdx", ".mdd":
			t.Errorf("dictionary data is tracked in the repository: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	goldenDir := filepath.Join(repoRoot, "testdata", "golden")
	err = filepath.WalkDir(goldenDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".html") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if len(data) > 8<<10 {
			t.Errorf("fixture is not minimal (%d bytes): %s", len(data), path)
		}
		lower := strings.ToLower(string(data))
		for _, forbidden := range []string{
			"<link ", " id=", "id=\"", "sound://media/", "sound://colmp3/",
			"sound://uk/", "sound://us/", ".css\"", "@@@link=",
		} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("fixture contains publisher-specific skeleton/resource marker %q: %s", forbidden, path)
			}
		}
		// Both resource schemes real dictionaries use have to point at invented
		// paths. A fixture is allowed to reproduce the shape of a reference,
		// never a publisher's actual asset layout.
		for _, scheme := range []string{"sound://", "snd://"} {
			if strings.Count(lower, scheme) != strings.Count(lower, scheme+"synthetic/") {
				t.Errorf("fixture contains a non-synthetic %s audio path: %s", scheme, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func shortName(title string) string {
	title = strings.NewReplacer(" ", "-", "/", "-", "(", "", ")", "").Replace(title)
	return truncate(title, 26)
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
