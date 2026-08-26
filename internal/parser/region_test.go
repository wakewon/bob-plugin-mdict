package parser

import (
	"bytes"
	"testing"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// A pronunciation block that prints its own accent label must not inherit the
// label of the block above it. Getting this wrong puts an American recording
// behind a British flag, which is the failure this project cares about most.
func TestRegionPrefersOwnLabel(t *testing.T) {
	const markup = `<div><div id="uk"><span>BrE</span> <span>/ˈwɛksl/</span></div>` +
		`<div id="us"><span>NAmE</span> <span>/ˈwɛksəl/</span></div></div>`
	doc, err := html.Parse(bytes.NewReader([]byte(markup)))
	if err != nil {
		t.Fatal(err)
	}
	var uk, us *html.Node
	Walk(doc, func(node *html.Node) bool {
		switch Attr(node, "id") {
		case "uk":
			uk = node
		case "us":
			us = node
		}
		return true
	})
	if uk == nil || us == nil {
		t.Fatal("fixture nodes not found")
	}
	if got := DetectRegion(DescriptorText(uk)); got != entryir.RegionUK {
		t.Errorf("British block detected as %q", got)
	}
	if got := DetectRegion(DescriptorText(us)); got != entryir.RegionUS {
		t.Errorf("American block detected as %q, want us — it follows the British one", got)
	}
}

// A block with no label of its own may still borrow a neighbour's, which is
// how dictionaries that label only the first of a pair are read correctly.
func TestRegionBorrowsFromNeighbourWhenSilent(t *testing.T) {
	const markup = `<div><div id="label">BrE</div><div id="quiet">/ˈwɛksl/</div></div>`
	doc, err := html.Parse(bytes.NewReader([]byte(markup)))
	if err != nil {
		t.Fatal(err)
	}
	var quiet *html.Node
	Walk(doc, func(node *html.Node) bool {
		if Attr(node, "id") == "quiet" {
			quiet = node
		}
		return true
	})
	if got := DetectRegion(DescriptorText(quiet)); got != entryir.RegionUK {
		t.Errorf("unlabelled block detected as %q, want uk from its neighbour", got)
	}
}
