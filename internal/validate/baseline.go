package validate

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

// A validation run is only half useful on its own. The other half is the
// previous run: a parser change is judged by what it did to records that were
// already being read correctly, not by the absolute numbers it produces.
//
// The baseline is therefore the corpus turned into a regression suite. It
// holds measurements and hashes — never dictionary text — and it lives beside
// the corpus in a git-ignored directory, because the corpus it describes is
// the developer's own library.

// Baseline is a previous run, reduced to what a comparison needs.
type Baseline struct {
	GeneratedAt string     `json:"generatedAt"`
	Snapshots   []Snapshot `json:"snapshots"`
}

// Change classifies one record between two runs.
type Change string

const (
	ChangeUnchanged Change = "unchanged"
	// ChangeSourceChanged means the record itself is different, so nothing can
	// be concluded about the parser from it.
	ChangeSourceChanged Change = "source-changed"
	ChangeImprovement   Change = "likely-improvement"
	ChangeNeedsReview   Change = "changed"
	ChangeRegression    Change = "possible-regression"
	ChangeNew           Change = "new"
	ChangeMissing       Change = "missing"
)

// Comparison is the full baseline-versus-current result.
type Comparison struct {
	BaselineAt string      `json:"baselineAt"`
	Counts     []NameCount `json:"counts"`
	// Missing lists records the baseline had that this run did not produce.
	Missing []string `json:"missing,omitempty"`
	byKey   map[string]Change
	reasons map[string]string
}

// Verdict returns the classification for one snapshot.
func (c *Comparison) Verdict(snapshot Snapshot) (Change, string) {
	if c == nil {
		return "", ""
	}
	key := snapshot.identity()
	return c.byKey[key], c.reasons[key]
}

func (s Snapshot) identity() string { return s.DictionaryID + "\x00" + s.Key }

// LoadBaseline reads a saved run. A missing file is not an error: the first
// run of the pipeline has nothing to compare against and should say so rather
// than fail.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var baseline Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, err
	}
	return &baseline, nil
}

// SaveBaseline writes this run's snapshots for the next one to compare with.
func SaveBaseline(path string, run *Run) error {
	baseline := Baseline{GeneratedAt: run.GeneratedAt}
	for _, dictionary := range run.Dictionaries {
		baseline.Snapshots = append(baseline.Snapshots, dictionary.Snapshots...)
	}
	sort.Slice(baseline.Snapshots, func(i, j int) bool {
		return baseline.Snapshots[i].identity() < baseline.Snapshots[j].identity()
	})
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Thresholds for calling a difference a regression. They are wide enough that
// ordinary re-tokenization noise does not trip them.
const (
	retentionDrop   = 0.15
	duplicationRise = 0.15
	dominanceRise   = 0.2
)

// compare classifies every current record against the baseline.
//
// The rule that matters most here is the one it does not implement: more
// extracted fields is not treated as an improvement. A parse can produce twice
// as many senses by splitting one meaning in half, and a correct fallback is
// better than a confidently wrong structure. Only a record that went from
// nothing to something, without losing content or gaining duplication, is
// called an improvement — and even that is a hypothesis for a human to check.
func compare(baseline *Baseline, run *Run) *Comparison {
	if baseline == nil {
		return nil
	}
	previous := make(map[string]Snapshot, len(baseline.Snapshots))
	for _, snapshot := range baseline.Snapshots {
		previous[snapshot.identity()] = snapshot
	}

	comparison := &Comparison{
		BaselineAt: baseline.GeneratedAt,
		byKey:      make(map[string]Change),
		reasons:    make(map[string]string),
	}
	counts := map[string]int{}
	seen := make(map[string]struct{}, len(previous))

	for _, dictionary := range run.Dictionaries {
		for _, snapshot := range dictionary.Snapshots {
			key := snapshot.identity()
			seen[key] = struct{}{}
			before, ok := previous[key]
			change, reason := classify(before, snapshot, ok)
			comparison.byKey[key] = change
			comparison.reasons[key] = reason
			counts[string(change)]++
		}
	}
	for key, snapshot := range previous {
		if _, ok := seen[key]; ok {
			continue
		}
		counts[string(ChangeMissing)]++
		comparison.Missing = append(comparison.Missing, snapshot.DictionaryTitle+" · "+snapshot.Key)
	}
	sort.Strings(comparison.Missing)
	comparison.Counts = sortedCounts(counts)
	return comparison
}

func classify(before, after Snapshot, existed bool) (Change, string) {
	if !existed {
		return ChangeNew, "not present in the baseline"
	}
	if before.RecordHash != after.RecordHash {
		return ChangeSourceChanged, "the source record itself differs"
	}
	if before.EntryHash == after.EntryHash {
		return ChangeUnchanged, ""
	}

	structuredBefore := before.Fields.Senses > 0
	structuredAfter := after.Fields.Senses > 0
	retentionFell := after.Metrics.Retention < before.Metrics.Retention-retentionDrop
	duplicationRose := after.Metrics.Duplication > before.Metrics.Duplication+duplicationRise
	dominanceGrew := after.Metrics.LargestFieldShare > before.Metrics.LargestFieldShare+dominanceRise
	newFailures := len(after.Failures) > len(before.Failures)

	// A record that now accounts for much more of its source has not regressed
	// because one of its fields got bigger; that is what accounting for more
	// of the source looks like. Retention is checked first for the same
	// reason: it is the measure closest to "did anything go missing".
	retentionRose := after.Metrics.Retention > before.Metrics.Retention+retentionDrop

	switch {
	case structuredBefore && !structuredAfter && !retentionRose:
		return ChangeRegression, "senses were recovered before and are not now"
	case retentionFell:
		return ChangeRegression, "content retention fell materially"
	case newFailures:
		return ChangeRegression, "new backend parity failures"
	case duplicationRose && !retentionRose:
		return ChangeRegression, "duplicated output rose materially"
	case dominanceGrew && !retentionRose:
		return ChangeRegression, "one field grew to dominate the record"
	case structuredBefore && !structuredAfter:
		return ChangeImprovement, "structure gave way to a fallback that keeps far more of the record"
	case !structuredBefore && structuredAfter && !retentionFell:
		return ChangeImprovement, "structure recovered where there was none, with no loss of retention"
	case retentionRose:
		return ChangeImprovement, "the parse now accounts for materially more of the record"
	}
	return ChangeNeedsReview, "the semantic result changed"
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339) }
