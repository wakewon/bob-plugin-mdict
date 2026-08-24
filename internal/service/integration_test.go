package service_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// dictionaryDir returns the real dictionary library to test against.
//
// These are the developer's own licensed dictionaries. They are never committed
// to the repository, so CI and any other checkout simply skip these tests
// instead of failing.
func dictionaryDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("BOB_MDICT_TEST_DICTIONARIES"); dir != "" {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("BOB_MDICT_TEST_DICTIONARIES is set to %q but it does not exist", dir)
		}
		if !containsMDX(dir) {
			t.Skipf("%q contains no .mdx files", dir)
		}
		return dir
	}
	_, thisFile, _, _ := runtime.Caller(0)
	fallback := filepath.Join(filepath.Dir(thisFile), "..", "..", "local_assets", "dictionaries")
	// A directory that exists but holds no dictionaries is the same situation
	// as no directory at all: skip rather than fail, so a fresh checkout and CI
	// behave identically.
	if !containsMDX(fallback) {
		t.Skip("no real dictionaries available; set BOB_MDICT_TEST_DICTIONARIES to run integration tests")
	}
	return fallback
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

func TestRealDictionariesAreDiscoveredAndHealthy(t *testing.T) {
	svc := newService(t)
	total, healthy := svc.Registry().Counts()
	if total == 0 {
		t.Fatal("no dictionaries discovered")
	}
	if healthy != total {
		for _, dict := range svc.Registry().All() {
			info := dict.Info()
			if info.Health != "ok" {
				t.Errorf("%s is unavailable: %v", info.Title, info.Diagnostics)
			}
		}
	}
	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		t.Logf("%-46s entries=%-8d mdd=%d profile=%s",
			truncate(info.Title, 46), info.EntryCount, info.MDDVolumes, svc.ProfileID(dict))
		if info.EntryCount == 0 {
			t.Errorf("%s reported zero entries", info.Title)
		}
	}
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
	want := bobadapter.Render(result.Matches[0].Entry, opts)
	if !reflect.DeepEqual(result.Bob, want) {
		t.Fatal("Bob output was not rendered exclusively from the first dictionary match")
	}
}

// TestRealLookupsProduceStructure is the anti-"pseudo-completion" check: every
// dictionary must yield real parts and senses, not a blob of stripped text.
func TestRealLookupsProduceStructure(t *testing.T) {
	svc := newService(t)
	words := []string{"abandon", "run", "good", "book", "take", "quickly", "water", "light"}

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
				entry := result.Matches[0].Entry
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
			if hits < len(words)/2 {
				t.Errorf("only %d/%d probe words resolved", hits, len(words))
			}
			if withSenses < hits {
				t.Errorf("%d/%d hits produced no structured senses", hits-withSenses, hits)
			}
			if withDefinitions == 0 {
				t.Error("no definitions were extracted at all")
			}
			t.Logf("%d/%d words hit, %d produced senses, %d senses carried definitions",
				hits, len(words), withSenses, withDefinitions)
		})
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
		entry := result.Matches[0].Entry

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
			for _, pronunciation := range result.Matches[0].Entry.Pronunciations {
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
		payload, _ := json.Marshal(match.Entry)
		if strings.Contains(string(payload), "@@@LINK") || strings.Contains(string(payload), "@@LINK") {
			t.Errorf("%s leaked a redirect stub into the entry", shortName(match.DictionaryTitle))
		}
		if match.Entry.Source.RedirectedFrom != "" {
			sawRedirect = true
			t.Logf("%s: %q redirected to %q",
				shortName(match.DictionaryTitle), match.Entry.Source.RedirectedFrom, match.Entry.Source.MatchedKey)
		}
		if match.Entry.IsEmpty() {
			t.Errorf("%s produced an empty entry for 'hello'", shortName(match.DictionaryTitle))
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
	payload, _ := json.MarshalIndent(dict, "", "  ")
	t.Logf("toDict: %d phonetics, %d parts, %d exchanges, %d additions, %d bytes",
		len(dict.Phonetics), len(dict.Parts), len(dict.Exchanges), len(dict.Additions), len(payload))
}

// TestLookupLatency records the interactive-path timings the product promises.
func TestLookupLatency(t *testing.T) {
	svc := newService(t)
	words := []string{"abandon", "run", "good", "book", "take", "water", "light", "make"}

	started := time.Now()
	count := 0
	for _, word := range words {
		result, err := svc.Lookup(word, service.LookupOptions{Mode: service.ModeExact, MaxExamples: 4})
		if err == nil {
			count += len(result.Matches)
		}
	}
	cold := time.Since(started)

	started = time.Now()
	for i := 0; i < 5; i++ {
		for _, word := range words {
			_, _ = svc.Lookup(word, service.LookupOptions{Mode: service.ModeExact, MaxExamples: 4})
		}
	}
	warm := time.Since(started) / time.Duration(5*len(words))

	t.Logf("cold: %v for %d words (%d matches); warm: %v per lookup",
		cold.Round(time.Millisecond), len(words), count, warm.Round(time.Microsecond))

	// An interactive popup has to feel instant. This bound is deliberately
	// loose so the test reports a real regression, not machine noise.
	if warm > 100*time.Millisecond {
		t.Errorf("warm lookup took %v, which is too slow for interactive use", warm)
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
		if strings.Count(lower, "sound://") != strings.Count(lower, "sound://synthetic/") {
			t.Errorf("fixture contains a non-synthetic audio path: %s", path)
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
