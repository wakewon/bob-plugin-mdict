package parser

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestBoundaryAwareDOMTextJoining(t *testing.T) {
	cases := map[string]string{
		`<p>good<span>or</span>the general good</p>`:              "good or the general good",
		`<p>ropemaker<span>s</span> join line<span>s</span>.</p>`: "ropemakers join lines.",
		`<p>中文<span>词条</span></p>`:                                "中文词条",
		`<p>中文<span>entry</span></p>`:                             "中文 entry",
		`<p>entry<span>词条</span></p>`:                             "entry 词条",
		`<p>word<span>,</span> next</p>`:                          "word, next",
	}
	for markup, want := range cases {
		doc, err := html.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatal(err)
		}
		if got := Text(doc, TextOptions{SkipHidden: true}); got != want {
			t.Errorf("Text(%s) = %q, want %q", markup, got, want)
		}
	}
}
