package validate

import (
	"sort"
	"strings"
	"unicode"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

// Tier is how much scrutiny a dictionary earns, ordered by what this project
// is actually for.
//
// The corpus contains a hundred dictionaries and they are not equally
// important. A Chinese-English learner's dictionary is the product; a database
// of place names is a file that happens to be in MDX. Weighting review by that
// difference is what keeps a hundred encyclopedia fallbacks from drowning out
// twenty real parsing problems.
type Tier int

const (
	// TierChinese is any dictionary with Chinese on either side.
	TierChinese Tier = iota
	// TierEnglishMono is English-to-English.
	TierEnglishMono
	// TierEnglishOther is English paired with a language other than Chinese.
	TierEnglishOther
	// TierOtherLexical is an ordinary dictionary in some other combination.
	TierOtherLexical
	// TierReference is an encyclopedia, name list or article collection: not
	// primarily a lexicon, and not something to force senses onto.
	TierReference
)

// Label names a tier for a report.
func (t Tier) Label() string {
	switch t {
	case TierChinese:
		return "A · Chinese"
	case TierEnglishMono:
		return "B · English monolingual"
	case TierEnglishOther:
		return "C · English ↔ other"
	case TierOtherLexical:
		return "D · other lexical"
	case TierReference:
		return "E · reference / non-lexical"
	}
	return "unknown"
}

// Short is the compact tier letter.
func (t Tier) Short() string {
	if t < TierChinese || t > TierReference {
		return "?"
	}
	return string(rune('A' + int(t)))
}

// Language is what sampling can determine about a dictionary's languages
// without anyone labelling it by hand.
//
// Everything here is derived from scripts in the sampled records plus the
// dictionary's own title. It decides review priority only. No parsing
// behaviour depends on it, which is the reason a rough answer is acceptable:
// the cost of misclassifying a dictionary is that a human reads it in the
// wrong order, not that it parses differently.
type Language struct {
	Tier Tier `json:"tier"`
	// KeyScript is the writing system the headwords are in.
	KeyScript string `json:"keyScript"`
	// ContentScripts are the scripts found in record text, most common first.
	ContentScripts []string `json:"contentScripts,omitempty"`
	// Chinese reports Chinese text on either side of the dictionary.
	Chinese bool `json:"chinese"`
	// Bilingual reports two scripts, or a title that names two languages.
	Bilingual bool `json:"bilingual"`
	// Lexical reports whether the resource behaves like a lexicon at all.
	Lexical bool `json:"lexical"`
	// ChineseClauses and CJKClauses are the raw counts the Chinese decision
	// was made from, kept so the threshold can be argued with rather than
	// taken on trust.
	ChineseClauses int `json:"chineseClauses,omitempty"`
	CJKClauses     int `json:"cjkClauses,omitempty"`
	// Evidence records, in words, why the tier came out as it did.
	Evidence []string `json:"evidence,omitempty"`
}

// scriptTally counts characters by writing system.
type scriptTally struct {
	Han      int
	Kana     int
	Hangul   int
	Latin    int
	Cyrillic int
	Arabic   int
	Other    int
}

func (t *scriptTally) add(text string) {
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han):
			t.Han++
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			t.Kana++
		case unicode.In(r, unicode.Hangul):
			t.Hangul++
		case unicode.In(r, unicode.Cyrillic):
			t.Cyrillic++
		case unicode.In(r, unicode.Arabic):
			t.Arabic++
		case unicode.IsLetter(r) && r < unicode.MaxASCII:
			t.Latin++
		case unicode.IsLetter(r) && unicode.In(r, unicode.Latin):
			t.Latin++
		case unicode.IsLetter(r):
			t.Other++
		}
	}
}

func (t scriptTally) total() int {
	return t.Han + t.Kana + t.Hangul + t.Latin + t.Cyrillic + t.Arabic + t.Other
}

// dominant names the script holding at least a third of the letters.
func (t scriptTally) dominant() string {
	pairs := []struct {
		name  string
		count int
	}{
		{"han", t.Han}, {"kana", t.Kana}, {"hangul", t.Hangul},
		{"latin", t.Latin}, {"cyrillic", t.Cyrillic}, {"arabic", t.Arabic}, {"other", t.Other},
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].count > pairs[j].count })
	total := t.total()
	if total == 0 || pairs[0].count*3 < total {
		return "mixed"
	}
	return pairs[0].name
}

