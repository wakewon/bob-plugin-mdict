package validate

import (
	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
)

// Run is one complete validation pass over one or many dictionaries.
type Run struct {
	Root        string `json:"root"`
	GeneratedAt string `json:"generatedAt"`
	Total       int    `json:"total"`
	Healthy     int    `json:"healthy"`
	Unavailable int    `json:"unavailable"`

	Dictionaries []DictionaryResult `json:"dictionaries"`
	Families     []diagnose.Family  `json:"families,omitempty"`

	Tiers    []NameCount `json:"tiers,omitempty"`
	Parsers  []NameCount `json:"parsers,omitempty"`
	Rules    []NameCount `json:"rules,omitempty"`
	Signals  []NameCount `json:"signals,omitempty"`
	Failures []NameCount `json:"failures,omitempty"`

	Aggregate  RunTotals   `json:"aggregate"`
	Queue      []Snapshot  `json:"queue,omitempty"`
	Comparison *Comparison `json:"comparison,omitempty"`
}

// RunTotals are the corpus-level numbers a parser change is judged by.
//
// They are reported per tier as well as overall, because an average taken over
// a corpus that is a quarter encyclopedias measures the corpus, not the parser.
type RunTotals struct {
	Records         int          `json:"records"`
	MeanRetention   float64      `json:"meanRetention"`
	MeanDuplication float64      `json:"meanDuplication"`
	ByTier          []TierTotals `json:"byTier,omitempty"`
}

// TierTotals are one priority tier's numbers.
type TierTotals struct {
	Tier            string  `json:"tier"`
	Dictionaries    int     `json:"dictionaries"`
	Records         int     `json:"records"`
	Structured      int     `json:"structured"`
	Fallback        int     `json:"fallback"`
	MeanRetention   float64 `json:"meanRetention"`
	MeanDuplication float64 `json:"meanDuplication"`
	Flagged         int     `json:"flagged"`
}

// Corpus validates every dictionary in the service's registry.
func Corpus(svc *service.Service, opts Options) *Run {
	dicts := svc.Registry().All()
	run := &Run{
		Root:        svc.Registry().Root(),
		GeneratedAt: timestamp(),
		Total:       len(dicts),
	}
	for i, dict := range dicts {
		if opts.Progress != nil {
			opts.Progress(i+1, len(dicts), dict.Info().Title)
		}
		result := Dictionary(svc, dict)
		run.Dictionaries = append(run.Dictionaries, result)
	}
	finish(run, opts)
	return run
}

// One validates a single dictionary, producing the same shape of run so that
// the single and corpus paths share every downstream step.
func One(svc *service.Service, dict *mdict.Dictionary, opts Options) *Run {
	run := &Run{
		Root:         svc.Registry().Root(),
		GeneratedAt:  timestamp(),
		Total:        1,
		Dictionaries: []DictionaryResult{Dictionary(svc, dict)},
	}
	finish(run, opts)
	return run
}

func finish(run *Run, opts Options) {
	reports := make([]diagnose.Report, 0, len(run.Dictionaries))
	parsers := map[string]int{}
	rules := map[string]int{}
	signals := map[string]int{}
	failures := map[string]int{}
	byTier := map[Tier]*TierTotals{}

	for _, dictionary := range run.Dictionaries {
		reports = append(reports, dictionary.Report)
		if dictionary.Report.Container.Health != string(mdict.HealthOK) {
			run.Unavailable++
			continue
		}
		run.Healthy++
		parsers[dictionary.Report.Profile.Selected]++

		tier := dictionary.Language.Tier
		totals := byTier[tier]
		if totals == nil {
			totals = &TierTotals{Tier: tier.Label()}
			byTier[tier] = totals
		}
		totals.Dictionaries++

		for _, snapshot := range dictionary.Snapshots {
			run.Aggregate.Records++
			totals.Records++
			totals.MeanRetention += snapshot.Metrics.Retention
			totals.MeanDuplication += snapshot.Metrics.Duplication
			run.Aggregate.MeanRetention += snapshot.Metrics.Retention
			run.Aggregate.MeanDuplication += snapshot.Metrics.Duplication
			if snapshot.Fields.Senses > 0 {
				totals.Structured++
			}
			if snapshot.Fields.Fallback > 0 && snapshot.Fields.Senses == 0 {
				totals.Fallback++
			}
			if len(snapshot.Signals) > 0 {
				totals.Flagged++
			}
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
	}

	if run.Aggregate.Records > 0 {
		run.Aggregate.MeanRetention /= float64(run.Aggregate.Records)
		run.Aggregate.MeanDuplication /= float64(run.Aggregate.Records)
	}
	for tier := TierChinese; tier <= TierReference; tier++ {
		totals := byTier[tier]
		if totals == nil {
			continue
		}
		if totals.Records > 0 {
			totals.MeanRetention /= float64(totals.Records)
			totals.MeanDuplication /= float64(totals.Records)
		}
		run.Aggregate.ByTier = append(run.Aggregate.ByTier, *totals)
	}

	run.Families = diagnose.Families(reports)
	run.Tiers = tierCounts(run)
	run.Parsers = sortedCounts(parsers)
	run.Rules = sortedCounts(rules)
	run.Signals = sortedCounts(signals)
	run.Failures = sortedCounts(failures)
	run.Comparison = compare(opts.Baseline, run)

	inFamily := map[string]bool{}
	for _, family := range run.Families {
		for _, member := range family.Members {
			inFamily[member] = true
		}
	}
	previous := map[string]Snapshot{}
	if opts.Baseline != nil {
		for _, snapshot := range opts.Baseline.Snapshots {
			previous[snapshot.identity()] = snapshot
		}
	}
	for d := range run.Dictionaries {
		dictionary := &run.Dictionaries[d]
		for s := range dictionary.Snapshots {
			snapshot := &dictionary.Snapshots[s]
			before, had := previous[snapshot.identity()]
			scoreSnapshot(snapshot, dictionary.Language.Tier, run.Comparison, before, had,
				inFamily[snapshot.DictionaryID])
		}
	}

	size := opts.QueueSize
	if size <= 0 {
		size = DefaultQueueSize
	}
	run.Queue = buildQueue(run, size)
}

// DefaultQueueSize is chosen from what a person can actually read. A thousand
// sampled records is data; a hundred ranked ones is a review.
const DefaultQueueSize = 100
