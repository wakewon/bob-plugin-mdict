package service_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

// The service is used from many goroutines at once: Bob's HTTP requests each
// get their own, and a rescan can land in the middle of them. Three mutexes
// guard that — the profile map, the entry cache, and the registry — and until
// this file existed nothing in the test suite ever ran two goroutines at the
// same time, so `go test -race` was checking single-threaded execution.
//
// These tests use synthetic fixtures rather than the development corpus on
// purpose: race detection needs contention, not volume, and a suite that takes
// an hour is a suite nobody runs.

// syntheticService builds a small multi-dictionary service from scratch.
func syntheticService(t *testing.T, dictionaries, entries int) *service.Service {
	t.Helper()
	root := t.TempDir()
	for d := 0; d < dictionaries; d++ {
		records := make([]testmdx.Entry, 0, entries*2)
		for e := 0; e < entries; e++ {
			key := fmt.Sprintf("word%02d", e)
			html := fmt.Sprintf(`<article><h1>%s</h1><div class="sense">`+
				`<span class="pos">noun</span><span class="definition">synthetic meaning %d in book %d</span>`+
				`<span class="example">a synthetic example</span></div></article>`, key, e, d)
			records = append(records, testmdx.Entry{Key: key, HTML: html})
			// A second record under the same key, so multi-record presentation
			// and sibling navigation are exercised concurrently too.
			records = append(records, testmdx.Entry{Key: key, HTML: strings.Replace(html, "noun", "verb", 1)})
		}
		path := filepath.Join(root, fmt.Sprintf("book%02d", d), fmt.Sprintf("book%02d.mdx", d))
		if err := mkdirWrite(path, records); err != nil {
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

func mkdirWrite(path string, records []testmdx.Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return testmdx.Write(path, records)
}

// TestConcurrentLookupsShareCacheAndProfilesSafely drives the two mutexes that
// a real Bob session contends on: the profile map, filled lazily on first use,
// and the entry cache, read and written by every lookup.
//
// Correctness is asserted as well as safety. A cache that races could return
// another dictionary's entry, and that would be a silent wrong answer rather
// than a crash, so every goroutine checks it got what it asked for.
func TestConcurrentLookupsShareCacheAndProfilesSafely(t *testing.T) {
	svc := syntheticService(t, 4, 8)
	ids := make([]string, 0, 4)
	for _, dict := range svc.Registry().All() {
		ids = append(ids, dict.ID())
	}
	if len(ids) != 4 {
		t.Fatalf("expected 4 synthetic dictionaries, got %d", len(ids))
	}

	var wg sync.WaitGroup
	errs := make(chan string, 512)
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < 12; round++ {
				id := ids[(worker+round)%len(ids)]
				word := fmt.Sprintf("word%02d", (worker*round)%8)
				result, err := svc.Lookup(word, service.LookupOptions{
					DictionaryIDs: []string{id},
					Mode:          service.ModeExact,
					MaxExamples:   4,
					RenderBob:     true,
					BobOptions:    bobadapter.DefaultOptions(),
				})
				if err != nil {
					errs <- fmt.Sprintf("lookup %s/%s: %v", id, word, err)
					continue
				}
				if len(result.Matches) != 1 {
					errs <- fmt.Sprintf("lookup %s/%s returned %d matches", id, word, len(result.Matches))
					continue
				}
				match := result.Matches[0]
				if match.DictionaryID != id {
					errs <- fmt.Sprintf("asked %s, got entry from %s", id, match.DictionaryID)
				}
				if match.LookupKey != word {
					errs <- fmt.Sprintf("asked %q, got key %q", word, match.LookupKey)
				}
				if result.Bob == nil || result.Bob.Word != word {
					errs <- fmt.Sprintf("Bob card for %q is wrong: %+v", word, result.Bob)
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	reportConcurrencyErrors(t, errs)
}

// TestConcurrentRescanDoesNotCorruptInFlightLookups is the harder case: Rescan
// replaces the profile map, the cache and the whole registry while lookups are
// reading them. A lookup may legitimately fail while the registry is being
// rebuilt — what it must never do is return another dictionary's content, or
// tear.
func TestConcurrentRescanDoesNotCorruptInFlightLookups(t *testing.T) {
	svc := syntheticService(t, 3, 6)

	var wg sync.WaitGroup
	errs := make(chan string, 512)
	stop := make(chan struct{})

	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				word := fmt.Sprintf("word%02d", worker%6)
				result, err := svc.Lookup(word, service.LookupOptions{
					Mode: service.ModeExact, Limit: 1, MaxExamples: 2,
					RenderMarkdown:  true,
					MarkdownOptions: mdrender.UserOptions(),
				})
				if err != nil || len(result.Matches) == 0 {
					// A miss during a registry rebuild is acceptable.
					continue
				}
				if result.Matches[0].LookupKey != word {
					errs <- fmt.Sprintf("asked %q, got key %q", word, result.Matches[0].LookupKey)
				}
				if !strings.HasPrefix(result.Markdown, "# "+word) {
					errs <- fmt.Sprintf("Markdown for %q starts %q", word, firstLine(result.Markdown))
				}
			}
		}(worker)
	}

	for i := 0; i < 4; i++ {
		if err := svc.Rescan(); err != nil {
			t.Errorf("rescan %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
	close(errs)
	reportConcurrencyErrors(t, errs)
}

// TestConcurrentResourceTokensResolveToTheirOwnDictionary covers the tokenizer
// and the registry lookup behind /v2/resource, which Bob hits once per audio
// or image element — several at a time, from a page it has just rendered.
func TestConcurrentResourceTokensResolveToTheirOwnDictionary(t *testing.T) {
	svc := syntheticService(t, 3, 4)

	var wg sync.WaitGroup
	errs := make(chan string, 256)
	for worker := 0; worker < 12; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < 10; round++ {
				// An unknown token must be refused, never resolved to whatever
				// another goroutine happens to be minting.
				if _, _, err := svc.ResolveResource(fmt.Sprintf("not-a-token-%d-%d", worker, round)); err == nil {
					errs <- "an invented resource token resolved"
				}
				svc.ProfileID(svc.Registry().All()[round%3])
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	reportConcurrencyErrors(t, errs)
}

func reportConcurrencyErrors(t *testing.T, errs <-chan string) {
	t.Helper()
	seen := map[string]int{}
	for message := range errs {
		seen[message]++
	}
	for message, count := range seen {
		t.Errorf("%s (x%d)", message, count)
	}
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}