// present lists every script holding at least a twentieth of the letters,
// which is where a gloss column stops being an occasional loanword.
func (t scriptTally) present() []string {
	total := t.total()
	if total == 0 {
		return nil
	}
	var out []string
	for _, pair := range []struct {
		name  string
		count int
	}{
		{"han", t.Han}, {"kana", t.Kana}, {"hangul", t.Hangul},
		{"latin", t.Latin}, {"cyrillic", t.Cyrillic}, {"arabic", t.Arabic}, {"other", t.Other},
	} {
		if pair.count*20 >= total {
			out = append(out, pair.name)
		}
	}
	return out
}

// minChineseSegment is how many Han characters a clause needs before its
// absence of kana means anything.
const minChineseSegment = 4

// clauseBreaks are where one language's run of text ends and the next begins.
// Splitting there is what separates a Chinese gloss from the Japanese text it
// glosses; measured over a whole record the two are indistinguishable.
const clauseBreaks = "\n。．.；;！!？?、,，:：（）()「」『』【】〔〕[]/／·・…—–\t "

// countChineseClauses reports how many clauses are Han-only and how many
// contain CJK at all.
//
// Japanese prose is written with kana between its kanji, and Korean with
// hangul; a clause of four or more Han characters with neither is Chinese.
// This is the one language distinction the product actually needs, and it is
// made from character ranges rather than from any word list.
func countChineseClauses(text string) (chinese, cjk int) {
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune(clauseBreaks, r)
	}) {
		var tally scriptTally
		tally.add(clause)
		if tally.Han+tally.Kana+tally.Hangul == 0 {
			continue
		}
		cjk++
		if tally.Han >= minChineseSegment && tally.Kana == 0 && tally.Hangul == 0 {
			chinese++
		}
	}
	return chinese, cjk
}

// chineseTitleHints are the ways a dictionary announces Chinese in its own
// name. They corroborate the script evidence and never override it; a title is
// metadata a repacker can rewrite, which is why it is never trusted alone.
// The hints are whole words on purpose. A single "漢" also appears in 漢字,
// which is how a Japanese dictionary of personal names ends up filed as a
// Chinese one.
var chineseTitleHints = []string{
	"英汉", "汉英", "英漢", "漢英", "汉语", "漢語", "中文", "中国", "中國",
	"中华", "中華", "中日", "日汉", "日漢", "汉日", "双解", "雙解", "汉德", "汉法",
	"华语", "普通话", "chinese", "hanyu", "cn)", "-cn", "zh-", "en-cn", "cn-en",
}

// otherLanguageTitleHints name a second language that is not Chinese.
// Latin-script language pairs are invisible to script analysis — English and
// French use the same alphabet — so for those the title is the only evidence
// available without building language identification this round does not need.
var otherLanguageTitleHints = []string{
	"french", "français", "francais", "german", "deutsch", "italian", "italiano",
	"spanish", "español", "espanol", "portuguese", "russian", "русск",
	"japanese", "korean", "malay", "melayu", "arabic", "dutch", "swedish",
	"latin-english", "english-latin", "日本語", "한국", "bahasa",
}

// referenceTitleHints mark a resource that is not a lexicon. They only apply
// once the structural evidence already says the same thing.
var referenceTitleHints = []string{
	"encyclop", "百科", "wikipedia", "维基", "維基", "who's who", "biograph",
	"gazetteer", "地名", "人名", "名鉴", "名鑑", "yearbook", "almanac",
}

