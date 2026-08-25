package diagnose

import (
	"sort"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

// Container is what the MDX file itself says about the dictionary.
type Container struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Health      string   `json:"health"`
	EntryCount  int64    `json:"entryCount"`
	Encoding    string   `json:"encoding,omitempty"`
	Version     string   `json:"version,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	MDDVolumes  int      `json:"mddVolumes"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// Coverage counts how many representative samples yielded each kind of
// semantic structure.
//
// These are coverage counts, not accuracy: nothing here has been compared
// against a human reading of the entry. A field being present says the parser
// produced something of that kind, not that what it produced is right.
type Coverage struct {
	Samples      int `json:"samples"`
	Headword     int `json:"headword"`
	PartOfSpeech int `json:"partOfSpeech"`
	Definitions  int `json:"definitions"`
	Translations int `json:"translations"`
	Examples     int `json:"examples"`
	IPA          int `json:"ipa"`
	Forms        int `json:"forms"`
	Phrases      int `json:"phrases"`
	CrossRefs    int `json:"crossReferences"`
	Sections     int `json:"sections"`
	// Fallback counts samples that produced no structure at all and were
	// emitted as one untyped section.
	Fallback int `json:"fallback"`
	// Structured is how many samples yielded at least one sense.
	Structured int `json:"structured"`
	// Empty is how many samples yielded nothing worth showing.
	Empty int `json:"empty"`

	StructuredRate float64 `json:"structuredRate"`
	FallbackRate   float64 `json:"fallbackRate"`
	// MedianSenses is the middle sense count across the samples.
	MedianSenses int `json:"medianSenses"`
}

// Warning is a conservative signal that a dictionary is worth a human look.
// It is never proof that parsing is wrong.
type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Report is everything the diagnostics know about one dictionary.
type Report struct {
	Container     Container             `json:"container"`
	Profile       ProfileEvidence       `json:"profile"`
	DOM           DOMSummary            `json:"dom"`
	Coverage      Coverage              `json:"coverage"`
	Pronunciation PronunciationEvidence `json:"pronunciation"`
	Warnings      []Warning             `json:"warnings,omitempty"`
	// SampleScores records the informativeness of the chosen samples, which is
	// how a "this dictionary has nothing to sample" case stays visible.
	SampleScores []int `json:"sampleScores,omitempty"`
}

// Options configures one dictionary diagnostic.
type Options struct {
	Sampling SampleOptions
	// ProfileOverride forces a parser choice for comparison runs: "" or "auto"
	// keeps automatic detection, "generic" disables profiles, anything else
	// names a profile ID. It is a debugging aid, not a stored configuration.
	ProfileOverride string
	// ResolveAudio reports whether a reference exists in the dictionary's MDD.
	// Nil means resource resolution is not being tested, which is the normal
	// case for an MDX-only corpus.
	ResolveAudio func(ref string) bool
}

// Inspect produces the full diagnostic for one dictionary.
func Inspect(dict *mdict.Dictionary, opts Options) Report {
	// Health, title and entry count only exist once the index has been built.
	// Loading is idempotent, so a dictionary the registry already opened costs
	// nothing here, and one it has not is reported rather than misread as ok.
	_ = dict.Load()
	info := dict.Info()
	report := Report{Container: Container{
		ID:          info.ID,
		Title:       info.Title,
		Health:      string(info.Health),
		EntryCount:  info.EntryCount,
		Encoding:    info.Encoding,
		Version:     info.Version,
		CreatedAt:   info.CreatedAt,
		MDDVolumes:  info.MDDVolumes,
		Diagnostics: info.Diagnostics,
	}}
	if info.Health != mdict.HealthOK {
		report.Profile = ProfileEvidence{Selected: GenericProfileID, Strength: EvidenceAbsent}
		return report
	}

	sampling := opts.Sampling
	if sampling.Pool == 0 && sampling.Keep == 0 {
		sampling = DiagnosticSampling
	}
	samples := Samples(dict, sampling)
	if len(samples) == 0 {
		report.Profile = ProfileEvidence{Selected: GenericProfileID, Strength: EvidenceAbsent}
		report.Warnings = append(report.Warnings, Warning{
			Code:   "no-samples",
			Detail: "no record could be resolved for structural sampling",
		})
		return report
	}
	for _, sample := range samples {
		report.SampleScores = append(report.SampleScores, sample.Score)
	}

	profile, evidence := SelectProfile(info.Title, samples)
	profile, evidence = applyOverride(profile, evidence, opts.ProfileOverride)
	report.Profile = evidence
	report.DOM = SummarizeDOM(samples)
	report.Pronunciation = collectPronunciationEvidence(samples, opts.ResolveAudio)
	report.Pronunciation.MDDVolumes = info.MDDVolumes

	entries := make([]*entryir.Entry, 0, len(samples))
	for _, sample := range samples {
		entry, err := parser.Parse(sample.HTML, parser.Options{
			Headword:            sample.Key,
			Profile:             profile,
			MaxExamplesPerSense: 4,
		})
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	report.Coverage = measureCoverage(entries)
	report.Warnings = append(report.Warnings, detectWarnings(report, samples, entries)...)
	return report
}

// ApplyProfileOverride implements the development-only parser override: "auto"
// or "" keeps automatic detection, "generic" disables profiles, and anything
// else names a profile to force. It returns the profile to use and the label
// to report it under.
//
// Nothing persists it. The product always resolves a parser from the current
// MDX and the current rules, which is what lets a dictionary pick up future
// parser improvements without anyone re-recording a mapping.
func ApplyProfileOverride(profile *parser.Profile, override string) (*parser.Profile, string) {
	label := GenericProfileID
	if profile != nil {
		label = profile.ID
	}
	switch strings.TrimSpace(strings.ToLower(override)) {
	case "", "auto":
		return profile, label
	case GenericProfileID:
		return nil, GenericProfileID
	}
	forced := profiles.ByID(strings.TrimSpace(override))
	if forced == nil {
		return profile, label
	}
	return forced, forced.ID
}

// applyOverride threads the override through the evidence record.
func applyOverride(profile *parser.Profile, evidence ProfileEvidence, override string) (*parser.Profile, ProfileEvidence) {
	forced, label := ApplyProfileOverride(profile, override)
	if label != evidence.Selected {
		evidence.Override = label
	}
	evidence.Selected = label
	return forced, evidence
}

func measureCoverage(entries []*entryir.Entry) Coverage {
	coverage := Coverage{Samples: len(entries)}
	if len(entries) == 0 {
		return coverage
	}
	senseCounts := make([]int, 0, len(entries))
	for _, entry := range entries {
		senses := entry.SenseCount()
		senseCounts = append(senseCounts, senses)

		if entry.IsEmpty() {
			coverage.Empty++
		}
		if isFallback(entry) {
			coverage.Fallback++
		}
		if senses > 0 {
			coverage.Structured++
		}
		if strings.TrimSpace(entry.Headword) != "" {
			coverage.Headword++
		}
		if len(entry.Sections)+len(entry.UsageNotes)+len(entry.GrammarNotes) > 0 || entry.Etymology != "" {
			coverage.Sections++
		}
		if len(entry.Forms) > 0 {
			coverage.Forms++
		}
		if len(entry.Phrases)+len(entry.Idioms)+len(entry.PhrasalVerbs)+len(entry.Derivatives) > 0 {
			coverage.Phrases++
		}
		if len(entry.CrossReferences)+len(entry.Related)+len(entry.Synonyms)+len(entry.Antonyms) > 0 {
			coverage.CrossRefs++
		}
		for _, pronunciation := range entry.Pronunciations {
			if pronunciation.IPA != "" {
				coverage.IPA++
				break
			}
		}
		pos, definition, translation, example := false, false, false, false
		for _, part := range entry.Parts {
			if strings.TrimSpace(part.POS) != "" {
				pos = true
			}
			forEachSense(part.Senses, func(sense entryir.Sense) {
				if strings.TrimSpace(sense.Definition) != "" {
					definition = true
				}
				if strings.TrimSpace(sense.Translation) != "" {
					translation = true
				}
				if len(sense.Examples) > 0 {
					example = true
				}
			})
		}
		countIf(&coverage.PartOfSpeech, pos)
		countIf(&coverage.Definitions, definition)
		countIf(&coverage.Translations, translation)
		countIf(&coverage.Examples, example)
	}
	sort.Ints(senseCounts)
	coverage.MedianSenses = senseCounts[len(senseCounts)/2]
	coverage.StructuredRate = ratio(coverage.Structured, coverage.Samples)
	coverage.FallbackRate = ratio(coverage.Fallback, coverage.Samples)
	return coverage
}

// isFallback reports whether the parser gave up and emitted the record as one
// untyped section.
func isFallback(entry *entryir.Entry) bool {
	return len(entry.Parts) == 0 &&
		len(entry.Sections) == 1 &&
		entry.Sections[0].Title == parser.FallbackSectionTitle
}

func forEachSense(senses []entryir.Sense, visit func(entryir.Sense)) {
	for _, sense := range senses {
		visit(sense)
		forEachSense(sense.Subsenses, visit)
	}
}

func countIf(target *int, condition bool) {
	if condition {
		*target++
	}
}

func ratio(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}
