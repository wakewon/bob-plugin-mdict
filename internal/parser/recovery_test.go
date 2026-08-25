package parser

import (
	"strings"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// Every case here was found by running the parser over a real corpus and
// reading what it produced. The dictionaries themselves stay out of the
// repository: each fixture is the smallest invented DOM that reproduces the
// behaviour, with invented words and invented meanings.

func parse(t *testing.T, headword, markup string) *entryir.Entry {
	t.Helper()
	entry, err := Parse([]byte(markup), Options{Headword: headword, MaxExamplesPerSense: 8})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return entry
}

func senseTexts(entry *entryir.Entry) []string {
	var out []string
	for _, part := range entry.Parts {
		var walk func([]entryir.Sense)
		walk = func(senses []entryir.Sense) {
			for _, sense := range senses {
				out = append(out, sense.Definition)
				walk(sense.Subsenses)
			}
		}
		walk(part.Senses)
	}
	return out
}

// An encyclopedia opens with a numbered table of contents, and it ascends as
// convincingly as any sense list. What it is made of is links.
func TestNumberedTableOfContentsIsNotASenseList(t *testing.T) {
	body := strings.Repeat("Invented prose about the invented subject, at length. ", 60)
	entry := parse(t, "Wexal Theory", `<div>
		<h1>Wexal Theory</h1>
		<div id="toc"><ul>
			<li><a href="#one">1. Origins</a></li>
			<li><a href="#two">2. Development</a></li>
			<li><a href="#three">3. Criticism</a></li>
		</ul></div>
		<div id="body"><p>`+body+`</p></div>
	</div>`)
	if len(entry.Parts) != 0 {
		t.Errorf("a table of contents was read as senses: %v", senseTexts(entry))
	}
	if len(entry.Sections) != 1 || entry.Sections[0].Title != FallbackSectionTitle {
		t.Errorf("the article should survive as untyped content, got %+v", entry.Sections)
	}
}

// The same guard, without links: a numbering that accounts for a fortieth of
// the record is an index of it, not the meanings in it.
func TestNumberingThatCoversNothingIsNotASenseList(t *testing.T) {
	body := strings.Repeat("Invented prose about the invented subject, at length. ", 60)
	entry := parse(t, "wexal", `<div>
		<p><span>1. Origins</span></p>
		<p><span>2. Development</span></p>
		<p><span>3. Criticism</span></p>
		<div>`+body+`</div>
	</div>`)
	if len(entry.Parts) != 0 {
		t.Errorf("an index was read as senses: %v", senseTexts(entry))
	}
}

// Markup with unclosed tags parses into a chain where each sense element
// contains the whole rest of the entry. Reading that as a sense list emits the
// entry once per sense.
func TestNestedSenseElementsAreNotASenseList(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<div class="s"><span class="n">1</span>a small hook used to fasten a sail
			<div class="s"><span class="n">2</span>the act of fastening such a hook
				<div class="s"><span class="n">3</span>a fee paid for mooring</div>
			</div>
		</div>
	</div>`)
	for _, definition := range senseTexts(entry) {
		if strings.Count(definition, "fastening") > 0 && strings.Count(definition, "mooring") > 0 {
			t.Errorf("a sense swallowed its siblings: %q", definition)
		}
	}
}

// A dictionary that prints the number and the definition in one element and
// the examples in the elements after it. Nothing owns the examples, so without
// the boundary rule nothing reads them.
func TestExamplesBetweenTwoNumbersBelongToTheFirst(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<span class="d"><span class="n">1.</span>a small hook used to fasten a sail 系帆小钩:</span>
		<span class="x"><span class="e">She reached for the wexal.</span><span class="c">她伸手去拿小钩。</span></span>
		<span class="x"><span class="e">The wexal held fast.</span><span class="c">小钩牢牢固定住了。</span></span>
		<span class="d"><span class="n">2.</span>the act of fastening such a hook 系钩:</span>
		<span class="x"><span class="e">A careful wexal takes practice.</span><span class="c">仔细系钩需要练习。</span></span>
	</div>`)
	if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 2 {
		t.Fatalf("expected two senses, got %+v", entry.Parts)
	}
	first := entry.Parts[0].Senses[0]
	if len(first.Examples) != 2 {
		t.Fatalf("expected the first sense to own two examples, got %+v", first.Examples)
	}
	if first.Examples[0].Translation != "她伸手去拿小钩。" {
		t.Errorf("example translation not separated: %+v", first.Examples[0])
	}
	if !strings.Contains(first.Definition, "small hook") || strings.Contains(first.Definition, "reached") {
		t.Errorf("definition absorbed its examples: %q", first.Definition)
	}
	if first.Translation != "系帆小钩:" && first.Translation != "系帆小钩：" && !strings.HasPrefix(first.Translation, "系帆小钩") {
		t.Errorf("gloss not lifted out of the definition: %q", first.Translation)
	}
}

