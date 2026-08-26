package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/validate"
)

// The validation commands answer the question the diagnostics cannot: not
// "did the parser produce structure?" but "does that structure survive the
// backend, and is it a plausible reading of the record?".
//
// They run the real service, the real Bob adapter and the experimental
// Markdown renderer over records the dictionaries really contain, then rank
// what a person should actually read. Everything they write contains real
// dictionary text and belongs in a git-ignored directory.

type validateFlags struct {
	selector string
	all      bool
	out      string
	baseline string
	queue    int
}

func runValidate(svc *service.Service, flags validateFlags) error {
	started := time.Now()
	loadDictionaries(svc)

	opts := validate.Options{QueueSize: flags.queue}
	if flags.baseline != "" {
		baseline, err := validate.LoadBaseline(flags.baseline)
		if err != nil {
			return fmt.Errorf("read baseline: %w", err)
		}
		if baseline == nil {
			fmt.Fprintf(os.Stderr, "no baseline at %s; reporting absolute results only\n", flags.baseline)
		}
		opts.Baseline = baseline
	}

	var run *validate.Run
	if flags.all {
		total, _ := svc.Registry().Counts()
		fmt.Fprintf(os.Stderr, "validating %d dictionaries in %s\n", total, svc.Config().DictionaryDir)
		opts.Progress = func(index, count int, title string) {
			fmt.Fprintf(os.Stderr, "\r[%3d/%3d] %-50.50s", index, count, title)
		}
		run = validate.Corpus(svc, opts)
		fmt.Fprintf(os.Stderr, "\r%-64s\n", "")
	} else {
		dict, err := findDictionary(svc, flags.selector)
		if err != nil {
			return err
		}
		run = validate.One(svc, dict, opts)
	}
	fmt.Fprintf(os.Stderr, "validated %d records in %s\n",
		run.Aggregate.Records, time.Since(started).Round(time.Millisecond))

	fmt.Print(summarizeRun(run))
	if flags.out == "" {
		fmt.Fprintln(os.Stderr, "pass --validate-out DIR to write the Markdown review set and a comparison baseline")
		return nil
	}
	if err := validate.Write(flags.out, run); err != nil {
		return err
	}
	baselinePath := filepath.Join(flags.out, "baseline.json")
	if err := validate.SaveBaseline(baselinePath, run); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (index, %d snapshots, %d dictionary pages) and %s\n",
		filepath.Join(flags.out, "README.md"), len(run.Queue), len(run.Dictionaries), baselinePath)
	return nil
}

// summarizeRun prints the numbers worth seeing without opening a file.
func summarizeRun(run *validate.Run) string {
	var out strings.Builder
	fmt.Fprintf(&out, "dictionaries : %d (healthy %d, unavailable %d)\n", run.Total, run.Healthy, run.Unavailable)
	fmt.Fprintf(&out, "records      : %d\n", run.Aggregate.Records)
	fmt.Fprintf(&out, "retention    : %.0f%% mean   duplication: %.1f%% mean\n",
		100*run.Aggregate.MeanRetention, 100*run.Aggregate.MeanDuplication)

	fmt.Fprintf(&out, "\nby product priority\n")
	fmt.Fprintf(&out, "  %-28s %5s %7s %10s %9s %11s %7s\n",
		"tier", "dicts", "records", "structured", "fallback", "retention", "flagged")
	for _, tier := range run.Aggregate.ByTier {
		fmt.Fprintf(&out, "  %-28s %5d %7d %10d %9d %10.0f%% %7d\n",
			tier.Tier, tier.Dictionaries, tier.Records, tier.Structured, tier.Fallback,
			100*tier.MeanRetention, tier.Flagged)
	}

	fmt.Fprintf(&out, "\nparsers      : ")
	for _, item := range run.Parsers {
		fmt.Fprintf(&out, "%s=%d ", item.Name, item.Count)
	}
	fmt.Fprintf(&out, "\nrules        :\n")
	for _, item := range run.Rules {
		fmt.Fprintf(&out, "  %-26s %d\n", item.Name, item.Count)
	}
	if len(run.Signals) > 0 {
		fmt.Fprintf(&out, "signals      :\n")
		for _, item := range run.Signals {
			fmt.Fprintf(&out, "  %-30s %d\n", item.Name, item.Count)
		}
	}
	if len(run.Failures) > 0 {
		fmt.Fprintf(&out, "parity failures:\n")
		for _, item := range run.Failures {
			fmt.Fprintf(&out, "  %-38s %d\n", item.Name, item.Count)
		}
	} else {
		fmt.Fprintf(&out, "parity       : every invariant held\n")
	}
	if run.Comparison != nil {
		fmt.Fprintf(&out, "vs baseline %s:\n", run.Comparison.BaselineAt)
		for _, item := range run.Comparison.Counts {
			fmt.Fprintf(&out, "  %-22s %d\n", item.Name, item.Count)
		}
	}
	fmt.Fprintf(&out, "review queue : %d records\n", len(run.Queue))
	return out.String()
}
