package diagnose

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

// Warning codes. They are deliberately phrased as observations rather than
// verdicts: every one of them has a legitimate explanation for some dictionary,
// and the only claim being made is that a human should look.
const (
	WarnRichHTMLNoDefinitions = "rich-html-no-definitions"
	WarnHighFallbackRate      = "high-fallback-rate"
	WarnImplausibleSenseCount = "implausible-sense-count"
	WarnOversizedDefinition   = "oversized-definition"
	WarnDuplicateDefinitions  = "duplicate-definitions"
	WarnBilingualNoTranslate  = "bilingual-without-translations"
	WarnExamplesNoDefinitions = "examples-without-definitions"
	WarnHeadwordMismatch      = "headword-unlike-lookup-key"
	WarnUntypedSectionsOnly   = "content-only-in-untyped-sections"
)

// Thresholds are set where a reasonable dictionary would not land by accident.
const (
	richRecordBytes      = 1500
	fallbackRateWarn     = 0.5
	implausibleSenses    = 40
	oversizedDefinition  = 800
	duplicateDefinitions = 4
	headwordMismatchRate = 0.6
)

// detectWarnings runs the sanity checks over one dictionary's samples.
func detectWarnings(report Report, samples []Sample, entries []*entryir.Entry) []Warning {
	if len(entries) == 0 {
		return nil
	}
	var warnings []Warning
	add := func(code, format string, args ...any) {
		warnings = append(warnings, Warning{Code: code, Detail: fmt.Sprintf(format, args...)})
	}
	coverage := report.Coverage

	if coverage.Definitions == 0 && report.DOM.MedianBytes >= richRecordBytes {
		add(WarnRichHTMLNoDefinitions,
			"median record is %d bytes of markup but no sample yielded a definition",
			report.DOM.MedianBytes)
	}
	if coverage.Samples >= 3 && coverage.FallbackRate >= fallbackRateWarn {
		add(WarnHighFallbackRate, "%d of %d samples produced no structure at all",
			coverage.Fallback, coverage.Samples)
	}
	if coverage.Examples > 0 && coverage.Definitions == 0 {
		add(WarnExamplesNoDefinitions,
			"%d samples yielded examples while none yielded a definition", coverage.Examples)
	}
	if coverage.Structured == 0 && coverage.Sections > 0 && coverage.Fallback == 0 {
		add(WarnUntypedSectionsOnly,
			"every sample's content landed in untyped sections rather than senses")
	}

	maxSenses, maxDefinition, duplicates, mismatches, headwords := 0, 0, 0, 0, 0
	bilingualSamples, translated := 0, 0
	for i, entry := range entries {
		senses := entry.SenseCount()
		if senses > maxSenses {
			maxSenses = senses
		}
		var definitions []string
		hasTranslation := false
		for _, part := range entry.Parts {
			forEachSense(part.Senses, func(sense entryir.Sense) {
				definition := strings.TrimSpace(sense.Definition)
				if definition == "" {
					return
				}
				definitions = append(definitions, strings.ToLower(definition))
				if length := len([]rune(definition)); length > maxDefinition {
					maxDefinition = length
				}
				if mixesScripts(definition) {
					bilingualSamples++
				}
				if strings.TrimSpace(sense.Translation) != "" {
					hasTranslation = true
				}
			})
		}
		if hasTranslation {
			translated++
		}
		if unique := distinct(definitions); len(definitions) >= duplicateDefinitions && unique*2 <= len(definitions) {
			duplicates++
		}
		if headword := strings.TrimSpace(entry.Headword); headword != "" && i < len(samples) {
			headwords++
			if !parser.HeadwordMatchesKey(headword, samples[i].Key) {
				mismatches++
			}
		}
	}

	if maxSenses > implausibleSenses {
		add(WarnImplausibleSenseCount,
			"one sample produced %d senses, which usually means a list was read as senses", maxSenses)
	}
	if maxDefinition > oversizedDefinition {
		add(WarnOversizedDefinition,
			"longest extracted definition is %d characters, suggesting a whole record was swallowed", maxDefinition)
	}
	if duplicates > 0 {
		add(WarnDuplicateDefinitions,
			"%d of %d samples repeated more than half of their definitions", duplicates, len(entries))
	}
	// Only meaningful for a dictionary whose definitions genuinely mix scripts:
	// a monolingual Japanese dictionary is not bilingual just because it is not
	// written in Latin script.
	if bilingualSamples > 0 && translated == 0 {
		add(WarnBilingualNoTranslate,
			"definitions mix scripts in %d places but no translation field was extracted", bilingualSamples)
	}
	if headwords >= 3 && float64(mismatches) >= headwordMismatchRate*float64(headwords) {
		add(WarnHeadwordMismatch,
			"%d of %d parsed headwords do not resemble the key they were looked up under",
			mismatches, headwords)
	}
	return warnings
}

func distinct(values []string) int {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	return len(seen)
}

// mixesScripts reports whether text contains both Latin letters and CJK
// characters, which is the shape of a bilingual gloss.
func mixesScripts(text string) bool {
	latin, cjk := false, false
	for _, r := range text {
		switch {
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			latin = true
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			cjk = true
		}
		if latin && cjk {
			return true
		}
	}
	return false
}
