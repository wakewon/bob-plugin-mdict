package validate

import "sort"

// A corpus run produces roughly a thousand validated records. Reading all of
// them is not review, it is data entry.
//
// The queue is what turns that into a morning's work: score every record by
// how much a human looking at it would learn, then take the top hundred. The
// score is dominated by product priority — a Chinese-English dictionary is the
// thing this project is for — and after that by evidence that something is
// wrong, and after that by novelty.

// tierWeight is the product-priority component of the score.
var tierWeight = map[Tier]int{
	TierChinese:      100,
	TierEnglishMono:  60,
	TierEnglishOther: 35,
	TierOtherLexical: 20,
	// A gazetteer falling back cleanly is the correct outcome, not a finding.
	TierReference: 5,
}

// signalWeight rates each conservative signal by how often it turns out to be
// a real defect rather than a quirk.
var signalWeight = map[string]int{
	SignalParityFailure:     70,
	SignalNoResult:          50,
	SignalLowRetention:      45,
	SignalBilingualNoGloss:  40,
	SignalHighDuplication:   40,
	SignalDominantField:     35,
	SignalSubsenseEcho:      30,
	SignalExamplesNoDefs:    30,
	SignalSingleHugeExample: 30,
	SignalRepeatedContent:   25,
	SignalHighSenseCount:    25,
	SignalHeadwordMismatch:  25,
	SignalTranslationOnly:   20,
	// A fallback is very often the right answer, so it barely counts.
	SignalFallback: 5,
}

// changeWeight rates what happened to a record since the baseline.
var changeWeight = map[Change]int{
	ChangeRegression:    120,
	ChangeImprovement:   45,
	ChangeNeedsReview:   30,
	ChangeSourceChanged: 5,
	ChangeNew:           15,
}

// newGenericRules are the heuristics introduced in the previous round. They
// are exactly the ones with no track record, so a record that depends on one
// is worth more review attention than a record parsed by a profile.
var newGenericRules = map[string]bool{
	"generic:markerBlocks":   true,
	"generic:markerRun":      true,
	"generic:orderedList":    true,
	"generic:definitionList": true,
	"generic:definitionRun":  true,
}

// Queue limits. One pathological dictionary must not fill the queue, and the
// dictionaries this project exists for must not be crowded out of it.
const (
	maxPerDictionary  = 3
	chineseShareFloor = 2 // at least one in two queue slots, where available
	referenceShareCap = 20
)

// scoreSnapshot rates one record and records why.
func scoreSnapshot(snapshot *Snapshot, tier Tier, comparison *Comparison, before Snapshot, hadBefore, inFamily bool) {
	score := tierWeight[tier]
	reasons := []string{tier.Label()}

	if comparison != nil {
		if change, reason := comparison.Verdict(*snapshot); change != "" {
			score += changeWeight[change]
			if weight := changeWeight[change]; weight > 0 {
				label := string(change)
				if reason != "" {
					label += " (" + reason + ")"
				}
				reasons = append(reasons, label)
			}
		}
	}
	if hadBefore {
		// New semantic kinds are worth checking precisely because they are new:
		// a translation that did not exist before is either a real recovery or
		// a new way of being wrong, and only reading it distinguishes them.
		if before.Fields.Translations == 0 && snapshot.Fields.Translations > 0 {
			score += 40
			reasons = append(reasons, "newly extracts translations")
		}
		if before.Fields.Examples == 0 && snapshot.Fields.Examples > 0 {
			score += 25
			reasons = append(reasons, "newly extracts examples")
		}
		if before.Fields.Fallback > 0 && snapshot.Fields.Fallback == 0 {
			score += 30
			reasons = append(reasons, "no longer falls back")
		}
	}

	for _, signal := range snapshot.Signals {
		if weight, ok := signalWeight[signal]; ok {
			score += weight
			reasons = append(reasons, signal)
		}
	}
	for _, rule := range snapshot.Rules {
		if newGenericRules[rule.Name] {
			score += 25
			reasons = append(reasons, "relies on "+rule.Name)
			break
		}
	}
	if snapshot.Parser == "generic" && snapshot.Fields.Translations > 0 {
		score += 20
		reasons = append(reasons, "generic bilingual extraction")
	}
	if inFamily {
		score += 8
		reasons = append(reasons, "represents a template family")
	}
	snapshot.Score = score
	snapshot.Reasons = reasons
}

// buildQueue ranks and then selects, because ranking alone produces a queue of
// a hundred records from four dictionaries.
func buildQueue(run *Run, size int) []Snapshot {
	var all []Snapshot
	tierOf := map[string]Tier{}
	for _, dictionary := range run.Dictionaries {
		tierOf[dictionary.Report.Container.ID] = dictionary.Language.Tier
		all = append(all, dictionary.Snapshots...)
	}
	sortSnapshots(all)

	perDictionary := map[string]int{}
	tierCount := map[Tier]int{}
	var queue []Snapshot

	admit := func(snapshot Snapshot, dictionaryCap int) bool {
		tier := tierOf[snapshot.DictionaryID]
		if perDictionary[snapshot.DictionaryID] >= dictionaryCap {
			return false
		}
		if tier == TierReference && tierCount[TierReference] >= size/referenceShareCap+1 {
			return false
		}
		perDictionary[snapshot.DictionaryID]++
		tierCount[tier]++
		queue = append(queue, snapshot)
		return true
	}

	for _, snapshot := range all {
		if len(queue) >= size {
			break
		}
		admit(snapshot, maxPerDictionary)
	}

	// Chinese-related dictionaries are the point of the exercise. If ordinary
	// scoring did not give them half the queue, take the slots back from the
	// bottom rather than hoping the weights were right.
	available := 0
	for _, snapshot := range all {
		if tierOf[snapshot.DictionaryID] == TierChinese {
			available++
		}
	}
	floor := size / chineseShareFloor
	if available < floor {
		floor = available
	}
	if tierCount[TierChinese] < floor {
		chosen := make(map[string]struct{}, len(queue))
		for _, snapshot := range queue {
			chosen[snapshot.identity()] = struct{}{}
		}
		for _, snapshot := range all {
			if tierCount[TierChinese] >= floor {
				break
			}
			if tierOf[snapshot.DictionaryID] != TierChinese {
				continue
			}
			if _, already := chosen[snapshot.identity()]; already {
				continue
			}
			if len(queue) >= size {
				dropLowestNonChinese(&queue, tierOf, tierCount)
			}
			if admit(snapshot, maxPerDictionary+2) {
				chosen[snapshot.identity()] = struct{}{}
			}
		}
	}

	sortSnapshots(queue)
	return queue
}

// dropLowestNonChinese frees a slot from the least informative record that is
// not a priority-A one.
func dropLowestNonChinese(queue *[]Snapshot, tierOf map[string]Tier, tierCount map[Tier]int) {
	items := *queue
	worst := -1
	for i, snapshot := range items {
		if tierOf[snapshot.DictionaryID] == TierChinese {
			continue
		}
		if worst < 0 || items[i].Score < items[worst].Score {
			worst = i
		}
	}
	if worst < 0 {
		return
	}
	tierCount[tierOf[items[worst].DictionaryID]]--
	*queue = append(items[:worst], items[worst+1:]...)
}

// tierCounts summarises how the corpus divides by product priority.
func tierCounts(run *Run) []NameCount {
	counts := map[string]int{}
	for _, dictionary := range run.Dictionaries {
		if dictionary.Report.Container.Health != "ok" {
			continue
		}
		counts[dictionary.Language.Tier.Label()]++
	}
	out := sortedCounts(counts)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
