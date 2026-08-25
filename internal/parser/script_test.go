package parser

import "testing"

func TestDominantScript(t *testing.T) {
	cases := []struct {
		text string
		want script
	}{
		{"a small brass fitting", scriptLatin},
		{"系帆小钩", scriptCJK},
		{"ゲルトを量る", scriptCJK},
		{"a small hook 系帆小钩", scriptMixed},
		{"Dizionario delle collocazioni", scriptLatin},
		{"/ˈwɛksl/", scriptLatin},
		{"12345", scriptUnknown},
	}
	for _, test := range cases {
		if got := dominantScript(test.text); got != test.want {
			t.Errorf("dominantScript(%q) = %q, want %q", test.text, got, test.want)
		}
	}
}

func TestScriptOpposite(t *testing.T) {
	if scriptLatin.opposite() != scriptCJK || scriptCJK.opposite() != scriptLatin {
		t.Error("latin and cjk are each other's opposite")
	}
	// A mixed or unclassifiable headword gives no direction to translate in,
	// and the rule has to switch itself off rather than guess.
	if scriptMixed.opposite() != scriptUnknown || scriptUnknown.opposite() != scriptUnknown {
		t.Error("an unclassifiable headword must disable script-based glossing")
	}
}

func TestHeadwordMatchesKey(t *testing.T) {
	matching := [][2]string{
		{"a·ban·don", "abandon"},
		{"Abandon", "abandon"},
		{"anti-", "anti-Gallicanism"},
		{"bird", "bird"},
		{"con•trib•ute", "contribute"},
	}
	for _, pair := range matching {
		if !HeadwordMatchesKey(pair[0], pair[1]) {
			t.Errorf("HeadwordMatchesKey(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
	// A section heading is not a headword, however short it is.
	for _, pair := range [][2]string{{"Examples", "anthology"}, {"Word History", "get"}} {
		if HeadwordMatchesKey(pair[0], pair[1]) {
			t.Errorf("HeadwordMatchesKey(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}
