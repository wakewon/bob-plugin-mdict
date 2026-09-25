package htmlmd_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/htmlmd"
	"golang.org/x/net/html"
)

// Every fixture here is invented. The converter is checked against the shapes
// dictionary pages take — layout in the stylesheet, MDict addressing, hidden
// language variants — not against any publisher's content.

type fakeResources struct {
	sheets map[string]string
}

func (f fakeResources) Stylesheet(href string) *htmlmd.Stylesheet {
	if css, ok := f.sheets[href]; ok {
		return htmlmd.ParseStylesheet([]byte(css))
	}
	return nil
}

func (fakeResources) Audio(ref string) string {
	if strings.Contains(ref, "missing") {
		return ""
	}
	return "http://127.0.0.1:15321/v2/resource/AUDIO"
}

func (fakeResources) Image(ref string) string {
	if strings.HasPrefix(ref, "http") || strings.Contains(ref, "missing") {
		return ""
	}
	return "http://127.0.0.1:15321/v2/resource/IMAGE"
}

func convert(t *testing.T, css, body string) string {
	t.Helper()
	page := `<link rel="stylesheet" href="d.css">` + body
	return htmlmd.Convert([]byte(page), htmlmd.Options{
		Resources: fakeResources{sheets: map[string]string{"d.css": css}},
	})
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestStylesheetDisplayDecidesLayout(t *testing.T) {
	got := convert(t,
		`.sense{display:block} .zh{display:none}`,
		`<span class="hw">wexal</span><span class="sense">1. a small hook<span class="zh">钩</span></span><span class="sense">2. a fastening</span>`)
	expect(t, got, "wexal\n\n1\\. a small hook\n\n2\\. a fastening")
}

func TestInlineDisplayJoinsBlocks(t *testing.T) {
	got := convert(t, `.pos{display:inline}`, `<div>wexal <div class="pos">noun</div></div>`)
	expect(t, got, "wexal noun")
}

func TestCascadeOrder(t *testing.T) {
	cases := []struct {
		name, css, body, want string
	}{
		{"specificity beats order", `span.a{display:none} .a{display:inline}`, `<p>x<span class="a">gone</span></p>`, "x"},
		{"later rule wins a tie", `.a{display:none} .a{display:inline}`, `<p>x <span class="a">kept</span></p>`, "x kept"},
		{"important beats specificity", `.a{display:none!important} #b.a{display:inline}`, `<p>x<span id="b" class="a">gone</span></p>`, "x"},
		{"inline style beats rules", `.a{display:none}`, `<p>x <span class="a" style="display:inline">kept</span></p>`, "x kept"},
		{"sibling selector", `.n+.d{display:none}`, `<p><span class="n">1</span><span class="d">gone</span></p>`, "1"},
		{"conditional media is skipped", `@media (max-width:10px){.a{display:none}} @media screen{.b{display:none}}`, `<p><span class="a">kept</span><span class="b">gone</span></p>`, "kept"},
		{"hover state is skipped", `.a:hover{display:none}`, `<p><span class="a">kept</span></p>`, "kept"},
		{"unsupported selector costs only itself", `.a::-webkit-scrollbar, .b{display:none}`, `<p><span class="a">kept</span><span class="b">gone</span></p>`, "kept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expect(t, convert(t, tc.css, tc.body), tc.want)
		})
	}
}

func TestGeneratedContent(t *testing.T) {
	got := convert(t,
		`.ipa:before,.ipa:after{content:"/"} .x:before{content:"\2022  "} .toggle:before{content:"TRANS";float:right} .icon:before{content:"\e900"}`,
		`<div><span class="toggle"></span><span class="icon"></span><span class="ipa"> ˈwɛk<u>sl</u> </span></div><div class="x">tie the rope</div>`)
	expect(t, got, "/ˈwɛksl/\n\n• tie the rope")
}

func TestResourcesAndLinks(t *testing.T) {
	got := convert(t, ``,
		`<p><a href="sound://uk/wexal.mp3"><span>ˈwɛksl</span><img src="speaker.png"></a>; <a href="sound://missing.mp3">us</a></p>`+
			`<p>see <a href="entry://belay">belay</a> and <a href="#top">top</a></p>`+
			`<p><img src="fig.png" alt="hook"><img src="https://example.invalid/x.png"><img src="missing.png"></p>`)
	expect(t, got, "ˈwɛksl [🔊 UK](http://127.0.0.1:15321/v2/resource/AUDIO) ; us\n\n"+
		"see belay and top\n\n"+
		"![hook](http://127.0.0.1:15321/v2/resource/IMAGE)")
}

func TestSpeakerOnlyLinkBecomesSpacedIcon(t *testing.T) {
	got := convert(t, ``, `<div><a href="sound://ex/1.mp3">&nbsp;</a><span>An example.</span></div>`)
	expect(t, got, "[🔊](http://127.0.0.1:15321/v2/resource/AUDIO) An example.")
}

