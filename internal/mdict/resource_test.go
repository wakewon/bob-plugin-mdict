package mdict

import "testing"

func TestParseLinkTarget(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		// Real records terminate with CRLF and a NUL byte, which ordinary
		// whitespace trimming leaves in place.
		{"three at signs with NUL", "@@@LINK=Hello!\r\n\x00", "Hello!", true},
		{"three at signs plain", "@@@LINK=colour", "colour", true},
		{"two at signs variant", "@@LINK=color", "color", true},
		{"leading BOM", "\ufeff@@@LINK=colour\r\n\x00", "colour", true},
		{"target with spaces", "@@@LINK=take off\r\n\x00", "take off", true},
		{"not a redirect", "<div>a real entry</div>", "", false},
		{"empty target", "@@@LINK=\r\n\x00", "", false},
		{"marker inside content", "<p>see @@@LINK=x</p>", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseLinkTarget([]byte(tc.content))
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("ParseLinkTarget(%q) = (%q, %v), want (%q, %v)", tc.content, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestNormalizeResourceKey(t *testing.T) {
	// An MDD stores keys Windows-style while entry HTML uses a URL scheme.
	// Both must fold to the same index key or no audio ever resolves.
	cases := map[string]string{
		`\uk\hello__gb_1.mp3`:               "uk/hello__gb_1.mp3",
		`sound://uk/hello__gb_1.mp3`:        "uk/hello__gb_1.mp3",
		`snd://uk/hello__gb_1.mp3`:          "uk/hello__gb_1.mp3",
		`\\media\\english\\breProns\\a.mp3`: "media/english/breprons/a.mp3",
		`/COLmp3/00016.mp3`:                 "colmp3/00016.mp3",
		`sound://media/spx/x.spx?v=2`:       "media/spx/x.spx",
		`file://pic/a.png#frag`:             "pic/a.png",
		"":                                  "",
		"   ":                               "",
	}
	for input, want := range cases {
		if got := NormalizeResourceKey(input); got != want {
			t.Errorf("NormalizeResourceKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResourceCandidatesIncludesBareName(t *testing.T) {
	got := ResourceCandidates("sound://uk/hello.mp3")
	if len(got) != 2 || got[0] != "uk/hello.mp3" || got[1] != "hello.mp3" {
		t.Errorf("ResourceCandidates = %v, want [uk/hello.mp3 hello.mp3]", got)
	}
}

func TestAudioClassification(t *testing.T) {
	if !IsAudioRef("sound://uk/a.mp3") {
		t.Error("mp3 should be audio")
	}
	if IsAudioRef("pic/a.png") {
		t.Error("png should not be audio")
	}
	if !IsSpeexRef(`\media\spx\a.spx`) {
		t.Error("spx should be speex")
	}
	// SPX is transcoded before it leaves the service, so it must advertise WAV.
	if got := MIMEType("sound://media/spx/a.spx"); got != "audio/wav" {
		t.Errorf("MIMEType(spx) = %q, want audio/wav", got)
	}
	if got := MIMEType("sound://uk/a.mp3"); got != "audio/mpeg" {
		t.Errorf("MIMEType(mp3) = %q, want audio/mpeg", got)
	}
}
