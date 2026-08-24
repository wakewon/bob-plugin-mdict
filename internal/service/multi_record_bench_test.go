package service_test

import (
	"path/filepath"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/testmdx"
)

func benchmarkSyntheticRecordCount(b *testing.B, recordCount int) {
	root := b.TempDir()
	entries := make([]testmdx.Entry, recordCount)
	for i := range entries {
		pos := "noun"
		if i%2 == 1 {
			pos = "verb"
		}
		entries[i] = testmdx.Entry{
			Key:  "flimber",
			HTML: syntheticCaseHTML("flimber", "synthetic "+pos+" record "+string(rune('A'+i))),
		}
	}
	if err := testmdx.Write(filepath.Join(root, "synthetic.mdx"), entries); err != nil {
		b.Fatal(err)
	}
	svc, err := service.New(config.Config{DictionaryDir: root, CacheDir: b.TempDir(), Port: 15321})
	if err != nil {
		b.Fatal(err)
	}
	if err := svc.Rescan(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, lookupErr := svc.Lookup("flimber", service.LookupOptions{Limit: 1, MaxExamples: i + 1})
		if lookupErr != nil || len(result.Matches) != 1 {
			b.Fatalf("matches=%d err=%v", len(result.Matches), lookupErr)
		}
		if len(result.Matches[0].Records) != recordCount {
			b.Fatalf("records=%d, want %d", len(result.Matches[0].Records), recordCount)
		}
	}
}

func BenchmarkSyntheticSingleRecordLookup(b *testing.B) { benchmarkSyntheticRecordCount(b, 1) }

func BenchmarkSyntheticThreeRecordLookup(b *testing.B) { benchmarkSyntheticRecordCount(b, 3) }
