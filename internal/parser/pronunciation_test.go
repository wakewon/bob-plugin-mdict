package parser_test

import (
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
)

// resolveAll pretends every reference exists in the MDD.
type resolveAll struct{}

func (resolveAll) ResolveAudio(ref string) *entryir.Audio {
	return &entryir.Audio{ResourceRef: ref, URL: "http://127.0.0.1/x", MIMEType: "audio/mpeg"}
}

// resolveNone pretends the MDD is missing, which is what happens when a user
// installs an .mdx without its .mdd.
type resolveNone struct{}

func (resolveNone) ResolveAudio(string) *entryir.Audio { return nil }

// TestUKAndUSAudioAreNotShared is the regression guard for the single worst
// failure mode in a dictionary plugin: attaching one clip to both regions, so
// the British button plays the American recording.
func TestUKAndUSAudioAreNotShared(t *testing.T) {
	markup := []byte(`
		<span class="Head">
		  <span class="HWD">flimber</span>
		  <a class="PronCodes" href="sound://synthetic/ameProns/wrong-wrapper.mp3">
		    <span class="PRON">ˈflɪmbə</span>
		  </a>
		  <a class="speaker brefile" href="sound://synthetic/breProns/flimber-uk.mp3"> </a>
		  <a class="speaker amefile" href="sound://synthetic/ameProns/flimber-us.mp3"> </a>
		</span>`)

	entry, err := parser.Parse(markup, parser.Options{
		Profile: profiles.ByID("ldoce5pp"),
		Audio:   resolveAll{},
	})
	if err != nil {
		t.Fatal(err)
	}

	byRegion := map[entryir.Region]*entryir.Pronunciation{}
	for i := range entry.Pronunciations {
		byRegion[entry.Pronunciations[i].AudioRegion] = &entry.Pronunciations[i]
	}
	uk, ok := byRegion[entryir.RegionUK]
	if !ok || uk.Audio == nil {
		t.Fatalf("no UK audio: %+v", entry.Pronunciations)
	}
	us, ok := byRegion[entryir.RegionUS]
	if !ok || us.Audio == nil {
		t.Fatalf("no US audio: %+v", entry.Pronunciations)
	}
	if uk.Audio.ResourceRef == us.Audio.ResourceRef {
		t.Fatalf("UK and US resolved to the same clip %q", uk.Audio.ResourceRef)
	}
	if want := "sound://synthetic/breProns/flimber-uk.mp3"; uk.Audio.ResourceRef != want {
		t.Errorf("UK audio = %q, want %q", uk.Audio.ResourceRef, want)
	}
	if want := "sound://synthetic/ameProns/flimber-us.mp3"; us.Audio.ResourceRef != want {
		t.Errorf("US audio = %q, want %q", us.Audio.ResourceRef, want)
	}
}

// TestNoMDDMeansNoAudio checks the core product promise: without a real
// dictionary recording, nothing is offered in its place.
func TestNoMDDMeansNoAudio(t *testing.T) {
	markup := []byte(`
		<span class="h-g"><span class="top-g"><span class="h">flimber</span>
		<span class="ei-g">
		  <a class="fayin" href="sound://synthetic/oald/uk-flimber.mp3"><span class="phon-gb">ˈflɪmbə</span></a>
		</span></span></span>`)

	entry, err := parser.Parse(markup, parser.Options{
		Profile: profiles.ByID("oald8"),
		Audio:   resolveNone{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Pronunciations) == 0 {
		t.Fatal("the transcription should survive even without audio")
	}
	for _, item := range entry.Pronunciations {
		if item.Audio != nil {
			t.Errorf("audio was invented for %q with no MDD resource", item.IPARegion)
		}
		if item.IPA == "" {
			t.Error("transcription was dropped along with the missing audio")
		}
	}
}

func TestSharedIPAAndRegionalAudioRemainSeparateFacts(t *testing.T) {
	markup := []byte(`<div>
		<span class="pron">/ˈflɪmbə/</span>
		<a class="speaker uk" href="sound://synthetic/uk.mp3"></a>
		<a class="speaker us" href="sound://synthetic/us.mp3"></a>
	</div>`)
	entry, err := parser.Parse(markup, parser.Options{Audio: resolveAll{}})
	if err != nil {
		t.Fatal(err)
	}
	var shared bool
	var ukAudio, usAudio bool
	for _, item := range entry.Pronunciations {
		if item.IPARegion == entryir.RegionNeutral && item.IPA != "" && item.Audio == nil {
			shared = true
		}
		ukAudio = ukAudio || item.AudioRegion == entryir.RegionUK && item.Audio != nil
		usAudio = usAudio || item.AudioRegion == entryir.RegionUS && item.Audio != nil
	}
	if !shared || !ukAudio || !usAudio {
		t.Fatalf("shared/regional facts were conflated: %+v", entry.Pronunciations)
	}
}

// TestNilAudioResolverIsSafe covers callers that do not want audio at all.
func TestNilAudioResolverIsSafe(t *testing.T) {
	entry, err := parser.Parse([]byte(`<div class="sense"><span class="def">A definition.</span></div>`), parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if entry.SenseCount() != 1 {
		t.Errorf("SenseCount = %d, want 1", entry.SenseCount())
	}
}

func TestParseHandlesEmptyAndGarbageInput(t *testing.T) {
	for _, input := range []string{"", "   ", "\x00\x01\x02", "<<<>>>", "@@@LINK=other"} {
		entry, err := parser.Parse([]byte(input), parser.Options{Headword: "x"})
		if err != nil {
			t.Errorf("Parse(%q) returned an error: %v", input, err)
			continue
		}
		if entry.Headword != "x" {
			t.Errorf("Parse(%q) lost the fallback headword", input)
		}
	}
}
