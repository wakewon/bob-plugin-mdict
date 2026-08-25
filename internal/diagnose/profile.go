package diagnose

import (
	"sort"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

// EvidenceStrength grades how well a profile's structural fingerprint fits.
type EvidenceStrength string

const (
	// EvidenceStrong means one profile matched a majority of representative
	// samples and nothing else matched it equally well.
	EvidenceStrong EvidenceStrength = "strong"
	// EvidenceWeak means a profile matched, but only in a minority of samples.
	// A single coincidental match is exactly how a dictionary gets mislabelled,
	// so weak evidence resolves to generic.
	EvidenceWeak EvidenceStrength = "weak"
	// EvidenceConflicting means two profiles fit equally well.
	EvidenceConflicting EvidenceStrength = "conflicting"
	// EvidenceAbsent means no profile fingerprint matched at all.
	EvidenceAbsent EvidenceStrength = "absent"
)

// ProfileCandidate records how one profile scored against the samples.
type ProfileCandidate struct {
	ID string `json:"id"`
	// Matched is how many samples satisfied the full structural fingerprint.
	Matched int `json:"matched"`
	// Score is the summed fingerprint score, used only to break ties.
	Score int `json:"score"`
}

// ProfileEvidence is the conservative verdict on which profile applies.
type ProfileEvidence struct {
	// Selected is the profile ID that will actually be used, or "generic".
	Selected string           `json:"selected"`
	Strength EvidenceStrength `json:"strength"`
	Samples  int              `json:"samples"`
	// Override records a development-only parser override, so a report never
	// presents a forced choice as though the evidence had produced it.
	Override   string             `json:"override,omitempty"`
	Candidates []ProfileCandidate `json:"candidates,omitempty"`
}

// GenericProfileID is the label used when no profile applies.
const GenericProfileID = "generic"

// SelectProfile decides which profile a dictionary should be parsed with.
//
// The rule this preserves is that a structural fingerprint, not a title and not
// an output-volume contest, decides. What changes is how many records get a
// vote: one coincidental sample can no longer carry a dictionary into the wrong
// profile, and two profiles that fit equally well resolve to generic rather
// than to whichever happened to be listed first.
func SelectProfile(title string, samples []Sample) (*parser.Profile, ProfileEvidence) {
	evidence := ProfileEvidence{Selected: GenericProfileID, Strength: EvidenceAbsent, Samples: len(samples)}
	if len(samples) == 0 {
		return nil, evidence
	}
	all, err := profiles.All()
	if err != nil {
		return nil, evidence
	}

	byID := make(map[string]*parser.Profile, len(all))
	tally := make(map[string]*ProfileCandidate, len(all))
	for _, profile := range all {
		byID[profile.ID] = profile
		for _, sample := range samples {
			score := profile.Fingerprint(title, sample.Doc)
			if score <= 0 {
				continue
			}
			candidate := tally[profile.ID]
			if candidate == nil {
				candidate = &ProfileCandidate{ID: profile.ID}
				tally[profile.ID] = candidate
			}
			candidate.Matched++
			candidate.Score += score
		}
	}
	if len(tally) == 0 {
		return nil, evidence
	}

	candidates := make([]ProfileCandidate, 0, len(tally))
	for _, candidate := range tally {
		candidates = append(candidates, *candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Matched != candidates[j].Matched {
			return candidates[i].Matched > candidates[j].Matched
		}
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].ID < candidates[j].ID
	})
	evidence.Candidates = candidates

	best := candidates[0]
	// A majority of the sampled records must agree, and at least two of them:
	// a dictionary whose single richest record happens to carry a matching
	// class combination is not that dictionary.
	//
	// The exception is a dictionary that only has one record to offer. There
	// is no second opinion to be had there, and refusing the profile would
	// punish a small dictionary for being small.
	if best.Matched*2 < len(samples) || (len(samples) > 1 && best.Matched < 2) {
		evidence.Strength = EvidenceWeak
		return nil, evidence
	}
	if len(candidates) > 1 &&
		candidates[1].Matched == best.Matched && candidates[1].Score == best.Score {
		evidence.Strength = EvidenceConflicting
		return nil, evidence
	}
	evidence.Strength = EvidenceStrong
	evidence.Selected = best.ID
	return byID[best.ID], evidence
}
