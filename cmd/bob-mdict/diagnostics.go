package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// The diagnostic commands are development tools. They answer three questions
// about a dictionary nobody has looked at yet: does the container open, which
// parser was chosen and on what evidence, and how much semantic structure the
// parser actually recovers from records this dictionary really contains.
//
// They report structure — tags, classes, counts, rates — and never dictionary
// text, so their output can be kept and discussed freely.

func diagnoseOptions(parserOverride string, dict *mdict.Dictionary) diagnose.Options {
	opts := diagnose.Options{
		Sampling:        diagnose.DiagnosticSampling,
		ProfileOverride: parserOverride,
	}
	// Resource resolution is only a meaningful question when the MDD is
	// actually present. For an MDX-only corpus the answer is "not tested",
	// which is different from "no audio".
	if dict != nil && dict.Info().MDDVolumes > 0 {
		opts.ResolveAudio = dict.HasResource
	}
	return opts
}

func runDiagnoseOne(svc *service.Service, selector, parserOverride, outDir string, asJSON bool) error {
	loadDictionaries(svc)
	dict, err := findDictionary(svc, selector)
	if err != nil {
		return err
	}
	report := diagnose.Inspect(dict, diagnoseOptions(parserOverride, dict))

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Print(renderReport(report))
	}
	if outDir == "" {
		return nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(outDir, "diagnose-"+report.Container.ID+".json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", path)
	return nil
}

