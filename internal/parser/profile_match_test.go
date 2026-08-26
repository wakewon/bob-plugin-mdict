package parser

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestProfileFingerprintCanRequireTitleAndStructure(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<div class="CK"><div class="JS"><span class="GZ">group</span></div></div>`))
	if err != nil {
		t.Fatal(err)
	}
	profile := &Profile{Match: ProfileMatch{
		TitleContains: []string{"Oxford Collocations"},
		RequireTitle:  true,
		Selectors:     []string{".CK .JS", ".JS .GZ"},
	}}
	if score := profile.Fingerprint("Oxford Idioms Dictionary", doc); score != 0 {
		t.Fatalf("shared structure without required title scored %d", score)
	}
	if score := profile.Fingerprint("Oxford Collocations Dictionary", doc); score == 0 {
		t.Fatal("matching title and structure did not select profile")
	}
}