// In a bilingual dictionary the examples are the elements carrying both
// languages. Grammar codes printed among them carry only one.
func TestSingleLanguageSiblingsAreNotExamples(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<span class="d"><span class="n">1.</span>a small hook used to fasten a sail 系帆小钩:</span>
		<span class="x"><span class="e">She reached for the wexal.</span><span class="c">她伸手去拿小钩。</span></span>
		<span class="g">[G] n+prep</span>
		<span class="d"><span class="n">2.</span>the act of fastening such a hook 系钩:</span>
		<span class="x"><span class="e">A careful wexal takes practice.</span><span class="c">仔细系钩需要练习。</span></span>
		<span class="g">[G] n+adv</span>
	</div>`)
	for _, part := range entry.Parts {
		for _, sense := range part.Senses {
			for _, example := range sense.Examples {
				if strings.Contains(example.Text, "[G]") {
					t.Errorf("a grammar code was read as an example: %q", example.Text)
				}
			}
		}
	}
}

// After the last sense there is no next sense to stop at, so the run reaches
// the end of the record and takes the sections printed after the sense list
// with it.
func TestTheLastSenseDoesNotSwallowWhatFollowsTheSenseList(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<span class="d"><span class="n">1.</span>a small hook used to fasten a sail</span>
		<span class="d"><span class="n">2.</span>the act of fastening such a hook</span>
		<span class="deriv">Derivatives</span>
		<span class="deriv">wexalage noun the fee paid for mooring</span>
	</div>`)
	for _, part := range entry.Parts {
		for _, sense := range part.Senses {
			for _, example := range sense.Examples {
				if strings.Contains(example.Text, "wexalage") {
					t.Errorf("the last sense swallowed a following section: %q", example.Text)
				}
			}
		}
	}
}

// One thesaurus opens each article with its own numbered list of senses and
// then prints the article. The division is right until the last number, which
// takes everything left.
func TestABlockTooLargeToBeASenseBecomesUntypedContent(t *testing.T) {
	body := strings.Repeat("Invented explanatory prose that goes on and on. ", 90)
	entry := parse(t, "wexal", `<div class="entry">
		<p><span class="n">1.</span> a small hook used to fasten a sail</p>
		<p><span class="n">2.</span> the act of fastening such a hook</p>
		<p><span class="n">3.</span> `+body+`</p>
	</div>`)
	if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 2 {
		t.Fatalf("expected the two real senses, got %+v", senseTexts(entry))
	}
	if len(entry.Sections) != 1 || entry.Sections[0].Title != FallbackSectionTitle {
		t.Fatalf("the oversized block should survive as untyped content, got %+v", entry.Sections)
	}
	if !strings.Contains(entry.Sections[0].Body, "Invented explanatory prose") {
		t.Errorf("the oversized block was not preserved: %q", entry.Sections[0].Body)
	}
}

