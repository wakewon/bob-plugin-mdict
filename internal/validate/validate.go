// Package validate runs the real backend over real records and reports whether
// what the parser recovered survives, intact and plausible, all the way to the
// structures a client receives.
//
// It is a development tool. Nothing in the served request path imports it, it
// is never started by the daemon, and it costs the running service nothing.
//
// The pipeline deliberately reuses the shipping implementation at every step —
// the same sampling as profile detection, the same service lookup, the same
// Bob adapter — because a validation harness that reimplements the thing it
// validates only ever proves that the copy agrees with itself.
package validate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// profileByID resolves the profile a dictionary was actually parsed with, so
// the metrics can be measured against the same scope the parser worked in.
func profileByID(id string) *parser.Profile {
	if id == "" || id == diagnose.GenericProfileID {
		return nil
	}
	return profiles.ByID(id)
}

// maxExamplesPerSense is set high on purpose: a validation run wants to see
// everything the parser produced, and Bob's own cap is a presentation decision
// that would otherwise look like content loss.
const maxExamplesPerSense = 32

// samplingForTier gives the dictionaries this project exists for more records
// to be judged on.
//
// A Chinese-English dictionary is a compatibility target; a gazetteer is a
// file that happens to be in MDX. Sampling both equally would spend the review
// budget in proportion to what the corpus contains rather than to what the
// product needs.
func samplingForTier(tier Tier) diagnose.SampleOptions {
	switch tier {
	case TierChinese:
		return diagnose.SampleOptions{Pool: 96, Keep: 16}
	case TierEnglishMono:
		return diagnose.SampleOptions{Pool: 80, Keep: 12}
	case TierEnglishOther:
		return diagnose.SampleOptions{Pool: 64, Keep: 8}
	case TierReference:
		// Enough to prove the container opens, the parser does not panic and
		// the fallback is readable. Nothing here should be forced into senses.
		return diagnose.SampleOptions{Pool: 48, Keep: 3}
	default:
		return diagnose.SampleOptions{Pool: 64, Keep: 6}
	}
}

// Snapshot is one validated record: what went in, what came out at each layer,
// and every automatic signal about it.
//
// The persisted half is metadata and measurements. The rendered half — source
// HTML, Markdown, Bob JSON — carries real dictionary text and is therefore
// never serialized: it exists only to be written into a local review file.
type Snapshot struct {
	DictionaryID    string `json:"dictionaryId"`
	DictionaryTitle string `json:"dictionaryTitle"`
	Tier            string `json:"tier"`
	// Key and MatchedKey are local-only identity. They stay inside
	// git-ignored artifacts.
	Key        string `json:"key"`
	MatchedKey string `json:"matchedKey,omitempty"`
	// RecordHash identifies the source bytes without reproducing them, so a
	// baseline can tell "the parser changed" from "the dictionary changed".
	RecordHash string `json:"recordHash"`
	EntryHash  string `json:"entryHash"`

	Parser     string `json:"parser"`
	Evidence   string `json:"evidence"`
	RawRecords int    `json:"rawRecords"`
	Records    int    `json:"records"`

	Fields   Fields      `json:"fields"`
	Metrics  Metrics     `json:"metrics"`
	Rules    []NameCount `json:"rules,omitempty"`
	Signals  []string    `json:"signals,omitempty"`
	Failures []string    `json:"failures,omitempty"`

	// Score and Reasons are filled in by the review queue.
	Score   int      `json:"score,omitempty"`
	Reasons []string `json:"reasons,omitempty"`

	checks     []Check
	sourceHTML []byte
	sourceText string
	recordText string
	markdown   string
	bobJSON    string
	irJSON     string
	profileEv  diagnose.ProfileEvidence
}

// DictionaryResult is one dictionary's validation run.
type DictionaryResult struct {
	Report    diagnose.Report `json:"report"`
	Language  Language        `json:"language"`
	Snapshots []Snapshot      `json:"snapshots"`
	// Aggregates over this dictionary's snapshots.
	MeanRetention   float64     `json:"meanRetention"`
	MeanDuplication float64     `json:"meanDuplication"`
	Rules           []NameCount `json:"rules,omitempty"`
	Signals         []NameCount `json:"signals,omitempty"`
	Failures        []NameCount `json:"failures,omitempty"`
}

// Options configures a validation run.
type Options struct {
	// QueueSize caps the human-review queue.
	QueueSize int
	// Baseline is a previous run to compare against, or nil.
	Baseline *Baseline
	// Progress is called once per dictionary.
	Progress func(index, total int, title string)
}