// languageOf classifies a dictionary from its samples and its title.
func languageOf(title string, samples []diagnose.Sample, structure structureEvidence) Language {
	lang := Language{Lexical: true}
	var keys, content scriptTally
	chineseClauses, cjkClauses := 0, 0
	var keyLengths []int
	multiword := 0

	for _, sample := range samples {
		keys.add(sample.Key)
		keyLengths = append(keyLengths, len([]rune(sample.Key)))
		if strings.ContainsAny(sample.Key, " 　") {
			multiword++
		}
		text := parser.Normalize(parser.Text(sample.Doc, parser.TextOptions{SkipHidden: true}))
		content.add(text)
		c, total := countChineseClauses(text)
		chineseClauses += c
		cjkClauses += total
	}

	lang.KeyScript = keys.dominant()
	lang.ContentScripts = content.present()
	lang.ChineseClauses, lang.CJKClauses = chineseClauses, cjkClauses
	lowerTitle := strings.ToLower(title)

	// Chinese on either side.
	//
	// The strongest evidence is negative: Han characters with no kana and no
	// hangul anywhere in the sampled records cannot be Japanese or Korean, and
	// it catches the two-character glosses a clause test is too coarse for.
	// The clause test then covers dictionaries that mix scripts — a
	// Chinese-Japanese dictionary has Japanese on one side and Chinese on the
	// other — and the title is corroboration for the rest.
	hanOnlyCJK := content.Han > 0 && content.Kana == 0 && content.Hangul == 0
	keyChinese := keys.Han >= minChineseSegment && keys.Kana == 0 && keys.Hangul == 0
	contentChinese := chineseClauses >= 4 && chineseClauses*3 >= cjkClauses
	titleChinese := containsAny(lowerTitle, chineseTitleHints)
	switch {
	case hanOnlyCJK:
		lang.Chinese = true
		lang.Evidence = append(lang.Evidence, "Han text with no kana or hangul anywhere")
	case contentChinese:
		lang.Chinese = true
		lang.Evidence = append(lang.Evidence, "Han-only clauses in record text")
	case keyChinese && cjkClauses == 0:
		lang.Chinese = true
		lang.Evidence = append(lang.Evidence, "Han-only headwords")
	case titleChinese && (keys.Han > 0 || content.Han > 0):
		lang.Chinese = true
		lang.Evidence = append(lang.Evidence, "title names Chinese and Han text is present")
	}

	scripts := len(lang.ContentScripts)
	lang.Bilingual = scripts > 1 || containsAny(lowerTitle, otherLanguageTitleHints)

	// Lexicality. A resource that yields parts of speech or transcriptions is
	// a lexicon whatever else it looks like, so those veto the demotion
	// outright rather than merely counting against it.
	//
	// A resource that yields parts of speech or transcriptions is a lexicon
	// whatever else it looks like, so both veto the demotion outright. What
	// they do not veto is a title that says "encyclopedia": numbered sections
	// are as normal in an encyclopedia article as they are in a sense list, so
	// numbering cannot rescue one.
	medianKey := median(keyLengths)
	longKeys := medianKey > 20
	mostlyMultiword := len(samples) > 0 && multiword*2 > len(samples)
	hugeRecords := structure.MedianBytes > 6000
	referenceTitle := containsAny(lowerTitle, referenceTitleHints)
	if !structure.HasPOS && !structure.HasIPA {
		switch {
		case referenceTitle:
			lang.Lexical = false
			lang.Evidence = append(lang.Evidence, "no POS or IPA, and the title names a reference work")
		case !structure.HasSenseNumbers && hugeRecords:
			lang.Lexical = false
			lang.Evidence = append(lang.Evidence, "no POS, IPA or numbering, and records are article-sized")
		case !structure.HasSenseNumbers && longKeys && mostlyMultiword:
			// Long *and* multi-word: an idiom dictionary has multi-word
			// headwords too, but they are short ones.
			lang.Lexical = false
			lang.Evidence = append(lang.Evidence, "no POS, IPA or numbering, and headwords are long phrases")
		}
	}

	latinContent := content.Latin*3 >= content.total()
	switch {
	case !lang.Lexical:
		lang.Tier = TierReference
	case lang.Chinese:
		lang.Tier = TierChinese
	case latinContent && scripts <= 1 && !containsAny(lowerTitle, otherLanguageTitleHints):
		lang.Tier = TierEnglishMono
	case latinContent || containsAny(lowerTitle, otherLanguageTitleHints):
		lang.Tier = TierEnglishOther
	default:
		lang.Tier = TierOtherLexical
	}
	return lang
}

// structureEvidence is the handful of lexicon signals the tier decision needs.
type structureEvidence struct {
	HasPOS          bool
	HasIPA          bool
	HasSenseNumbers bool
	MedianBytes     int
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

func median(values []int) int {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int(nil), values...)
	sort.Ints(ordered)
	return ordered[len(ordered)/2]
}
