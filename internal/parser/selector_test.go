package parser

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func mustParse(t *testing.T, markup string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSelectorMatching(t *testing.T) {
	doc := mustParse(t, `
		<div class="p-g" id="block">
		  <span class="pos-g"><span class="pos">verb</span></span>
		  <span class="n-g"><span class="d">first</span></span>
		  <span class="n-g"><span class="d">second</span></span>
		  <a class="speaker brefile" href="sound://synthetic/uk/a.mp3">uk</a>
		  <a class="speaker amefile" href="sound://synthetic/us/a.mp3">us</a>
		</div>`)

	cases := []struct {
		selector string
		want     int
	}{
		{".n-g", 2},
		{".p-g .pos-g .pos", 1},
		{"a.speaker.brefile", 1},
		{"a.speaker", 2},
		{"#block", 1},
		{"[href^=sound://synthetic/uk]", 1},
		{"[href*=us]", 1},
		{"[href$=.mp3]", 2},
		{"span[class]", 6},
		{".n-g, .pos", 3},
		{".missing", 0},
		{"", 0},
	}
	for _, tc := range cases {
		t.Run(tc.selector, func(t *testing.T) {
			got := QueryAllNested(doc, ParseSelector(tc.selector))
			if len(got) != tc.want {
				t.Errorf("QueryAllNested(%q) matched %d nodes, want %d", tc.selector, len(got), tc.want)
			}
		})
	}
}

// TestQueryAllDoesNotDescendIntoMatches is the behaviour sense extraction
// relies on: a nested block with the same class is a subsense, not a sibling.
func TestQueryAllDoesNotDescendIntoMatches(t *testing.T) {
	doc := mustParse(t, `
		<ul class="semb">
		  <li>outer<ol class="subSenses"><li class="subSense">inner</li></ol></li>
		  <li>second</li>
		</ul>`)
	shallow := QueryAll(doc, ParseSelector(".semb li"))
	if len(shallow) != 2 {
		t.Errorf("QueryAll matched %d nodes, want 2 (subsenses must not be counted)", len(shallow))
	}
	nested := QueryAllNested(doc, ParseSelector(".semb li"))
	if len(nested) != 3 {
		t.Errorf("QueryAllNested matched %d nodes, want 3", len(nested))
	}
}

func TestMalformedSelectorIsIgnoredNotFatal(t *testing.T) {
	// A broken rule in one profile must not take a whole dictionary down.
	sel := ParseSelectors([]string{".good", "..bad", "[", ".alsogood"})
	doc := mustParse(t, `<div class="good"></div><div class="alsogood"></div><div class="bad"></div>`)
	if got := len(QueryAllNested(doc, sel)); got != 2 {
		t.Errorf("matched %d nodes, want 2", got)
	}
}

func TestRemoveMatchingDetachesSubtrees(t *testing.T) {
	doc := mustParse(t, `<div class="keep">text<span class="drop">noise</span> tail</div>`)
	if n := RemoveMatching(doc, ParseSelector(".drop")); n != 1 {
		t.Fatalf("removed %d nodes, want 1", n)
	}
	if got := Text(doc, TextOptions{}); got != "text tail" {
		t.Errorf("Text after removal = %q, want %q", got, "text tail")
	}
}

func TestTextSkipsHiddenAndScripts(t *testing.T) {
	doc := mustParse(t, `<div>visible<span style="display:none">hidden</span><script>var x=1</script><style>.a{}</style></div>`)
	if got := Text(doc, TextOptions{SkipHidden: true}); got != "visible" {
		t.Errorf("Text = %q, want %q", got, "visible")
	}
}

func TestTextSeparatesBlocks(t *testing.T) {
	// Without block separation, adjacent sentences run together into one word.
	doc := mustParse(t, `<div><p>First sentence.</p><p>Second sentence.</p></div>`)
	if got := Text(doc, TextOptions{}); got != "First sentence. Second sentence." {
		t.Errorf("Text = %q", got)
	}
}
