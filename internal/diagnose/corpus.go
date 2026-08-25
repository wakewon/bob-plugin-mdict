package diagnose

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
)

// CorpusReport is the batch result over a whole dictionary directory.
type CorpusReport struct {
	Root         string       `json:"root"`
	Total        int          `json:"total"`
	Healthy      int          `json:"healthy"`
	Unavailable  int          `json:"unavailable"`
	Dictionaries []Report     `json:"dictionaries"`
	Profiles     []Count      `json:"profileDistribution"`
	Families     []Family     `json:"families,omitempty"`
	Aggregate    CorpusTotals `json:"aggregate"`
}

// CorpusTotals are the numbers a parser change is judged by.
type CorpusTotals struct {
	Sampled int `json:"sampled"`
	// WithDefinitions and friends count dictionaries where at least one sample
	// produced that kind of structure.
	WithDefinitions  int `json:"withDefinitions"`
	WithPOS          int `json:"withPartOfSpeech"`
	WithExamples     int `json:"withExamples"`
	WithTranslations int `json:"withTranslations"`
	WithIPA          int `json:"withIPA"`
	WithPhrases      int `json:"withPhrases"`
	WithCrossRefs    int `json:"withCrossReferences"`
	// StrongStructure counts dictionaries where most samples produced senses.
	StrongStructure int `json:"strongStructure"`
	// HeavyFallback counts dictionaries where most samples produced none.
	HeavyFallback int `json:"heavyFallback"`
	// FullyUnparsed counts dictionaries where every sample fell back.
	FullyUnparsed int `json:"fullyUnparsed"`
	Flagged       int `json:"flagged"`
	// MeanStructuredRate and MeanFallbackRate average the per-dictionary rates,
	// so a 500 000-headword dictionary and a 400-headword one count the same.
	MeanStructuredRate float64 `json:"meanStructuredRate"`
	MeanFallbackRate   float64 `json:"meanFallbackRate"`
	// Warnings counts each conservative signal across the corpus.
	Warnings []Count `json:"warnings,omitempty"`
}

// Family is a group of dictionaries built from the same HTML template.
type Family struct {
	Key string `json:"key"`
	// Members are dictionary IDs.
	Members []string `json:"members"`
	Titles  []string `json:"titles"`
	// SharedClasses is the class vocabulary they have in common, which is the
	// evidence for calling them one family.
	SharedClasses []string `json:"sharedClasses"`
	// Profiles is what those members currently parse with.
	Profiles []string `json:"profiles"`
}

// Corpus runs the diagnostics over every dictionary in a registry.
func Corpus(registry *mdict.Registry, opts Options, progress func(index, total int, title string)) CorpusReport {
	dicts := registry.All()
	report := CorpusReport{Root: registry.Root(), Total: len(dicts)}
	profileCounts := map[string]int{}
	warningCounts := map[string]int{}

	var structuredSum, fallbackSum float64
	for i, dict := range dicts {
		if progress != nil {
			progress(i+1, len(dicts), dict.Info().Title)
		}
		item := Inspect(dict, opts)
		report.Dictionaries = append(report.Dictionaries, item)

		if item.Container.Health == string(mdict.HealthOK) {
			report.Healthy++
		} else {
			report.Unavailable++
			continue
		}
		profileCounts[item.Profile.Selected]++
		for _, warning := range item.Warnings {
			warningCounts[warning.Code]++
		}
		if len(item.Warnings) > 0 {
			report.Aggregate.Flagged++
		}
		if item.Coverage.Samples == 0 {
			continue
		}
		report.Aggregate.Sampled++
		structuredSum += item.Coverage.StructuredRate
		fallbackSum += item.Coverage.FallbackRate
		countIf(&report.Aggregate.WithDefinitions, item.Coverage.Definitions > 0)
		countIf(&report.Aggregate.WithPOS, item.Coverage.PartOfSpeech > 0)
		countIf(&report.Aggregate.WithExamples, item.Coverage.Examples > 0)
		countIf(&report.Aggregate.WithTranslations, item.Coverage.Translations > 0)
		countIf(&report.Aggregate.WithIPA, item.Coverage.IPA > 0)
		countIf(&report.Aggregate.WithPhrases, item.Coverage.Phrases > 0)
		countIf(&report.Aggregate.WithCrossRefs, item.Coverage.CrossRefs > 0)
		countIf(&report.Aggregate.StrongStructure, item.Coverage.StructuredRate >= 0.8)
		countIf(&report.Aggregate.HeavyFallback, item.Coverage.FallbackRate >= 0.5)
		countIf(&report.Aggregate.FullyUnparsed, item.Coverage.FallbackRate == 1)
	}

	if report.Aggregate.Sampled > 0 {
		report.Aggregate.MeanStructuredRate = structuredSum / float64(report.Aggregate.Sampled)
		report.Aggregate.MeanFallbackRate = fallbackSum / float64(report.Aggregate.Sampled)
	}
	report.Profiles = topCounts(profileCounts, 32)
	report.Aggregate.Warnings = topCounts(warningCounts, 32)
	report.Families = detectFamilies(report.Dictionaries)
	return report
}

