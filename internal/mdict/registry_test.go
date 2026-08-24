package mdict

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitMDDStem(t *testing.T) {
	cases := []struct {
		filename string
		stem     string
		volume   int
	}{
		{"Dict.mdd", "Dict", 0},
		{"Dict.1.mdd", "Dict", 1},
		{"Dict.2.mdd", "Dict", 2},
		{"Dict.10.mdd", "Dict", 10},
		{"My.Dict.Name.mdd", "My.Dict.Name", 0},
		{"My.Dict.Name.3.mdd", "My.Dict.Name", 3},
	}
	for _, tc := range cases {
		stem, volume := splitMDDStem(tc.filename)
		if stem != tc.stem || volume != tc.volume {
			t.Errorf("splitMDDStem(%q) = (%q, %d), want (%q, %d)", tc.filename, stem, volume, tc.stem, tc.volume)
		}
	}
}

// TestScanGroupsVolumes checks discovery without needing real dictionary data:
// the files are empty, so they are found and grouped but never opened.
func TestScanGroupsVolumes(t *testing.T) {
	root := t.TempDir()
	write := func(parts ...string) {
		path := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Alpha", "Alpha.mdx")
	write("Alpha", "Alpha.mdd")
	write("Alpha", "Alpha.2.mdd")
	write("Alpha", "Alpha.1.mdd")
	write("Alpha", "Alpha.css")
	write("nested", "Beta", "Beta.mdx")
	write("nested", "Beta", "Unrelated.mdd")
	write("Alpha", "._Alpha.mdx")

	registry := NewRegistry(root)
	if err := registry.Scan(); err != nil {
		t.Fatal(err)
	}
	all := registry.All()
	if len(all) != 2 {
		t.Fatalf("found %d dictionaries, want 2: %+v", len(all), all)
	}

	byTitle := map[string]*Dictionary{}
	for _, dict := range all {
		byTitle[dict.Info().Title] = dict
	}

	alpha, ok := byTitle["Alpha"]
	if !ok {
		t.Fatal("Alpha not discovered")
	}
	volumes := alpha.MDDPaths()
	if len(volumes) != 3 {
		t.Fatalf("Alpha has %d MDD volumes, want 3: %v", len(volumes), volumes)
	}
	// The unnumbered volume must come first, then .1, then .2.
	wantOrder := []string{"Alpha.mdd", "Alpha.1.mdd", "Alpha.2.mdd"}
	for i, want := range wantOrder {
		if filepath.Base(volumes[i]) != want {
			t.Errorf("volume %d is %q, want %q", i, filepath.Base(volumes[i]), want)
		}
	}

	beta, ok := byTitle["Beta"]
	if !ok {
		t.Fatal("Beta not discovered in nested directory")
	}
	if len(beta.MDDPaths()) != 0 {
		t.Errorf("Beta claimed unrelated MDD files: %v", beta.MDDPaths())
	}
}

func TestScanMissingRootIsNotAnError(t *testing.T) {
	registry := NewRegistry(filepath.Join(t.TempDir(), "does-not-exist"))
	if err := registry.Scan(); err != nil {
		t.Fatalf("scan of a missing directory should succeed, got %v", err)
	}
	if total, healthy := registry.Counts(); total != 0 || healthy != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", total, healthy)
	}
}

func TestStableIDIsDeterministicAndOpaque(t *testing.T) {
	path := "/Users/someone/Library/Application Support/bob-mdict/dictionaries/A/A.mdx"
	first, second := stableID(path), stableID(path)
	if first != second {
		t.Error("stableID is not deterministic")
	}
	if stableID(path) == stableID(path+"x") {
		t.Error("different paths produced the same id")
	}
	// The ID leaves the process in API responses; it must not carry the path.
	for _, fragment := range []string{"Users", "someone", "mdx", "/"} {
		if contains(first, fragment) {
			t.Errorf("id %q leaks path fragment %q", first, fragment)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