func runDiagnoseCorpus(svc *service.Service, parserOverride, outDir string, asJSON bool) error {
	started := time.Now()
	loadDictionaries(svc)
	total, _ := svc.Registry().Counts()
	fmt.Fprintf(os.Stderr, "diagnosing %d dictionaries in %s\n", total, svc.Config().DictionaryDir)

	opts := diagnoseOptions(parserOverride, nil)
	report := diagnose.Corpus(svc.Registry(), opts, func(index, count int, title string) {
		fmt.Fprintf(os.Stderr, "\r[%3d/%3d] %-50.50s", index, count, title)
	})
	fmt.Fprintf(os.Stderr, "\rdone in %s%-40s\n", time.Since(started).Round(time.Millisecond), "")

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Print(report.Markdown())
	}
	if outDir == "" {
		return nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(outDir, "corpus.json")
	if err := os.WriteFile(jsonPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	mdPath := filepath.Join(outDir, "corpus.md")
	if err := os.WriteFile(mdPath, []byte(report.Markdown()), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", jsonPath, "and", mdPath)
	return nil
}

// findDictionary accepts a stable ID or any case-insensitive fragment of a
// title or filename, because nobody remembers a sixteen-character hash.
func findDictionary(svc *service.Service, selector string) (*mdict.Dictionary, error) {
	selector = strings.TrimSpace(selector)
	if dict, ok := svc.Registry().ByID(selector); ok {
		return dict, nil
	}
	needle := strings.ToLower(selector)
	var matches []*mdict.Dictionary
	for _, dict := range svc.Registry().All() {
		haystack := strings.ToLower(dict.Info().Title + " " + filepath.Base(dict.SourcePath()))
		if strings.Contains(haystack, needle) {
			matches = append(matches, dict)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no dictionary matches %q", selector)
	case 1:
		return matches[0], nil
	}
	var names []string
	for _, dict := range matches {
		names = append(names, fmt.Sprintf("%s (%s)", dict.Info().Title, dict.ID()))
	}
	sort.Strings(names)
	return nil, fmt.Errorf("%q matches %d dictionaries:\n  %s", selector, len(matches), strings.Join(names, "\n  "))
}

func renderReport(report diagnose.Report) string {
	var out strings.Builder
	container := report.Container
	fmt.Fprintf(&out, "%s\n", container.Title)
	fmt.Fprintf(&out, "  id           : %s\n", container.ID)
	fmt.Fprintf(&out, "  health       : %s\n", container.Health)
	fmt.Fprintf(&out, "  entries      : %d\n", container.EntryCount)
	fmt.Fprintf(&out, "  encoding     : %s (engine %s, created %s)\n",
		orDash(container.Encoding), orDash(container.Version), orDash(container.CreatedAt))
	fmt.Fprintf(&out, "  mdd volumes  : %d\n", container.MDDVolumes)
	for _, diagnostic := range container.Diagnostics {
		fmt.Fprintf(&out, "  ! %s\n", diagnostic)
	}

	if report.Profile.Override != "" {
		fmt.Fprintf(&out, "\nparser         : %s (forced by --parser; detection said %s, evidence %s over %d samples)\n",
			report.Profile.Override, detectedProfile(report.Profile), report.Profile.Strength, report.Profile.Samples)
	} else {
		fmt.Fprintf(&out, "\nparser         : %s (evidence: %s, over %d samples)\n",
			report.Profile.Selected, report.Profile.Strength, report.Profile.Samples)
	}
	for _, candidate := range report.Profile.Candidates {
		fmt.Fprintf(&out, "  candidate    : %-28s matched %d/%d samples (score %d)\n",
			candidate.ID, candidate.Matched, report.Profile.Samples, candidate.Score)
	}

	dom := report.DOM
	fmt.Fprintf(&out, "\nDOM\n")
	fmt.Fprintf(&out, "  median record: %d bytes, max depth %d\n", dom.MedianBytes, dom.MaxDepth)
	fmt.Fprintf(&out, "  tags         : %s\n", joinCounts(dom.Tags, 10))
	fmt.Fprintf(&out, "  classes      : %s\n", joinCounts(dom.Classes, 14))
	fmt.Fprintf(&out, "  attributes   : %s\n", joinCounts(dom.Attributes, 8))
	if len(dom.Signatures) > 0 {
		fmt.Fprintf(&out, "  signatures   : %s\n", strings.Join(dom.Signatures, "  "))
	}
	if len(dom.ReferenceSchemes) > 0 {
		fmt.Fprintf(&out, "  references   : %s\n", joinCounts(dom.ReferenceSchemes, 8))
	}
	if dom.FamilyKey != "" {
		fmt.Fprintf(&out, "  family key   : %s\n", dom.FamilyKey)
	}

	coverage := report.Coverage
	fmt.Fprintf(&out, "\nextraction coverage over %d samples\n", coverage.Samples)
	for _, row := range []struct {
		name  string
		value int
	}{
		{"headword", coverage.Headword},
		{"part of speech", coverage.PartOfSpeech},
		{"definitions", coverage.Definitions},
		{"translations", coverage.Translations},
		{"examples", coverage.Examples},
		{"IPA", coverage.IPA},
		{"forms", coverage.Forms},
		{"phrases/idioms", coverage.Phrases},
		{"cross-references", coverage.CrossRefs},
		{"other sections", coverage.Sections},
	} {
		fmt.Fprintf(&out, "  %-16s %s\n", row.name, bar(row.value, coverage.Samples))
	}
	fmt.Fprintf(&out, "  %-16s %.0f%%   fallback %.0f%%   median senses %d\n",
		"structural", 100*coverage.StructuredRate, 100*coverage.FallbackRate, coverage.MedianSenses)

	pronunciation := report.Pronunciation
	fmt.Fprintf(&out, "\npronunciation\n")
	fmt.Fprintf(&out, "  IPA in       : %d/%d samples\n", pronunciation.IPASamples, coverage.Samples)
	fmt.Fprintf(&out, "  audio refs in: %d/%d samples", pronunciation.AudioRefSamples, coverage.Samples)
	if len(pronunciation.RegionMarkers) > 0 {
		fmt.Fprintf(&out, " (regions: %s)", strings.Join(pronunciation.RegionMarkers, ", "))
	}
	out.WriteString("\n")
	// The distinction that matters for an MDX-only corpus: a reference in the
	// markup is a dictionary fact, a playable file is an MDD fact.
	if pronunciation.MDDVolumes == 0 {
		fmt.Fprintf(&out, "  resolved     : not tested — no MDD alongside this MDX\n")
	} else {
		fmt.Fprintf(&out, "  resolved     : %d references found in the MDD\n", pronunciation.AudioResolved)
	}

	if len(report.Warnings) > 0 {
		fmt.Fprintf(&out, "\nsignals worth a look (diagnostic only, not proof of a wrong parse)\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&out, "  %-34s %s\n", warning.Code, warning.Detail)
		}
	}
	return out.String()
}

// detectedProfile names what automatic detection would have chosen, which is
// the number a forced run is being compared against.
func detectedProfile(evidence diagnose.ProfileEvidence) string {
	if evidence.Strength == diagnose.EvidenceStrong && len(evidence.Candidates) > 0 {
		return evidence.Candidates[0].ID
	}
	return diagnose.GenericProfileID
}

func joinCounts(counts []diagnose.Count, limit int) string {
	if len(counts) > limit {
		counts = counts[:limit]
	}
	parts := make([]string, 0, len(counts))
	for _, item := range counts {
		parts = append(parts, fmt.Sprintf("%s(%d)", item.Name, item.Count))
	}
	return strings.Join(parts, " ")
}

func bar(value, total int) string {
	if total == 0 {
		return "n/a"
	}
	filled := value * 20 / total
	return fmt.Sprintf("%s%s %d/%d", strings.Repeat("█", filled), strings.Repeat("·", 20-filled), value, total)
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}
