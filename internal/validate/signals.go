package validate

import (
	"strings"
	"unicode"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

// Signals are conservative per-record observations. Like the dictionary-level
// warnings they extend, none of them proves a parse is wrong — every one has a
// legitimate explanation for some dictionary. What they do is rank: a record
// carrying three of these is a better use of a reviewer's attention than one
// carrying none.
const (
	SignalNoResult          = "no-result"
	SignalFallback          = "fallback-only"
	SignalLowRetention      = "low-retention"
	SignalHighDuplication   = "high-duplication"
	SignalDominantField     = "one-field-dominates"
	SignalRepeatedContent   = "repeated-content"
	SignalSubsenseEcho      = "subsense-repeats-parent"
	SignalExamplesNoDefs    = "examples-without-definitions"
	SignalHighSenseCount    = "high-sense-count"
	SignalHeadwordMismatch  = "headword-unlike-key"
	SignalBilingualNoGloss  = "bilingual-without-translation"
	SignalTranslationOnly   = "translation-without-definition"
	SignalParityFailure     = "backend-parity-failure"
	SignalSingleHugeExample = "example-longer-than-definition"
)

// Thresholds. They are set where a reasonable parse would not land by
// accident, and deliberately not tuned to make any particular number look
// better: a signal that fires on everything ranks nothing.
const (
	retentionFloor       = 0.5
	retentionMinTokens   = 40
	duplicationCeiling   = 0.25
	duplicationMinTokens = 30
	dominanceCeiling     = 0.6
	senseCountCeiling    = 40
	hugeExampleRatio     = 3
	minComparableRunes   = 24
	minBilingualClauses  = 3
)

// detectSignals looks at one validated record.
func detectSignals(snapshot Snapshot, set *entryir.EntrySet) []string {
	var signals []string
	add := func(signal string) { signals = append(signals, signal) }
	metrics := snapshot.Metrics
	fields := snapshot.Fields

	if fields.Fallback > 0 && fields.Senses == 0 {
		add(SignalFallback)
	}
	// A fallback section reproduces the record, so it cannot lose anything;
	// measuring its retention would only mask the structured parses that can.
	if fields.Fallback == 0 && metrics.SourceTokens >= retentionMinTokens && metrics.Retention < retentionFloor {
		add(SignalLowRetention)
	}
	// A four-token record whose headword also appears in its body is 33%
	// "duplicated" and perfectly correct. The ratio only means something once
	// there is enough output for it to mean something.
	if metrics.OutputTokens >= duplicationMinTokens && metrics.Duplication > duplicationCeiling {
		add(SignalHighDuplication)
	}
	if fields.Fallback == 0 && metrics.LargestFieldShare > dominanceCeiling {
		add(SignalDominantField)
	}
	if metrics.RepeatedDefinitions >= 2 || metrics.RepeatedExamples >= 3 || metrics.SectionEchoesSense > 0 {
		add(SignalRepeatedContent)
	}
	if metrics.SubsenseEchoesParent > 0 {
		add(SignalSubsenseEcho)
	}
	if fields.Examples > 0 && fields.Definitions == 0 && fields.Translations == 0 {
		add(SignalExamplesNoDefs)
	}
	if fields.Senses+fields.Subsenses > senseCountCeiling {
		add(SignalHighSenseCount)
	}
	if len(snapshot.Failures) > 0 {
		add(SignalParityFailure)
	}

	if set != nil {
		if primary := set.Primary(); primary != nil {
			if headword := strings.TrimSpace(primary.Headword); headword != "" &&
				!parser.HeadwordMatchesKey(headword, snapshot.Key) {
				add(SignalHeadwordMismatch)
			}
		}
		if bilingualWithoutGloss(set) {
			add(SignalBilingualNoGloss)
		}
		if fields.Translations > 0 && fields.Definitions == 0 {
			add(SignalTranslationOnly)
		}
		if oversizedExample(set) {
			add(SignalSingleHugeExample)
		}
	}
	return signals
}

// bilingualWithoutGloss reports a definition that clearly contains two scripts
// while the translation field stayed empty.
//
// This is the failure mode that matters most for the dictionaries this project
// is aimed at: a Chinese gloss left glued to the end of the English definition
// reads as one incoherent sentence in every client.
func bilingualWithoutGloss(set *entryir.EntrySet) bool {
	mixed, glossed := 0, 0
	for _, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		for _, part := range record.Entry.Parts {
			var walk func([]entryir.Sense)
			walk = func(senses []entryir.Sense) {
				for _, sense := range senses {
					if strings.TrimSpace(sense.Translation) != "" {
						glossed++
					} else if mixesScripts(sense.Definition) {
						mixed++
					}
					walk(sense.Subsenses)
				}
			}
			walk(part.Senses)
		}
	}
	return mixed >= minBilingualClauses && glossed == 0
}

// oversizedExample reports a sense whose example dwarfs its own definition,
// which is what a misidentified boundary looks like from the outside.
func oversizedExample(set *entryir.EntrySet) bool {
	for _, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		for _, part := range record.Entry.Parts {
			var walk func([]entryir.Sense) bool
			walk = func(senses []entryir.Sense) bool {
				for _, sense := range senses {
					definition := len([]rune(sense.Definition)) + len([]rune(sense.Translation))
					if definition >= minComparableRunes {
						for _, example := range sense.Examples {
							if len([]rune(example.Text)) > hugeExampleRatio*definition {
								return true
							}
						}
					}
					if walk(sense.Subsenses) {
						return true
					}
				}
				return false
			}
			if walk(part.Senses) {
				return true
			}
		}
	}
	return false
}

// mixesScripts reports Latin and CJK characters in the same string, which is
// the shape of an unseparated bilingual gloss.
func mixesScripts(text string) bool {
	latin, cjk := 0, 0
	for _, r := range text {
		switch {
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			latin++
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			cjk++
		}
	}
	// Both sides have to be substantial. One transliterated loanword inside a
	// Chinese definition is not a bilingual entry.
	return latin >= 4 && cjk >= 2
}