func TestHiddenElementsAndWordBoundaries(t *testing.T) {
	got := convert(t,
		`.hyp,.label{display:none}`,
		`<p><span class="hw">wex<span class="hyp">·</span>al</span></p>`+
			`<p><span class="phrase">make fast</span><span class="label">SAILING</span><span class="def">to tie</span></p>`+
			`<p><span>钩子</span><span class="label">X</span><span>绳</span></p>`)
	expect(t, got, "wexal\n\nmake fast to tie\n\n钩子绳")
}

func TestMarginsSeparateLabels(t *testing.T) {
	got := convert(t, `.pos{margin-left:8px}`, `<p><b>noun</b><span class="pos">verb</span></p>`)
	expect(t, got, "**noun** verb")
}

func TestAdjacentElementsDoNotMergeWords(t *testing.T) {
	got := convert(t, ``, `<p><span>noun</span><span>Plural</span> wexals</p><p><span>wexal</span><sup>2</sup> <span>hel</span>lo</p>`)
	expect(t, got, "noun Plural wexals\n\nwexal2 hello")
}

func TestStylesheetEmphasis(t *testing.T) {
	got := convert(t,
		`.hw,.ref{font-weight:bold} .lbl{font-style:italic} .sense{display:block;font-weight:700}`,
		`<p><span class="hw">wexal</span> <span class="lbl">nautical</span></p>`+
			`<p>wexal<span class="ref">, → belay</span></p>`+
			`<div class="sense"><div>block content is not wrapped</div></div>`)
	expect(t, got, "**wexal** *nautical*\n\nwexal, → **belay**\n\nblock content is not wrapped")
}

func TestListStyleNoneDropsMarkers(t *testing.T) {
	got := convert(t, `ol{list-style:none}`,
		`<ol><li><i>1</i> a hook</li></ol><ol><li><i>2</i> a fastening</li></ol><ul class="kept"><li>real list</li></ul>`)
	expect(t, got, "*1* a hook\n\n*2* a fastening\n\n- real list")
}

func TestLayoutTablesAreFlattened(t *testing.T) {
	got := convert(t, ``,
		`<table><tr><td><b>1a</b></td><td>to fasten</td><td></td></tr><tr><td></td><td>an example</td></tr></table>`+
			`<table><tr><th>form</th><th>word</th></tr><tr><td>plural</td><td>wexals</td></tr></table>`)
	expect(t, got, "**1a** to fasten\n\nan example\n\n| form | word |\n|---|---|\n| plural | wexals |")
}

func TestLineBreaksInsideParagraphsSurvive(t *testing.T) {
	got := convert(t, ``, `<p>訳 钩<br>ピンイン gōu<br></p><p>next</p>`)
	expect(t, got, "訳 钩  \nピンイン gōu\n\nnext")
}

