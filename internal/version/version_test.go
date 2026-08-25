package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate version test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

func readRepositoryFile(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReleaseVersionSourcesAreConsistent(t *testing.T) {
	root := repositoryRoot(t)
	want := strings.TrimSpace(string(readRepositoryFile(t, root, "VERSION")))
	if Version != want {
		t.Fatalf("internal version = %q, VERSION = %q", Version, want)
	}

	var info struct {
		Version       string `json:"version"`
		MinBobVersion string `json:"minBobVersion"`
	}
	if err := json.Unmarshal(readRepositoryFile(t, root, "plugin/info.json"), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version != want {
		t.Errorf("plugin version = %q, want %q", info.Version, want)
	}
	if info.MinBobVersion != "1.20.0" {
		t.Errorf("minBobVersion = %q, want 1.20.0 for query.originalText", info.MinBobVersion)
	}

	var appcast struct {
		Versions []struct {
			Version       string `json:"version"`
			MinBobVersion string `json:"minBobVersion"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(readRepositoryFile(t, root, "appcast.json"), &appcast); err != nil {
		t.Fatal(err)
	}
	for _, release := range appcast.Versions {
		if release.MinBobVersion != info.MinBobVersion {
			t.Errorf("appcast %s minBobVersion = %q, plugin = %q", release.Version, release.MinBobVersion, info.MinBobVersion)
		}
	}

	formula := string(readRepositoryFile(t, root, "packaging/homebrew/bob-mdict.rb.tmpl"))
	for _, placeholder := range []string{"@VERSION@", "@ARM64_SHA256@", "@AMD64_SHA256@"} {
		if !strings.Contains(formula, placeholder) {
			t.Errorf("Homebrew formula template is missing %s", placeholder)
		}
	}
}
