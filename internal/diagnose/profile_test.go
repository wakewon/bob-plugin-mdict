package diagnose_test

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
)

// Profile selection is the one place where being wrong is worse than being
// unhelpful: a dictionary parsed with another dictionary's selectors produces
// confident nonsense, while generic parsing produces something usable. These
// tests fix the conservative behaviour in place.

func samplesFrom(t *testing.T, markup ...string) []diagnose.Sample {
	t.Helper()
	out := make([]diagnose.Sample, 0, len(markup))
	for i, item := range markup {
		doc, err := html.Parse(bytes.NewReader([]byte(item)))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, diagnose.Sample{Key: string(rune('a' + i)), HTML: []byte(item), Doc: doc})
	}
	return out
}

// oxfordXML is the publisher-XML shape shared by the OALD9 family, written
// from scratch with invented content.
const oxfordXML = `<top-g><h>wexal</h><pron-gs><pron-g-blk><brelabel>BrE</brelabel>` +
	`<pron-g><phon-blk>/<phon>ˈwɛksl</phon>/</phon-blk></pron-g></pron-g-blk></pron-gs></top-g>` +
	`<subentry-g><top-g><pos-g><pos-blk><pos>noun</pos></pos-blk></pos-g></top-g>` +
	`<sn-gs><sn-blk><sn-g><def>a small hook used to fasten a sail</def></sn-g></sn-blk></sn-gs></subentry-g>`

// unrelated shares nothing with any profile.
const unrelated = `<div class="zz1"><span class="zz2">glimmet</span>` +
	`<span class="zz3">An invented thing.</span></div>`

func TestStrongEvidenceSelectsTheProfile(t *testing.T) {
	samples := samplesFrom(t, oxfordXML, oxfordXML, oxfordXML, oxfordXML)
	profile, evidence := diagnose.SelectProfile("A dictionary", samples)
	if profile == nil || profile.ID != "oxford-xml-learner" {
		t.Fatalf("selected %v, want oxford-xml-learner", evidence.Selected)
	}
	if evidence.Strength != diagnose.EvidenceStrong {
		t.Errorf("evidence = %q, want strong", evidence.Strength)
	}
	if evidence.Candidates[0].Matched != 4 {
		t.Errorf("matched %d of 4 samples", evidence.Candidates[0].Matched)
	}
}

func TestMinorityEvidenceFallsBackToGeneric(t *testing.T) {
	// One record out of five happens to carry the structure. That is how a
	// dictionary gets mislabelled, so it must not be enough.
	samples := samplesFrom(t, oxfordXML, unrelated, unrelated, unrelated, unrelated)
	profile, evidence := diagnose.SelectProfile("A dictionary", samples)
	if profile != nil {
		t.Errorf("selected %q on one matching sample in five", profile.ID)
	}
	if evidence.Strength != diagnose.EvidenceWeak {
		t.Errorf("evidence = %q, want weak", evidence.Strength)
	}
	if evidence.Selected != diagnose.GenericProfileID {
		t.Errorf("selected = %q, want generic", evidence.Selected)
	}
}

func TestNoEvidenceIsAbsentNotWeak(t *testing.T) {
	_, evidence := diagnose.SelectProfile("A dictionary", samplesFrom(t, unrelated, unrelated, unrelated))
	if evidence.Strength != diagnose.EvidenceAbsent {
		t.Errorf("evidence = %q, want absent", evidence.Strength)
	}
}

// A title alone must never select a profile; repacks rename themselves freely.
func TestTitleAloneDoesNotSelectAProfile(t *testing.T) {
	samples := samplesFrom(t, unrelated, unrelated, unrelated)
	if profile, _ := diagnose.SelectProfile("牛津高阶英汉双解词典 Oxford Advanced Learner's", samples); profile != nil {
		t.Errorf("a matching title selected %q with no matching structure", profile.ID)
	}
}

// A dictionary with a single record has no second opinion to offer, and should
// not be denied its profile for it.
func TestSingleSampleDictionaryCanStillMatch(t *testing.T) {
	profile, evidence := diagnose.SelectProfile("A dictionary", samplesFrom(t, oxfordXML))
	if profile == nil || evidence.Strength != diagnose.EvidenceStrong {
		t.Errorf("single-record dictionary got %q / %q", evidence.Selected, evidence.Strength)
	}
}

func TestProfileOverrideIsADebuggingAid(t *testing.T) {
	profile, _ := diagnose.SelectProfile("A dictionary", samplesFrom(t, oxfordXML, oxfordXML))
	if profile == nil {
		t.Fatal("fixture should have matched a profile")
	}

	forced, label := diagnose.ApplyProfileOverride(profile, "generic")
	if forced != nil || label != diagnose.GenericProfileID {
		t.Errorf("override generic gave %v / %q", forced, label)
	}
	kept, label := diagnose.ApplyProfileOverride(profile, "auto")
	if kept != profile || label != profile.ID {
		t.Errorf("override auto changed the selection to %q", label)
	}
	other, label := diagnose.ApplyProfileOverride(profile, "oald8")
	if other == nil || other.ID != "oald8" || label != "oald8" {
		t.Errorf("override by ID gave %q", label)
	}
	// An unknown name is a typo, not an instruction to disable parsing.
	unchanged, label := diagnose.ApplyProfileOverride(profile, "no-such-profile")
	if unchanged != profile || label != profile.ID {
		t.Errorf("unknown override changed the selection to %q", label)
	}
}

func TestEvidenceListsEveryCandidate(t *testing.T) {
	_, evidence := diagnose.SelectProfile("A dictionary", samplesFrom(t, oxfordXML, oxfordXML))
	var ids []string
	for _, candidate := range evidence.Candidates {
		ids = append(ids, candidate.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), "oxford-xml-learner") {
		t.Errorf("candidates = %v, want the matching profile listed", ids)
	}
}
