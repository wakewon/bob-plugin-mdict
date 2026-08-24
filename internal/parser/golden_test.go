package parser_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

var update = flag.Bool("update", false, "rewrite golden expectations")

const goldenDir = "../../testdata/golden"

// fakeAudio resolves every reference, so the golden files record exactly which
// resource references the parser attributed to which pronunciation.
type fakeAudio struct{}

func (fakeAudio) ResolveAudio(ref string) *entryir.Audio {
	return &entryir.Audio{
		ResourceRef: ref,
		Token:       "TOKEN",
		URL:         "http://127.0.0.1:15321/v2/resource/TOKEN",
		MIMEType:    "audio/mpeg",
	}
}

// profileForFixture maps a fixture name onto the profile it exercises.
// "generic-" fixtures deliberately run with no profile at all.
func profileForFixture(name string) *parser.Profile {
	switch {
	case strings.HasPrefix(name, "profile-oald8"):
		return profiles.ByID("oald8")
	case strings.HasPrefix(name, "profile-ldoce5pp"):
		return profiles.ByID("ldoce5pp")
	case strings.HasPrefix(name, "profile-collins"):
		return profiles.ByID("collins-cobuild-overhaul")
	case strings.HasPrefix(name, "profile-ode"):
		return profiles.ByID("ode-living-online")
	default:
		return nil
	}
}

func TestGolden(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join(goldenDir, "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no golden fixtures found")
	}

	for _, htmlPath := range matches {
		name := strings.TrimSuffix(filepath.Base(htmlPath), ".html")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(htmlPath)
			if err != nil {
				t.Fatal(err)
			}
			entry, err := parser.Parse(raw, parser.Options{
				Headword:            name,
				Profile:             profileForFixture(name),
				Audio:               fakeAudio{},
				MaxExamplesPerSense: 4,
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			// Source is populated by the service, not the parser; zero it so
			// the fixtures stay stable.
			entry.Source = entryir.Source{Profile: entry.Source.Profile}

			got, err := json.MarshalIndent(entry, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')

			jsonPath := filepath.Join(goldenDir, name+".json")
			if *update {
				if err := os.WriteFile(jsonPath, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatalf("missing expectation (run with -update): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("Entry IR mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}
