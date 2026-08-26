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
	if blocks[1].Image.Alt != "invented diagram" || len(blocks[3].Rows) != 2 {
		t.Fatalf("rich content = %+v", blocks)
	}
}

func TestMissingImageDoesNotProduceBrokenRichBlock(t *testing.T) {
	entry, err := Parse([]byte(`<article><p>before</p><img src="missing.png"><p>after</p></article>`), Options{
		Headword: "wexal", Image: ImageResolverFunc(func(string, string) *entryir.Image { return nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Sections) != 1 || len(entry.Sections[0].Blocks) != 0 || entry.Sections[0].Body != "before after" {
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
