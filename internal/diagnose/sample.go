// Package diagnose inspects a dictionary the way a developer would: it picks a
// handful of representative records, reports what the container and the DOM
// actually contain, and measures how much semantic structure the current
// parser recovers from them.
//
// It is a development and compatibility-analysis tool, not part of the lookup
// hot path. The one piece the running service shares with it is representative
// sampling, which also decides which profile a dictionary gets.
package diagnose

import (
	"bytes"
	"crypto/sha256"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
)

// Sample is one record chosen to represent a dictionary's recurring template.
type Sample struct {
	// Key is the MDX key the record was reached through. It is retained for
	// local debugging only and is never written to a shareable report.
	Key string
	// HTML is the resolved record content.
	HTML []byte
	// Doc is the parsed document, shared by every consumer so a dictionary is
	// only parsed once per sample.
	Doc *html.Node
	// Score is the structural informativeness that got it selected.
	Score int
}

// SampleOptions controls representative sampling.
type SampleOptions struct {
	// Pool is how many candidate keys are resolved and scored.
	Pool int
	// Keep is how many of them are retained as representative samples.
	Keep int
}

// DetectionSampling is the budget used during rescan, where sampling only has
// to decide a profile. It is deliberately smaller than the diagnostic budget:
// every dictionary in the user's library pays for it at startup.
var DetectionSampling = SampleOptions{Pool: 24, Keep: 5}

// DiagnosticSampling is the budget used by the diagnostic commands, which run
// on demand and can afford to look harder.
var DiagnosticSampling = SampleOptions{Pool: 64, Keep: 10}

func (o SampleOptions) normalized() SampleOptions {
	if o.Pool <= 0 {
		o.Pool = DetectionSampling.Pool
	}
	if o.Keep <= 0 {
		o.Keep = DetectionSampling.Keep
	}
	if o.Keep > o.Pool {
		o.Keep = o.Pool
	}
	return o
}

// minRecordBytes filters out redirect stubs and one-line cross-reference
// records, which carry no template information at all.
const minRecordBytes = 120

// Samples picks the structurally most informative records from a dictionary.
//
// The selection is language-independent and deterministic: candidates are
// strided across the key index, scored from their markup alone, and the best
// are kept. No word list, script assumption or locale is involved anywhere.
func Samples(dict *mdict.Dictionary, opts SampleOptions) []Sample {
	opts = opts.normalized()
	if dict == nil || dict.Info().Health != mdict.HealthOK {
		return nil
	}

	// Ask for more keys than the pool needs: redirects, stubs and unresolvable
	// records all fall out before scoring.
	keys := dict.SampleKeys(opts.Pool * 3)
	var candidates []Sample
	seen := make(map[[32]byte]struct{}, opts.Pool)
	for _, key := range keys {
		if len(candidates) >= opts.Pool {
			break
		}
		set, err := dict.LookupAll(key)
		if err != nil || len(set.Records) == 0 {
			continue
		}
		raw := set.Records[0].HTML
		if len(raw) < minRecordBytes {
			continue
		}
		// Many dictionaries store hundreds of keys pointing at one shared
		// record. Keeping them all would let a single template dominate the
		// sample and hide the rest of the dictionary. The whole record is
		// hashed: dictionaries begin every record with the same stylesheet
		// link, so a prefix would collapse the entire sample into one entry.
		fingerprint := sha256.Sum256(raw)
		if _, duplicate := seen[fingerprint]; duplicate {
			continue
		}
		seen[fingerprint] = struct{}{}
		candidates = append(candidates, Sample{
			Key:   set.Records[0].MatchedKey,
			HTML:  raw,
			Score: ScoreRecord(raw),
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	// Highest score first, with the source order as a stable tie-break so the
	// same dictionary always yields the same samples.
	order := make(map[string]int, len(candidates))
	for i, candidate := range candidates {
		order[candidate.Key] = i
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return order[candidates[i].Key] < order[candidates[j].Key]
	})
	if len(candidates) > opts.Keep {
		candidates = candidates[:opts.Keep]
	}

	kept := candidates[:0]
	for _, candidate := range candidates {
		doc, err := html.Parse(bytes.NewReader(candidate.HTML))
		if err != nil {
			continue
		}
		candidate.Doc = doc
		kept = append(kept, candidate)
	}
	return kept
}

var (
	tagRe   = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)`)
	classRe = regexp.MustCompile(`(?i)class\s*=\s*"([^"]*)"|class\s*=\s*'([^']*)'`)
)

// audioHints are the references a dictionary uses to point at a recording.
// Finding one in the MDX says a pronunciation is *referenced*; whether it can
// be played is a separate question the MDD answers.
var audioHints = []string{"sound://", "snd://", ".mp3", ".spx", ".wav", ".ogg"}

// ScoreRecord rates how much of a dictionary's recurring template one record
// exposes, from its markup alone.
//
// Richer structure, more distinct class vocabulary, repeated sense-like
// siblings, cross-references and pronunciation references all mean a record
// shows more of what the parser will have to cope with. Length contributes but
// is capped, so one enormous encyclopedia article cannot outrank a properly
// structured entry.
func ScoreRecord(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	lower := bytes.ToLower(raw)

	tags := make(map[string]struct{}, 16)
	for _, match := range tagRe.FindAllSubmatch(lower, 400) {
		tags[string(match[1])] = struct{}{}
	}
	classes := make(map[string]int, 32)
	for _, match := range classRe.FindAllSubmatch(lower, 400) {
		value := match[1]
		if len(value) == 0 {
			value = match[2]
		}
		for _, name := range strings.Fields(string(value)) {
			classes[name]++
		}
	}

	score := 0
	score += minInt(len(tags), 12)
	score += minInt(len(classes), 20)
	score += minInt(len(raw)/512, 10)

	// A class that repeats several times inside one record is the signature of
	// a list of senses, examples or idioms — exactly the structure worth
	// sampling.
	repeated := 0
	for _, count := range classes {
		if count >= 3 {
			repeated++
		}
	}
	score += minInt(repeated, 6)

	for _, hint := range audioHints {
		if bytes.Contains(lower, []byte(hint)) {
			score += 3
			break
		}
	}
	if bytes.Contains(lower, []byte("entry://")) || bytes.Contains(lower, []byte("<a ")) {
		score += 2
	}
	return score
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
