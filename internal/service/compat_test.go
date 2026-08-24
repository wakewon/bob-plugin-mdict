package service_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// TestCompatibilityMatrix probes every capability the product claims against
// every real dictionary available, and writes a Markdown table.
//
// It reports capabilities, never content: the output names dictionaries and
// ticks boxes, so it can be published without redistributing anything.
func TestCompatibilityMatrix(t *testing.T) {
	svc := newService(t)
	probes := []string{"abandon", "run", "good", "book", "take", "light", "hello", "quickly", "naive"}

	type row struct {
		title   string
		profile string
		cells   map[string]string
	}
	capabilities := []string{
		"MDX open", "Metadata", "Exact lookup", "Normalized lookup", "HTML decode",
		"Redirects", "MDD detected", "Audio found", "UK/US split", "IPA", "POS",
		"Definitions", "Translations", "Examples", "Forms", "Phrases/Idioms",
		"Cross-refs", "MDD images", "MDD audio files", "Parser",
	}

	var rows []row
	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		cells := map[string]string{}
		mark := func(name string, ok bool) {
			if ok {
				cells[name] = "yes"
			} else if _, exists := cells[name]; !exists {
				cells[name] = "no"
			}
		}

		mark("MDX open", info.Health == "ok")
		mark("Metadata", info.Title != "" && info.EntryCount > 0)
		mark("MDD detected", info.HasMDD)
		cells["Parser"] = svc.ProfileID(dict)

		for _, word := range probes {
			result, err := svc.Lookup(word, service.LookupOptions{
				DictionaryIDs: []string{info.ID}, Mode: service.ModeExact, MaxExamples: 4,
			})
			if err != nil || len(result.Matches) == 0 {
				continue
			}
			entry := primaryEntry(&result.Matches[0])
			mark("Exact lookup", true)
			mark("HTML decode", entry.SenseCount() > 0 || len(entry.Sections) > 0)
			mark("Redirects", entry.Source.RedirectedFrom != "")

			var uk, us *entryir.Audio
			for _, pronunciation := range entry.Pronunciations {
				mark("IPA", pronunciation.IPA != "")
				if pronunciation.Audio != nil {
					mark("Audio found", true)
					switch pronunciation.AudioRegion {
					case entryir.RegionUK:
						uk = pronunciation.Audio
					case entryir.RegionUS:
						us = pronunciation.Audio
					}
				}
			}
			mark("UK/US split", uk != nil && us != nil && uk.ResourceRef != us.ResourceRef)

			for _, part := range entry.Parts {
				mark("POS", part.POS != "")
				for _, sense := range part.Senses {
					mark("Definitions", strings.TrimSpace(sense.Definition) != "")
					mark("Translations", strings.TrimSpace(sense.Translation) != "")
					mark("Examples", len(sense.Examples) > 0)
				}
			}
			mark("Forms", len(entry.Forms) > 0)
			mark("Phrases/Idioms", len(entry.Phrases)+len(entry.Idioms)+len(entry.PhrasalVerbs) > 0)
		}

		// Uppercase and NFD input exercise the normalization fallbacks.
		normalized := 0
		for _, variant := range []string{"ABANDON", " abandon ", "Naïve"} {
			result, err := svc.Lookup(variant, service.LookupOptions{DictionaryIDs: []string{info.ID}})
			if err == nil && len(result.Matches) > 0 {
				normalized++
			}
		}
		mark("Normalized lookup", normalized > 0)

		// Cross-references show up as extracted synonyms and related words.
		crossRefs := false
		for _, word := range probes {
			result, err := svc.Lookup(word, service.LookupOptions{DictionaryIDs: []string{info.ID}})
			if err != nil || len(result.Matches) == 0 {
				continue
			}
			entry := primaryEntry(&result.Matches[0])
			if len(entry.Synonyms) > 0 || len(entry.WordFamily) > 0 {
				crossRefs = true
				break
			}
			for _, part := range entry.Parts {
				for _, sense := range part.Senses {
					if len(sense.Synonyms) > 0 || len(sense.Antonyms) > 0 {
						crossRefs = true
					}
				}
			}
			if crossRefs {
				break
			}
		}
		mark("Cross-refs", crossRefs)

		// Image and audio availability are facts about the MDD, not guesses
		// from a handful of sampled entries.
		kinds := dict.ResourceKinds()
		images := kinds[".jpg"] + kinds[".jpeg"] + kinds[".png"] + kinds[".gif"] + kinds[".webp"] + kinds[".svg"]
		sounds := kinds[".mp3"] + kinds[".wav"] + kinds[".ogg"] + kinds[".spx"]
		cells["MDD images"] = countCell(images)
		cells["MDD audio files"] = countCell(sounds)

		for _, capability := range capabilities {
			if _, ok := cells[capability]; !ok {
				cells[capability] = "no"
			}
		}
		rows = append(rows, row{title: info.Title, profile: svc.ProfileID(dict), cells: cells})
	}

	var out strings.Builder
	out.WriteString("| Capability |")
	for _, r := range rows {
		out.WriteString(" " + truncate(r.title, 34) + " |")
	}
	out.WriteString("\n|---|")
	for range rows {
		out.WriteString("---|")
	}
	out.WriteString("\n")
	symbol := map[string]string{"yes": "✅", "no": "—"}
	for _, capability := range capabilities {
		out.WriteString("| " + capability + " |")
		for _, r := range rows {
			value := r.cells[capability]
			if mapped, ok := symbol[value]; ok {
				value = mapped
			} else {
				value = "`" + value + "`"
			}
			out.WriteString(" " + value + " |")
		}
		out.WriteString("\n")
	}

	table := out.String()
	t.Log("\n" + table)

	// The generated table is a development artefact; keep it out of the
	// repository alongside the dictionaries it was produced from.
	if dir := os.Getenv("BOB_MDICT_MATRIX_OUT"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "compatibility-matrix.md"), []byte(table), 0o644)
			t.Logf("wrote %s", filepath.Join(dir, "compatibility-matrix.md"))
		}
	}
	fmt.Fprint(os.Stderr, "")
}

// probeRawContains reports whether any probe entry references a marker, by
// checking the parsed entry's own recorded references.
func probeRawContains(svc *service.Service, dictionaryID string, probes []string, marker string) bool {
	for _, word := range probes {
		result, err := svc.Lookup(word, service.LookupOptions{DictionaryIDs: []string{dictionaryID}, Debug: true})
		if err != nil || len(result.Matches) == 0 {
			continue
		}
		for _, record := range result.Matches[0].Records {
			if strings.Contains(strings.ToLower(entrySignature(record.Entry)), strings.ToLower(marker)) {
				return true
			}
		}
	}
	return false
}

// countCell renders a resource count, or an em dash when there are none.
func countCell(count int) string {
	if count == 0 {
		return "no"
	}
	return fmt.Sprintf("%d", count)
}

func entrySignature(entry *entryir.Entry) string {
	var builder strings.Builder
	for _, pronunciation := range entry.Pronunciations {
		if pronunciation.Audio != nil {
			builder.WriteString(pronunciation.Audio.ResourceRef)
		}
	}
	for _, part := range entry.Parts {
		for _, sense := range part.Senses {
			builder.WriteString(sense.Definition)
			for _, example := range sense.Examples {
				if example.Audio != nil {
					builder.WriteString(example.Audio.ResourceRef)
				}
			}
		}
	}
	return builder.String()
}
