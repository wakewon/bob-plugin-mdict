package service_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// BenchmarkColdStart measures what a user waits for when the service launches:
// discovery plus building every dictionary index.
func BenchmarkColdStart(b *testing.B) {
	dir := benchDictionaryDir(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		svc, err := service.New(config.Config{DictionaryDir: dir, CacheDir: b.TempDir(), Port: 15399})
		if err != nil {
			b.Fatal(err)
		}
		if err := svc.Rescan(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWarmLookup measures a repeated lookup, which is the cached path an
// interactive popup actually hits when a user re-selects the same word.
func BenchmarkWarmLookup(b *testing.B) {
	svc := benchService(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Lookup("abandon", service.LookupOptions{Mode: service.ModeExact, MaxExamples: 3}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUncachedLookup defeats the entry cache by varying an option that is
// part of the cache key, so it measures the real MDX decode plus parse cost.
func BenchmarkUncachedLookup(b *testing.B) {
	svc := benchService(b)
	words := []string{"abandon", "run", "good", "book", "take", "water", "light", "make", "hold", "set"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := svc.Lookup(words[i%len(words)], service.LookupOptions{
			Mode:        service.ModeExact,
			MaxExamples: 1 + (i % 7),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSingleDictionaryLookup measures the plugin's default configuration,
// where only the first matching dictionary answers.
func BenchmarkSingleDictionaryLookup(b *testing.B) {
	svc := benchService(b)
	all := svc.Registry().All()
	if len(all) == 0 {
		b.Skip("no dictionaries")
	}
	id := all[0].ID()
	words := []string{"abandon", "run", "good", "book", "take", "water", "light", "make", "hold", "set"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := svc.Lookup(words[i%len(words)], service.LookupOptions{
			DictionaryIDs: []string{id},
			Mode:          service.ModeExact,
			Limit:         1,
			MaxExamples:   1 + (i % 7),
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAudioResolution measures decompressing one pronunciation out of MDD.
func BenchmarkAudioResolution(b *testing.B) {
	svc := benchService(b)
	result, err := svc.Lookup("abandon", service.LookupOptions{Mode: service.ModeExact})
	if err != nil || len(result.Matches) == 0 {
		b.Skip("no match to benchmark")
	}
	var token string
	for _, match := range result.Matches {
		for _, pronunciation := range match.Entry.Pronunciations {
			if pronunciation.Audio != nil {
				token = pronunciation.Audio.Token
			}
		}
	}
	if token == "" {
		b.Skip("no audio to benchmark")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := svc.ResolveResource(token); err != nil {
			b.Fatal(err)
		}
	}
}

func benchDictionaryDir(b *testing.B) string {
	b.Helper()
	dir := os.Getenv("BOB_MDICT_TEST_DICTIONARIES")
	if dir == "" {
		_, thisFile, _, _ := runtime.Caller(0)
		dir = thisFile[:len(thisFile)-len("internal/service/bench_test.go")] + "local_assets/dictionaries"
	}
	if !benchContainsMDX(dir) {
		b.Skip("no real dictionaries available")
	}
	return dir
}

// benchContainsMDX mirrors the integration tests' skip condition.
func benchContainsMDX(root string) bool {
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

func benchService(b *testing.B) *service.Service {
	b.Helper()
	svc, err := service.New(config.Config{DictionaryDir: benchDictionaryDir(b), CacheDir: b.TempDir(), Port: 15399})
	if err != nil {
		b.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		b.Fatal(err)
	}
	return svc
}

// TestResidentMemoryReport records the memory cost of holding every index, so a
// regression that makes the service heavy is visible rather than silent.
func TestResidentMemoryReport(t *testing.T) {
	svc := newService(t)

	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)

	total, healthy := svc.Registry().Counts()
	var entries int64
	for _, dict := range svc.Registry().All() {
		entries += dict.Info().EntryCount
	}

	started := time.Now()
	for _, word := range []string{"abandon", "run", "good"} {
		_, _ = svc.Lookup(word, service.LookupOptions{Mode: service.ModeExact})
	}
	elapsed := time.Since(started)

	runtime.GC()
	runtime.ReadMemStats(&stats)
	// Without this the service is unreachable by the time stats are read and
	// the collector has already freed every index, reporting a heap near zero.
	runtime.KeepAlive(svc)

	t.Log(fmt.Sprintf(
		"%d dictionaries (%d healthy), %d headwords indexed: heap=%.1fMB sys=%.1fMB; 3 lookups in %v",
		total, healthy, entries,
		float64(stats.HeapAlloc)/(1<<20), float64(stats.Sys)/(1<<20), elapsed.Round(time.Millisecond)))
}