// Dictionary validates one dictionary end to end.
func Dictionary(svc *service.Service, dict *mdict.Dictionary) DictionaryResult {
	diagnostic := diagnose.Options{Sampling: diagnose.DiagnosticSampling}
	if dict.Info().MDDVolumes > 0 {
		diagnostic.ResolveAudio = dict.HasResource
	}
	report := diagnose.Inspect(dict, diagnostic)
	result := DictionaryResult{Report: report}
	if report.Container.Health != string(mdict.HealthOK) {
		return result
	}

	probe := diagnose.Samples(dict, diagnose.DiagnosticSampling)
	result.Language = languageOf(report.Container.Title, probe, structureEvidence{
		HasPOS:          report.Coverage.PartOfSpeech > 0,
		HasIPA:          report.Coverage.IPA > 0,
		HasSenseNumbers: anyVisibleNumbering(probe),
		MedianBytes:     report.DOM.MedianBytes,
	})

	samples := diagnose.Samples(dict, samplingForTier(result.Language.Tier))
	for _, sample := range samples {
		snapshot, ok := validateOne(svc, dict, sample, report, result.Language)
		if !ok {
			continue
		}
		result.Snapshots = append(result.Snapshots, snapshot)
	}
	summarize(&result)
	return result
}

// validateOne drives one record through the real pipeline.
func validateOne(svc *service.Service, dict *mdict.Dictionary, sample diagnose.Sample,
	report diagnose.Report, lang Language) (Snapshot, bool) {

	source, err := dict.LookupAll(sample.Key)
	if err != nil || len(source.Records) == 0 {
		return Snapshot{}, false
	}
	digest := sha256.Sum256(source.Records[0].HTML)
	snapshot := Snapshot{
		DictionaryID:    report.Container.ID,
		DictionaryTitle: report.Container.Title,
		Tier:            lang.Tier.Short(),
		Key:             sample.Key,
		MatchedKey:      source.MatchedKey,
		RecordHash:      hex.EncodeToString(digest[:])[:16],
		Parser:          report.Profile.Selected,
		Evidence:        string(report.Profile.Strength),
		RawRecords:      len(source.Records),
		profileEv:       report.Profile,
		sourceHTML:      source.Records[0].HTML,
	}
	// The denominator every retention figure is measured against: the same
	// view of a record the parser itself reads, scoped the way its profile
	// scopes it.
	snapshot.sourceText = parser.ScopedVisibleText(source.Records[0].HTML, profileByID(report.Profile.Selected))
	snapshot.recordText = parser.VisibleText(source.Records[0].HTML)

	// The real lookup: same profile resolution, same duplicate handling, same
	// cache, same everything a Bob request would get.
	result, err := svc.Lookup(sample.Key, service.LookupOptions{
		DictionaryIDs: []string{dict.ID()},
		Mode:          service.ModeExact,
		MaxExamples:   maxExamplesPerSense,
		RenderBob:     true,
		BobOptions:    bobOptions(bobadapter.MultiRecordSeparate),
	})
	if err != nil || len(result.Matches) == 0 {
		snapshot.Signals = append(snapshot.Signals, SignalNoResult)
		snapshot.Failures = append(snapshot.Failures, "service-returned-nothing")
		return snapshot, true
	}
	match := result.Matches[0]
	set := match.EntrySet()
	snapshot.Records = len(set.Records)

	separate := bobadapter.RenderEntrySet(set, bobOptions(bobadapter.MultiRecordSeparate))
	combined := bobadapter.RenderEntrySet(set, bobOptions(bobadapter.MultiRecordCombined))
	markdownOpts := mdrender.DefaultOptions()
	markdownOpts.MaxExamplesPerSense = maxExamplesPerSense
	markdownOpts.IncludeProvenance = true
	snapshot.markdown = mdrender.RenderEntrySet(set, markdownOpts)

	snapshot.irJSON = encodeJSON(set)
	snapshot.checks = checkParity(parityInput{
		source: source, set: set, separate: separate, combined: combined,
		markdown: snapshot.markdown, irJSON: snapshot.irJSON,
	})
	// The service renders Bob itself; confirming that its rendering matches the
	// one measured here is what stops this harness from validating a path the
	// product does not take.
	snapshot.checks = append(snapshot.checks, Check{
		Name:   "service-bob-matches-adapter",
		OK:     encodeJSON(result.Bob) == encodeJSON(separate),
		Detail: "the service's own Bob rendering differs from the adapter called directly",
	})
	for _, check := range snapshot.checks {
		if !check.OK {
			snapshot.Failures = append(snapshot.Failures, check.Name)
		}
	}

	rules := map[string]int{}
	for _, record := range set.Records {
		snapshot.Fields = addFields(snapshot.Fields, countFields(record.Entry))
		ruleCounts(record.Entry, rules)
	}
	snapshot.Rules = sortedCounts(rules)
	snapshot.Metrics = measure(snapshot.sourceText, snapshot.recordText, set.Primary())
	snapshot.EntryHash = hashEntrySet(set)
	snapshot.bobJSON = encodeJSON(separate)
	snapshot.Signals = append(snapshot.Signals, detectSignals(snapshot, set)...)
	return snapshot, true
}