// familyOverlap is how much class vocabulary two dictionaries must share
// before they are called one template family.
//
// It is set high on purpose. Two dictionaries that both use ".def" and
// ".sense" have not been built from the same template; two that share ten of
// twelve class names have.
const familyOverlap = 0.7

// detectFamilies groups dictionaries whose class vocabularies substantially
// coincide, by single-link clustering.
func detectFamilies(reports []Report) []Family {
	type node struct {
		report Report
		vocab  map[string]struct{}
	}
	var nodes []node
	for _, item := range reports {
		if len(item.DOM.ClassVocabulary) < 6 {
			// Too small a vocabulary to distinguish a template from a habit.
			continue
		}
		set := make(map[string]struct{}, len(item.DOM.ClassVocabulary))
		for _, class := range item.DOM.ClassVocabulary {
			set[class] = struct{}{}
		}
		nodes = append(nodes, node{report: item, vocab: set})
	}

	parent := make([]int, len(nodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range nodes {
		for j := i + 1; j < len(nodes); j++ {
			if jaccard(nodes[i].vocab, nodes[j].vocab) >= familyOverlap {
				parent[find(i)] = find(j)
			}
		}
	}

	groups := map[int][]int{}
	for i := range nodes {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	var families []Family
	for root, members := range groups {
		if len(members) < 2 {
			continue
		}
		shared := map[string]struct{}{}
		for class := range nodes[members[0]].vocab {
			shared[class] = struct{}{}
		}
		family := Family{Key: nodes[root].report.DOM.FamilyKey}
		for _, index := range members {
			item := nodes[index].report
			family.Members = append(family.Members, item.Container.ID)
			family.Titles = append(family.Titles, item.Container.Title)
			family.Profiles = append(family.Profiles, item.Profile.Selected)
			for class := range shared {
				if _, ok := nodes[index].vocab[class]; !ok {
					delete(shared, class)
				}
			}
		}
		for class := range shared {
			family.SharedClasses = append(family.SharedClasses, class)
		}
		sort.Strings(family.SharedClasses)
		sort.Strings(family.Titles)
		sort.Strings(family.Members)
		dedupe(&family.Profiles)
		families = append(families, family)
	}
	sort.Slice(families, func(i, j int) bool {
		if len(families[i].Members) != len(families[j].Members) {
			return len(families[i].Members) > len(families[j].Members)
		}
		return strings.Join(families[i].Members, ",") < strings.Join(families[j].Members, ",")
	})
	return families
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for key := range a {
		if _, ok := b[key]; ok {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func dedupe(values *[]string) {
	seen := make(map[string]struct{}, len(*values))
	out := (*values)[:0]
	for _, value := range *values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	*values = out
}

// Markdown renders the corpus report for a human reader.
//
// It contains titles, IDs, counts, percentages, class names and warning codes,
// and no dictionary content whatsoever, so the output is safe to keep and to
// quote from.
func (c CorpusReport) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "# MDX corpus diagnostics\n\n")
	fmt.Fprintf(&out, "- dictionaries discovered: **%d** (healthy %d, unavailable %d)\n",
		c.Total, c.Healthy, c.Unavailable)
	fmt.Fprintf(&out, "- sampled: %d\n", c.Aggregate.Sampled)
	fmt.Fprintf(&out, "- mean structural coverage: **%.1f%%**, mean fallback rate: **%.1f%%**\n",
		100*c.Aggregate.MeanStructuredRate, 100*c.Aggregate.MeanFallbackRate)
	fmt.Fprintf(&out, "- strong structure (≥80%% of samples): %d · heavy fallback (≥50%%): %d · fully unparsed: %d\n",
		c.Aggregate.StrongStructure, c.Aggregate.HeavyFallback, c.Aggregate.FullyUnparsed)
	fmt.Fprintf(&out, "- flagged for manual inspection: %d\n\n", c.Aggregate.Flagged)

	out.WriteString("## Parser selection\n\n")
	for _, item := range c.Profiles {
		fmt.Fprintf(&out, "- `%s`: %d\n", item.Name, item.Count)
	}

	out.WriteString("\n## Structure recovered, by kind\n\n")
	fmt.Fprintf(&out, "| kind | dictionaries |\n|---|---|\n")
	for _, row := range []struct {
		name  string
		value int
	}{
		{"definitions", c.Aggregate.WithDefinitions},
		{"part of speech", c.Aggregate.WithPOS},
		{"examples", c.Aggregate.WithExamples},
		{"translations", c.Aggregate.WithTranslations},
		{"IPA", c.Aggregate.WithIPA},
		{"phrases/idioms", c.Aggregate.WithPhrases},
		{"cross-references", c.Aggregate.WithCrossRefs},
	} {
		fmt.Fprintf(&out, "| %s | %d / %d |\n", row.name, row.value, c.Aggregate.Sampled)
	}

	if len(c.Aggregate.Warnings) > 0 {
		out.WriteString("\n## Warning signals\n\n")
		for _, item := range c.Aggregate.Warnings {
			fmt.Fprintf(&out, "- `%s`: %d\n", item.Name, item.Count)
		}
	}

	if len(c.Families) > 0 {
		out.WriteString("\n## Shared template families\n\n")
		for _, family := range c.Families {
			fmt.Fprintf(&out, "### %s (%d dictionaries, parsers: %s)\n\n",
				family.Key, len(family.Members), strings.Join(family.Profiles, ", "))
			for _, title := range family.Titles {
				fmt.Fprintf(&out, "- %s\n", title)
			}
			fmt.Fprintf(&out, "\nshared classes: `%s`\n\n", strings.Join(family.SharedClasses, "`, `"))
		}
	}

	out.WriteString("\n## Per dictionary\n\n")
	out.WriteString("| dictionary | id | entries | parser | evidence | struct | fallback | senses | defs | pos | ex | tr | ipa | audio-ref | mdd | warnings |\n")
	out.WriteString("|---|---|---:|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	rows := append([]Report(nil), c.Dictionaries...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Coverage.StructuredRate != rows[j].Coverage.StructuredRate {
			return rows[i].Coverage.StructuredRate < rows[j].Coverage.StructuredRate
		}
		return rows[i].Container.Title < rows[j].Container.Title
	})
	for _, item := range rows {
		codes := make([]string, 0, len(item.Warnings))
		for _, warning := range item.Warnings {
			codes = append(codes, warning.Code)
		}
		health := ""
		if item.Container.Health != string(mdict.HealthOK) {
			health = " ⚠️unavailable"
		}
		fmt.Fprintf(&out, "| %s%s | `%s` | %d | `%s` | %s | %.0f%% | %.0f%% | %d | %d | %d | %d | %d | %d | %d | %d | %s |\n",
			escapePipes(item.Container.Title), health, item.Container.ID, item.Container.EntryCount,
			item.Profile.Selected, item.Profile.Strength,
			100*item.Coverage.StructuredRate, 100*item.Coverage.FallbackRate,
			item.Coverage.MedianSenses, item.Coverage.Definitions, item.Coverage.PartOfSpeech,
			item.Coverage.Examples, item.Coverage.Translations, item.Coverage.IPA,
			item.Pronunciation.AudioRefSamples, item.Container.MDDVolumes,
			strings.Join(codes, " "))
	}
	return out.String()
}

func escapePipes(value string) string { return strings.ReplaceAll(value, "|", "\\|") }