// "entry_title" is used both for the headword and for a page banner, and
// "headword" both for the word and for the block holding the word, its word
// classes and its pronunciation.
func TestHeadwordClaimIsCheckedAgainstTheKey(t *testing.T) {
	banner := parse(t, "wexal", `<div>
		<h1 class="entry_title">Definition of 'wexal'</h1>
		<div class="body"><span class="orth">wexal</span>
		<div class="sense"><span class="def">a small hook used to fasten a sail</span></div></div>
	</div>`)
	if banner.Headword != "wexal" {
		t.Errorf("headword = %q, want the word rather than the banner", banner.Headword)
	}

	container := parse(t, "wexal", `<div>
		<div class="headword"><a>noun</a><a>verb</a><h2>wexal</h2><span class="pron">/ˈwɛksl/</span></div>
		<div class="sense"><span class="def">a small hook used to fasten a sail</span></div>
	</div>`)
	if container.Headword != "wexal" {
		t.Errorf("headword = %q, want the word rather than its container", container.Headword)
	}
}

// A sense with exactly one subsense is common. Discarding it left the subsense
// glued to the end of its parent's definition, with both glosses run together.
func TestASingleSubsenseIsKept(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<div class="item"><strong class="n">1</strong>
			<div class="d">a small hook used to fasten a sail</div>
			<div class="sub"><span class="sn">1.1</span><div class="d">the hook and its fitting together</div></div>
		</div>
		<div class="item"><strong class="n">2</strong>
			<div class="d">the act of fastening such a hook</div>
		</div>
	</div>`)
	if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 2 {
		t.Fatalf("expected two senses, got %+v", senseTexts(entry))
	}
	first := entry.Parts[0].Senses[0]
	if len(first.Subsenses) != 1 {
		t.Fatalf("expected one subsense, got %+v", first.Subsenses)
	}
	if strings.Contains(first.Definition, "fitting") {
		t.Errorf("the subsense stayed inside its parent's definition: %q", first.Definition)
	}
}

// A nested block that merely repeats its parent is a wrapper, and keeping it
// prints the same meaning twice.
func TestASubsenseThatRepeatsItsParentIsDropped(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<div class="item"><strong class="n">1</strong>
			<div class="sub"><span class="sn">1</span><div class="d">a small hook used to fasten a sail</div></div>
		</div>
		<div class="item"><strong class="n">2</strong>
			<div class="sub"><span class="sn">2</span><div class="d">the act of fastening such a hook</div></div>
		</div>
	</div>`)
	for _, sense := range entry.Parts[0].Senses {
		if len(sense.Subsenses) != 0 {
			t.Errorf("a wrapper was kept as a subsense: %+v", sense.Subsenses)
		}
	}
}

