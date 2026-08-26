package parser

import (
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

func TestFallbackPreservesResolvedImageAndTableOrder(t *testing.T) {
	entry, err := Parse([]byte(`<article><p>before illustration</p><img src="img/pic.png" alt="invented diagram"><p>after illustration</p><table><tr><th>Form</th><th>Value</th></tr><tr><td>plural</td><td>wexals</td></tr></table><p>after table</p></article>`), Options{
		Headword: "wexal",
		Image: ImageResolverFunc(func(ref, alt string) *entryir.Image {
			if ref != "img/pic.png" {
				t.Fatalf("ref = %q", ref)
			}
			return &entryir.Image{ResourceRef: ref, URL: "http://127.0.0.1:15321/v2/resource/TOKEN", Alt: alt, MIMEType: "image/png"}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Sections) != 1 {
		t.Fatalf("sections = %+v", entry.Sections)
	}
	blocks := entry.Sections[0].Blocks
	if len(blocks) != 5 || blocks[0].Kind != entryir.RichText || blocks[1].Kind != entryir.RichImage ||
		blocks[2].Kind != entryir.RichText || blocks[3].Kind != entryir.RichTable || blocks[4].Kind != entryir.RichText {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[1].Image.Alt != "invented diagram" {
		t.Fatalf("rich content = %+v", blocks)
	}
	// The <th> row is the table's own header, so it is lifted out of the body
	// rather than left to be guessed at by whatever renders it.
	table := blocks[3]
	if len(table.Header) != 2 || table.Header[0] != "Form" || table.Header[1] != "Value" {
		t.Fatalf("table header = %+v", table.Header)
	}
	if len(table.Rows) != 1 || table.Rows[0][0] != "plural" {
		t.Fatalf("table rows = %+v", table.Rows)
	}
}

// A table with no <th> declares no header. Saying so is different from
// inventing one, and lets the renderer decide what to do about it.
func TestTableWithoutHeaderCellsDeclaresNoHeader(t *testing.T) {
	entry, err := Parse([]byte(`<article><p>note</p><table><tr><td>present</td><td>wexals</td></tr><tr><td>past</td><td>wexalled</td></tr></table><img src="img/pic.png"></article>`), Options{
		Headword: "wexal",
		Image: ImageResolverFunc(func(ref, alt string) *entryir.Image {
			return &entryir.Image{ResourceRef: ref, URL: "http://127.0.0.1/resource/T", Alt: alt}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var table *entryir.RichBlock
	for i, block := range entry.Sections[0].Blocks {
		if block.Kind == entryir.RichTable {
			table = &entry.Sections[0].Blocks[i]
		}
	}
	if table == nil {
		t.Fatalf("no table block: %+v", entry.Sections)
	}
	if len(table.Header) != 0 {
		t.Errorf("invented a header the source did not declare: %+v", table.Header)
	}
	if len(table.Rows) != 2 {
		t.Errorf("rows = %+v", table.Rows)
	}
}

// Header cells used to label each row down the side are not a header row.
// Markdown has one header to give and it belongs to the columns.
func TestRowLabelHeaderCellsAreNotTakenAsTheHeaderRow(t *testing.T) {
	entry, err := Parse([]byte(`<article><table><tr><th>present</th><td>wexals</td></tr><tr><th>past</th><td>wexalled</td></tr></table><img src="img/pic.png"></article>`), Options{
		Headword: "wexal",
		Image: ImageResolverFunc(func(ref, alt string) *entryir.Image {
			return &entryir.Image{ResourceRef: ref, URL: "http://127.0.0.1/resource/T", Alt: alt}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range entry.Sections[0].Blocks {
		if block.Kind != entryir.RichTable {
			continue
		}
		if len(block.Header) != 0 || len(block.Rows) != 2 {
			t.Fatalf("row labels became a header: header=%+v rows=%+v", block.Header, block.Rows)
		}
	}
}

// Deduplication exists for one narrow case: a publisher shipping two language
// views of the same figure, which arrive back to back once CSS is gone.
func TestOnlyImmediatelyRepeatedImagesAreDeduplicated(t *testing.T) {
	resolver := ImageResolverFunc(func(ref, alt string) *entryir.Image {
		return &entryir.Image{ResourceRef: ref, URL: "http://127.0.0.1/resource/" + ref, Alt: alt}
	})

	adjacent, err := Parse([]byte(`<article><p>figure</p><img src="pic.png" alt="English view"><img src="pic.png" alt="Chinese view"><p>tail</p></article>`),
		Options{Headword: "wexal", Image: resolver})
	if err != nil {
		t.Fatal(err)
	}
	if got := countImages(adjacent.Sections[0].Blocks); got != 1 {
		t.Errorf("adjacent duplicate variants should collapse to one, got %d", got)
	}

	// The same resource used twice with real content between the two uses is
	// the dictionary illustrating two places. Deleting the second would remove
	// content the source deliberately put there.
	separated, err := Parse([]byte(`<article><p>first sense</p><img src="pic.png"><p>second sense with its own explanation</p><img src="pic.png"><p>tail</p></article>`),
		Options{Headword: "wexal", Image: resolver})
	if err != nil {
		t.Fatal(err)
	}
	if got := countImages(separated.Sections[0].Blocks); got != 2 {
		t.Errorf("separated occurrences of one image should both survive, got %d", got)
	}

	// Different resources next to each other are simply two illustrations.
	distinct, err := Parse([]byte(`<article><p>figures</p><img src="a.png"><img src="b.png"><p>tail</p></article>`),
		Options{Headword: "wexal", Image: resolver})
	if err != nil {
		t.Fatal(err)
	}
	if got := countImages(distinct.Sections[0].Blocks); got != 2 {
		t.Errorf("distinct adjacent images should both survive, got %d", got)
	}
}

func countImages(blocks []entryir.RichBlock) int {
	total := 0
	for _, block := range blocks {
		if block.Kind == entryir.RichImage {
			total++
		}
	}
	return total
}

func TestMissingImageDoesNotProduceBrokenRichBlock(t *testing.T) {
	entry, err := Parse([]byte(`<article><p>before</p><img src="missing.png"><p>after</p></article>`), Options{
		Headword: "wexal", Image: ImageResolverFunc(func(string, string) *entryir.Image { return nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Sections) != 1 || len(entry.Sections[0].Blocks) != 2 || entry.Sections[0].Body != "before after" ||
		entry.Sections[0].Blocks[0].Text != "before" || entry.Sections[0].Blocks[1].Text != "after" {
		t.Fatalf("missing image changed prose: %+v", entry.Sections)
	}
}

func TestResolvedImageOnlyEntryRemainsVisible(t *testing.T) {
	entry, err := Parse([]byte(`<article><img src="page.jpg" title="reference page"></article>`), Options{
		Headword: "illustrated page",
		Image: ImageResolverFunc(func(ref, alt string) *entryir.Image {
			return &entryir.Image{ResourceRef: ref, URL: "http://127.0.0.1/resource/TOKEN", Alt: alt}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.IsEmpty() || len(entry.Sections) != 1 || len(entry.Sections[0].Blocks) != 1 {
		t.Fatalf("image-only entry was discarded: %+v", entry)
	}
}