func bobOptions(mode bobadapter.MultiRecordMode) bobadapter.Options {
	opts := bobadapter.DefaultOptions()
	opts.MaxExamplesPerSense = maxExamplesPerSense
	opts.MultiRecordMode = mode
	return opts
}

func addFields(a, b Fields) Fields {
	return Fields{
		Parts: a.Parts + b.Parts, POSLabels: a.POSLabels + b.POSLabels,
		Senses: a.Senses + b.Senses, Subsenses: a.Subsenses + b.Subsenses,
		Definitions: a.Definitions + b.Definitions, Translations: a.Translations + b.Translations,
		Examples: a.Examples + b.Examples, ExampleTranslations: a.ExampleTranslations + b.ExampleTranslations,
		Labels: a.Labels + b.Labels, IPA: a.IPA + b.IPA, Audio: a.Audio + b.Audio,
		Forms: a.Forms + b.Forms, Phrases: a.Phrases + b.Phrases,
		CrossReferences: a.CrossReferences + b.CrossReferences,
		Sections:        a.Sections + b.Sections, Fallback: a.Fallback + b.Fallback,
	}
}

// hashEntrySet fingerprints the semantic result, ignoring the parts of it that
// change between processes.
//
// Resource tokens are minted per process, so an EntrySet containing audio
// would otherwise hash differently on every run and report every entry as
// changed.
func hashEntrySet(set *entryir.EntrySet) string {
	clone := *set
	clone.Records = make([]entryir.EntryRecord, 0, len(set.Records))
	for _, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		entry := *record.Entry
		entry.Pronunciations = append([]entryir.Pronunciation(nil), entry.Pronunciations...)
		for i := range entry.Pronunciations {
			if entry.Pronunciations[i].Audio != nil {
				audio := *entry.Pronunciations[i].Audio
				audio.Token, audio.URL = "", ""
				entry.Pronunciations[i].Audio = &audio
			}
		}
		clone.Records = append(clone.Records, entryir.EntryRecord{RecordOrdinal: record.RecordOrdinal, Entry: &entry})
	}
	digest := sha256.Sum256([]byte(encodeJSON(clone)))
	return hex.EncodeToString(digest[:])[:16]
}

// encodeJSON serializes without HTML escaping.
//
// Go's encoder turns "&" into "\u0026" by default, which is right for a script
// tag and wrong for everything here: it makes the snapshots harder to read and
// it breaks the token comparison that proves the Markdown came from the IR.
func encodeJSON(value any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return ""
	}
	return strings.TrimRight(buffer.String(), "\n")
}

// numberingRe finds a visible enumeration marker without parsing.
var numberingRe = regexp.MustCompile(`(?:^|[\s>])[(（]?\d{1,2}[.)）、]|[①-⑳❶-❿]`)

func anyVisibleNumbering(samples []diagnose.Sample) bool {
	for _, sample := range samples {
		if numberingRe.Match(sample.HTML) {
			return true
		}
	}
	return false
}

func summarize(result *DictionaryResult) {
	if len(result.Snapshots) == 0 {
		return
	}
	rules := map[string]int{}
	signals := map[string]int{}
	failures := map[string]int{}
	retention, duplication := 0.0, 0.0
	for _, snapshot := range result.Snapshots {
		retention += snapshot.Metrics.Retention
		duplication += snapshot.Metrics.Duplication
		for _, rule := range snapshot.Rules {
			rules[rule.Name] += rule.Count
		}
		for _, signal := range snapshot.Signals {
			signals[signal]++
		}
		for _, failure := range snapshot.Failures {
			failures[failure]++
		}
	}
	count := float64(len(result.Snapshots))
	result.MeanRetention = retention / count
	result.MeanDuplication = duplication / count
	result.Rules = sortedCounts(rules)
	result.Signals = sortedCounts(signals)
	result.Failures = sortedCounts(failures)
}

func sortSnapshots(snapshots []Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].Score != snapshots[j].Score {
			return snapshots[i].Score > snapshots[j].Score
		}
		if snapshots[i].DictionaryID != snapshots[j].DictionaryID {
			return snapshots[i].DictionaryID < snapshots[j].DictionaryID
		}
		return snapshots[i].Key < snapshots[j].Key
	})
}

func joinNames(counts []NameCount, limit int) string {
	if len(counts) > limit {
		counts = counts[:limit]
	}
	parts := make([]string, 0, len(counts))
	for _, item := range counts {
		parts = append(parts, item.Name)
	}
	return strings.Join(parts, ", ")
}