func TestScopeAndExtraStylesheets(t *testing.T) {
	page := `<div class="us">American edition</div><div class="uk">British edition<span class="tab">Examples</span></div>`
	got := htmlmd.Convert([]byte(page), htmlmd.Options{
		Stylesheets: []*htmlmd.Stylesheet{htmlmd.ParseStylesheet([]byte(`.tab{display:none}`))},
		Scope: func(doc *html.Node) *html.Node {
			var found *html.Node
			var visit func(*html.Node)
			visit = func(node *html.Node) {
				for _, a := range node.Attr {
					if a.Key == "class" && a.Val == "uk" && found == nil {
						found = node
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					visit(child)
				}
			}
			visit(doc)
			return found
		},
	})
	expect(t, got, "British edition")
}

func TestUnparseableInputStillReadable(t *testing.T) {
	got := htmlmd.Convert([]byte(`<div><span>unclosed <b>bold`), htmlmd.Options{})
	expect(t, got, "unclosed **bold**")
}

func records() []htmlmd.Record {
	return []htmlmd.Record{
		{Ordinal: 1, HTML: []byte(`<p>wexal<sup>1</sup> noun</p>`)},
		{Ordinal: 2, HTML: []byte(`<p>wexal<sup>2</sup> verb</p>`)},
	}
}

func TestRenderRecordsCombined(t *testing.T) {
	got := htmlmd.RenderRecords(records(), htmlmd.RecordOptions{Key: "wexal", MultiRecordMode: htmlmd.MultiRecordCombined})
	expect(t, got, "## Record 1 of 2\n\nwexal1 noun\n\n---\n\n## Record 2 of 2\n\nwexal2 verb\n")
}

func TestRenderRecordsSeparate(t *testing.T) {
	got := htmlmd.RenderRecords(records(), htmlmd.RecordOptions{Key: "wexal", MultiRecordMode: htmlmd.MultiRecordSeparate})
	expect(t, got, "wexal1 noun\n\n## Other entries\n\n- `wexal²`\n")

	got = htmlmd.RenderRecords(records(), htmlmd.RecordOptions{Key: "wexal", MultiRecordMode: htmlmd.MultiRecordCombined, RecordOrdinal: 2})
	expect(t, got, "wexal2 verb\n\n## Other entries\n\n- `wexal¹`\n")
}

func TestRenderRecordsSingleRecordHasNoFrame(t *testing.T) {
	one := records()[:1]
	for _, mode := range []htmlmd.MultiRecordMode{htmlmd.MultiRecordCombined, htmlmd.MultiRecordSeparate} {
		expect(t, htmlmd.RenderRecords(one, htmlmd.RecordOptions{Key: "wexal", MultiRecordMode: mode}), "wexal1 noun\n")
	}
}

func TestStylesheetLinks(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<link rel="stylesheet" href="a.css"><link rel="icon" href="i.png"><LINK REL="Stylesheet" href="b.css">`))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	htmlmd.StylesheetLinks(doc, func(href string) { got = append(got, href) })
	expect(t, strings.Join(got, ","), "a.css,b.css")
}

// fakeLookup encodes like the real helper link does, so the tests also show
// that the converter does not encode a link a second time.
func fakeLookup(query string) string {
	if query == "" {
		return ""
	}
	return "bobmdict://lookup?text=" + strings.ReplaceAll(url.QueryEscape(query), "+", "%20")
}

func TestEntryLinksBecomeLookupsWhenAvailable(t *testing.T) {
	page := `<p>see <a href="entry://cut%20and%20run#s1">cut and run</a><a href="bword://belay"><span>, </span>belay</a>, ` +
		`<a href="entry://#top">top</a> and <a href="entry://wexal"><img src="missing.png"></a></p>`
	got := htmlmd.Convert([]byte(page), htmlmd.Options{Resources: fakeResources{}, LookupLink: fakeLookup})
	expect(t, got, "see [cut and run](bobmdict://lookup?text=cut%20and%20run), [belay](bobmdict://lookup?text=belay), top and")

	got = htmlmd.Convert([]byte(page), htmlmd.Options{Resources: fakeResources{}})
	expect(t, got, "see cut and run, belay, top and")
}

func TestSiblingSelectorsBecomeLookupsWhenAvailable(t *testing.T) {
	got := htmlmd.RenderRecords(records(), htmlmd.RecordOptions{
		Options: htmlmd.Options{LookupLink: fakeLookup},
		Key:     "wexal", MultiRecordMode: htmlmd.MultiRecordSeparate,
	})
	expect(t, got, "wexal1 noun\n\n## Other entries\n\n- [wexal²](bobmdict://lookup?text=wexal%C2%B2)\n")
}

func TestAudioLinksCarryTheirOwnRegion(t *testing.T) {
	got := convert(t, `.pron > a{display:block} .ipa:before,.ipa:after{content:"/"}`,
		`<div class="pron">`+
			`<a href="sound://c/en_gb_wexal.mp3"><span class="ipa">ˈwɛksl</span><span class="icon-speak-uk"></span></a>`+
			`<a href="sound://c/en_us_wexal.mp3"><span class="ipa">ˈwɛksəl</span></a>`+
			`<a href="sound://c/12345.mp3"><span class="ipa">wɛks</span></a>`+
			`</div><p>An example.<a class="speaker exafile" href="sound://ex/p1.mp3"> </a></p>`)
	expect(t, got, "/ˈwɛksl/ [🔊 UK](http://127.0.0.1:15321/v2/resource/AUDIO)\n\n"+
		"/ˈwɛksəl/ [🔊 US](http://127.0.0.1:15321/v2/resource/AUDIO)\n\n"+
		"/wɛks/ [🔊](http://127.0.0.1:15321/v2/resource/AUDIO)\n\n"+
		"An example. [🔊](http://127.0.0.1:15321/v2/resource/AUDIO)")
}

func TestIconFontGlyphsInTextAreDropped(t *testing.T) {
	got := convert(t, ``, "<p><span>/ˈwɛksl/</span> <span class=\"icon\">\uea27</span> <a href=\"sound://uk/w.mp3\"></a></p>")
	expect(t, got, "/ˈwɛksl/ [🔊 UK](http://127.0.0.1:15321/v2/resource/AUDIO)")
}

// visibility is inherited and can be set back: a visible child of a hidden
// element is shown, and a hidden label between words still separates them.
func TestVisibilityIsInherited(t *testing.T) {
	got := convert(t,
		`.panel{visibility:hidden} .panel .shown{visibility:visible} .gap{visibility:hidden}`,
		`<div class="panel">toggle <span class="shown">kept definition</span><img src="icon.png"></div>`+
			`<p>abandon ship<span class="gap">ESCAPE</span>to leave</p>`)
	expect(t, got, "kept definition\n\nabandon ship to leave")
}

// Generated text is added to an element that has no children at all.
func TestGeneratedContentInAnEmptyElement(t *testing.T) {
	got := convert(t, `.num:before{content:"1."} .sep:after{content:"; "}`,
		`<p><span class="num"></span> a hook<span class="sep"></span>a fastening</p>`)
	expect(t, got, "1\\. a hook; a fastening")
}