// A gloss fused into the definition's own text node, with no element boundary
// anywhere for the node-level split to hold on to.
func TestFusedGlossIsSeparated(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<div class="sense"><span class="def">a small hook used to fasten a sail 系帆小钩</span></div>
		<div class="sense"><span class="def">the act of fastening such a hook 系钩</span></div>
	</div>`)
	senses := entry.Parts[0].Senses
	if senses[0].Definition != "a small hook used to fasten a sail" || senses[0].Translation != "系帆小钩" {
		t.Errorf("gloss not separated: %q / %q", senses[0].Definition, senses[0].Translation)
	}
}

// Text that changes script more than once is quoting, not glossing, and any
// seam chosen in it would cut a sentence in half.
func TestInterleavedScriptsAreNotSplit(t *testing.T) {
	entry := parse(t, "wexal", `<div class="entry">
		<div class="sense"><span class="def">a small 小 hook used to fasten a sail 帆</span></div>
		<div class="sense"><span class="def">the act 行为 of fastening such a hook 钩</span></div>
	</div>`)
	for _, sense := range entry.Parts[0].Senses {
		if sense.Translation != "" {
			t.Errorf("interleaved text was split anyway: %q / %q", sense.Definition, sense.Translation)
		}
	}
}

// Four Latin letters beside a sixty-character CJK definition are a romanized
// reading, not a translation of it.
func TestARomanizedReadingIsNotAGloss(t *testing.T) {
	entry := parse(t, "和谐", `<div class="entry">
		<div class="sense"><span class="def">（音調・色彩・形状などが）調和が取れている，釣り合っている，融合している． tiáo</span></div>
		<div class="sense"><span class="def">仲むつまじい，和やかである．おだやかで角が立たない様子． xié</span></div>
	</div>`)
	for _, sense := range entry.Parts[0].Senses {
		if sense.Translation != "" {
			t.Errorf("a reading was lifted out as a gloss: %q", sense.Translation)
		}
	}
}

// The other direction: a Chinese-keyed entry whose gloss is English, and so
// runs longer than the source rather than shorter. The proportion test has to
// allow for that without also admitting a four-letter reading.
func TestAnEnglishGlossOfAChineseHeadwordIsKept(t *testing.T) {
	entry := parse(t, "企业", `<div class="entry">
		<div class="sense"><span class="def">从事生产或服务活动的经济组织 enterprise; business undertaking</span></div>
		<div class="sense"><span class="def">泛指一项经营性的事务 venture; commercial project</span></div>
	</div>`)
	senses := entry.Parts[0].Senses
	if senses[0].Translation != "enterprise; business undertaking" {
		t.Errorf("gloss = %q, want the English side", senses[0].Translation)
	}
	if senses[0].Definition != "从事生产或服务活动的经济组织" {
		t.Errorf("definition = %q, want the Chinese side alone", senses[0].Definition)
	}
}

// A repack of a profiled dictionary reuses its structure while renaming the
// one class that marks the gloss. The profile still fingerprints, so it is
// still selected, and every definition then carries its translation glued to
// the end of it.
func TestAProfileWhoseGlossSelectorMissesFallsBackToScript(t *testing.T) {
	profile := &Profile{
		ID:          "invented-learner",
		Sense:       []string{".sense"},
		Definition:  []string{".def"},
		Translation: []string{".chn"},
	}
	original := `<div class="entry">
		<div class="sense"><span class="def">a small hook used to fasten a sail<span class="chn">系帆小钩</span></span></div>
	</div>`
	repack := `<div class="entry">
		<div class="sense"><span class="def">a small hook used to fasten a sail 系帆小钩</span></div>
	</div>`

	for name, markup := range map[string]string{"original": original, "repack": repack} {
		entry, err := Parse([]byte(markup), Options{Headword: "wexal", Profile: profile, MaxExamplesPerSense: 4})
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if len(entry.Parts) != 1 || len(entry.Parts[0].Senses) != 1 {
			t.Fatalf("%s: expected one sense, got %+v", name, entry.Parts)
		}
		sense := entry.Parts[0].Senses[0]
		if sense.Definition != "a small hook used to fasten a sail" || sense.Translation != "系帆小钩" {
			t.Errorf("%s: definition %q, translation %q", name, sense.Definition, sense.Translation)
		}
	}
}

// The profile still wins wherever it matches: an explicit gloss element is a
// statement about the markup, and script evidence is an inference from it.
func TestAProfileGlossSelectorStillWins(t *testing.T) {
	profile := &Profile{
		ID:          "invented-learner",
		Sense:       []string{".sense"},
		Definition:  []string{".def"},
		Translation: []string{".chn"},
	}
	entry, err := Parse([]byte(`<div class="entry">
		<div class="sense"><span class="def">to fasten 系住 something<span class="chn">系帆小钩</span></span></div>
	</div>`), Options{Headword: "wexal", Profile: profile, MaxExamplesPerSense: 4})
	if err != nil {
		t.Fatal(err)
	}
	sense := entry.Parts[0].Senses[0]
	if sense.Translation != "系帆小钩" {
		t.Errorf("translation = %q, want the element the profile named", sense.Translation)
	}
}
