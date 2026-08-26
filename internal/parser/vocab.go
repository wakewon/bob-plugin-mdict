package parser

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// posCanonical maps the many spellings dictionaries use for a part of speech
// onto one canonical label. Keys are lowercase with punctuation stripped.
var posCanonical = map[string]string{
	"n": "noun", "n.": "noun", "noun": "noun", "nouns": "noun", "名词": "noun",
	"pl": "noun", "npl": "noun", "nounplural": "noun",
	"v": "verb", "v.": "verb", "verb": "verb", "vb": "verb", "动词": "verb",
	"vt": "transitive verb", "vt.": "transitive verb", "transitiveverb": "transitive verb",
	"vi": "intransitive verb", "vi.": "intransitive verb", "intransitiveverb": "intransitive verb",
	"modalverb": "modal verb", "modal": "modal verb", "auxiliary": "auxiliary verb",
	"auxiliaryverb": "auxiliary verb", "auxv": "auxiliary verb",
	"adj": "adjective", "adj.": "adjective", "adjective": "adjective", "形容词": "adjective",
	"adv": "adverb", "adv.": "adverb", "adverb": "adverb", "副词": "adverb",
	"prep": "preposition", "prep.": "preposition", "preposition": "preposition", "介词": "preposition",
	"conj": "conjunction", "conj.": "conjunction", "conjunction": "conjunction", "连词": "conjunction",
	"pron": "pronoun", "pron.": "pronoun", "pronoun": "pronoun", "代词": "pronoun",
	"det": "determiner", "det.": "determiner", "determiner": "determiner", "限定词": "determiner",
	"art": "article", "article": "article", "冠词": "article",
	"int": "interjection", "int.": "interjection", "interj": "interjection",
	"interjection": "interjection", "exclam": "interjection", "exclamation": "interjection",
	"感叹词": "interjection", "叹词": "interjection",
	"num": "number", "number": "number", "numeral": "number", "数词": "number",
	"abbr": "abbreviation", "abbreviation": "abbreviation", "缩写": "abbreviation",
	"prefix": "prefix", "suffix": "suffix", "combiningform": "combining form",
	"symbol": "symbol",
}

// codePrefixes maps learner's-dictionary grammar codes onto part names.
var codePrefixes = map[string]string{
	"N": "noun", "V": "verb", "ADJ": "adjective", "ADV": "adverb",
	"PREP": "preposition", "CONJ": "conjunction", "PRON": "pronoun",
	"DET": "determiner", "QUANT": "determiner", "MODAL": "modal verb",
	"EXCLAM": "interjection", "NUM": "number", "ORD": "number",
	"COMB": "combining form", "PREFIX": "prefix", "SUFFIX": "suffix", "NEG": "adverb",
}

var posCleanRe = regexp.MustCompile(`[\s\.,;:/()\[\]]+`)

// CanonicalPOS normalizes a part-of-speech label. It returns "" when the text
// is not recognisably a part of speech, so callers can fall back rather than
// inventing a category.
func CanonicalPOS(raw string) string {
	text := Normalize(raw)
	if text == "" || len([]rune(text)) > 40 {
		return ""
	}
	key := strings.ToLower(posCleanRe.ReplaceAllString(text, ""))
	if canonical, ok := posCanonical[key]; ok {
		return canonical
	}
	// Learner's dictionaries label senses with grammar codes rather than plain
	// part names: "N-COUNT", "V-ERG", "ADJ-GRADED". The head of the code is the
	// part of speech and the tail is countability or gradability detail.
	upper := strings.ToUpper(text)
	for prefix, canonical := range codePrefixes {
		if upper == prefix || strings.HasPrefix(upper, prefix+"-") {
			return canonical
		}
	}

	// "verb, transitive" and similar compounds: try the leading token.
	if fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == ' '
	}); len(fields) > 0 {
		if canonical, ok := posCanonical[posCleanRe.ReplaceAllString(fields[0], "")]; ok {
			return canonical
		}
	}
	return ""
}

// SemanticLabel identifies headings that dictionaries sometimes place in the
// same visual slot as a POS even though they introduce a typed section.
type SemanticLabel string

const (
	LabelCrossReference SemanticLabel = "crossReference"
	LabelRelated        SemanticLabel = "related"
	LabelPhrase         SemanticLabel = "phrase"
	LabelIdiom          SemanticLabel = "idiom"
	LabelPhrasalVerb    SemanticLabel = "phrasalVerb"
	LabelDerivative     SemanticLabel = "derivative"
	LabelSynonyms       SemanticLabel = "synonyms"
	LabelAntonyms       SemanticLabel = "antonyms"
	LabelExamples       SemanticLabel = "examples"
	LabelCollocations   SemanticLabel = "collocations"
	LabelUsage          SemanticLabel = "usage"
	LabelGrammar        SemanticLabel = "grammar"
)

var semanticLabels = map[string]SemanticLabel{
	"seealso": LabelCrossReference, "seealsos": LabelCrossReference,
	"crossreference": LabelCrossReference, "crossreferences": LabelCrossReference,
	"crossref": LabelCrossReference, "reference": LabelCrossReference,
	"related": LabelRelated, "relatedword": LabelRelated, "relatedwords": LabelRelated,
	"phrase": LabelPhrase, "phrases": LabelPhrase, "convention": LabelPhrase,
	"conventions": LabelPhrase, "proverb": LabelPhrase, "proverbs": LabelPhrase,
	"saying": LabelPhrase, "sayings": LabelPhrase, "短语": LabelPhrase,
	"idiom": LabelIdiom, "idioms": LabelIdiom, "习语": LabelIdiom,
	"phrasalverb": LabelPhrasalVerb, "phrasalverbs": LabelPhrasalVerb,
	"derivative": LabelDerivative, "derivatives": LabelDerivative,
	"derivedword": LabelDerivative, "derivedwords": LabelDerivative,
	"synonym": LabelSynonyms, "synonyms": LabelSynonyms,
	"synonymantonym": LabelSynonyms, "synonymsantonyms": LabelSynonyms,
	"antonym": LabelAntonyms, "antonyms": LabelAntonyms,
	"example": LabelExamples, "examples": LabelExamples, "examplesentences": LabelExamples,
	"collocation": LabelCollocations, "collocations": LabelCollocations, "wordcombinations": LabelCollocations,
	"usage": LabelUsage, "usagenote": LabelUsage, "usagenotes": LabelUsage, "用法": LabelUsage,
	"grammar": LabelGrammar, "grammarnote": LabelGrammar, "grammarnotes": LabelGrammar, "语法": LabelGrammar,
}

var semanticLabelCleanRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// ClassifySemanticLabel returns a section role for a non-POS heading.
func ClassifySemanticLabel(raw string) SemanticLabel {
	key := strings.ToLower(semanticLabelCleanRe.ReplaceAllString(Normalize(raw), ""))
	for _, suffix := range []string{"section", "list", "block", "panel", "box", "group"} {
		key = strings.TrimSuffix(key, suffix)
	}
	return semanticLabels[key]
}

// knownLabels are register, regional and domain labels worth surfacing.
var knownLabels = map[string]bool{
	"formal": true, "informal": true, "slang": true, "literary": true, "poetic": true,
	"technical": true, "specialist": true, "humorous": true, "ironic": true,
	"offensive": true, "taboo": true, "derogatory": true, "disapproving": true,
	"approving": true, "old-fashioned": true, "old use": true, "archaic": true,
	"dated": true, "rare": true, "figurative": true, "literal": true,
	"british": true, "american": true, "australian": true, "scottish": true, "irish": true,
	"chiefly british": true, "chiefly us": true, "brit": true, "bre": true, "ame": true,
	"north american": true, "us": true, "uk": true, "nz": true,
	"medicine": true, "law": true, "computing": true, "biology": true, "music": true,
	"business": true, "finance": true, "grammar": true, "linguistics": true,
	"written": true, "spoken": true, "trademark": true,
}

// IsKnownLabel reports whether text reads as a dictionary usage label.
func IsKnownLabel(raw string) bool {
	text := strings.ToLower(strings.Trim(Normalize(raw), "()[]., "))
	if text == "" {
		return false
	}
	if knownLabels[text] {
		return true
	}
	// Comma-separated label lists such as "(informal, disapproving)".
	parts := strings.Split(text, ",")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !knownLabels[strings.TrimSpace(part)] {
			return false
		}
	}
	return true
}

// formNames maps inflection labels onto the names shown in Bob's exchanges
// list, in both English and Chinese as dictionaries use both.
var formNames = map[string]string{
	"plural": "plural", "pl": "plural", "复数": "plural",
	"past tense": "past tense", "past": "past tense", "pt": "past tense", "过去式": "past tense",
	"past participle": "past participle", "pp": "past participle", "过去分词": "past participle",
	"present participle": "present participle", "现在分词": "present participle",
	"present tense": "present tense", "3rd person singular present tense": "third person singular",
	"third person singular": "third person singular", "third person": "third person singular",
	"第三人称单数":      "third person singular",
	"comparative": "comparative", "比较级": "comparative",
	"superlative": "superlative", "最高级": "superlative",
}

// CanonicalForm normalizes an inflection label, returning "" when unrecognised.
func CanonicalForm(raw string) string {
	text := strings.ToLower(strings.Trim(Normalize(raw), " :,;()"))
	if name, ok := formNames[text]; ok {
		return name
	}
	return ""
}

// ipaRe detects IPA by the presence of characters that essentially only occur
// in phonetic transcription. Relying on character evidence rather than a class
// name is what lets unknown dictionaries still produce pronunciations.
// strongIPARe matches characters that occur in phonetic transcription and
// essentially nowhere else, so one of them is evidence on its own.
var strongIPARe = regexp.MustCompile(`[ˈˌːɪʊəɜɔɒʌθðʃʒŋɑɐɛɡʁʔɾɹɲʎɯɤʉɨʲʰ]`)

// weakIPARe matches characters the IPA shares with ordinary orthography.
//
// They cannot decide on their own. "y" is the close front rounded vowel and
// also the twenty-fifth letter of the English alphabet, which was enough to
// make the generic parser read "necessary" as a transcription — and with it
// every heading, label and section title in the corpus that happens to end in
// one. "ç" and "æ" are the same story in French and Danish.
var weakIPARe = regexp.MustCompile(`[yæœøç]`)

// delimitedRe matches a transcription written between the slashes or brackets
// dictionaries use for one. That delimiting is itself a claim, and it is what
// lets a transcription made only of ordinary letters still be recognised.
var delimitedRe = regexp.MustCompile(`^[/\[]\s*\S.*\S\s*[/\]]$`)

// LooksLikeIPA reports whether text is plausibly a phonetic transcription.
func LooksLikeIPA(raw string) bool {
	normalized := Normalize(raw)
	text := strings.Trim(normalized, "/[]() ")
	if text == "" {
		return false
	}
	runes := []rune(text)
	if len(runes) > 60 {
		return false
	}
	if !strongIPARe.MatchString(text) &&
		!(delimitedRe.MatchString(normalized) && weakIPARe.MatchString(text)) {
		return false
	}
	// Reject prose that merely contains one IPA-ish character.
	letters, spaces := 0, 0
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters > 0 && spaces <= 4
}

// CleanIPA strips the surrounding slashes and brackets.
func CleanIPA(raw string) string {
	return strings.Trim(Normalize(raw), "/[]()： ;,")
}

// ukMarkers and usMarkers are the tokens dictionaries use to signal a
// pronunciation variety, in class names, filenames, titles and visible text.
var ukMarkers = []string{
	"uk", "gb", "bre", "brit", "british", "breprons", "brefile", "en_gb", "rp",
	"phon-gb", "type_uk", "icon-speak-uk", "englishuk",
}

var usMarkers = []string{
	"us", "usa", "ame", "amefile", "ameprons", "american", "en_us", "name",
	"phon-us", "type_us", "icon-speak-us", "namerican",
}

// wordBoundaryMarker builds a regexp that matches a marker only at a token
// boundary, so "us" does not match inside "thesaurus" or "because".
func wordBoundaryMarker(marker string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(marker) + `($|[^a-z0-9])`)
}

var ukMarkerRes, usMarkerRes = compileMarkers(ukMarkers), compileMarkers(usMarkers)

func compileMarkers(markers []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(markers))
	for _, marker := range markers {
		out = append(out, wordBoundaryMarker(marker))
	}
	return out
}

// hasRegionMarker reports whether text carries any variety marker at all.
func hasRegionMarker(text string) bool {
	for _, re := range ukMarkerRes {
		if re.MatchString(text) {
			return true
		}
	}
	for _, re := range usMarkerRes {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// DetectRegion classifies a pronunciation from all the evidence around it:
// class names, ids, titles, hrefs, resource filenames and neighbouring text.
//
// It returns RegionOther when the evidence is absent or contradictory. Guessing
// would put an American clip behind a UK flag, which is worse than showing no
// region at all.
func DetectRegion(descriptors ...string) entryir.Region {
	haystack := strings.ToLower(strings.Join(descriptors, " "))
	if haystack == "" {
		return entryir.RegionOther
	}
	ukHits, usHits := 0, 0
	for _, re := range ukMarkerRes {
		if re.MatchString(haystack) {
			ukHits++
		}
	}
	for _, re := range usMarkerRes {
		if re.MatchString(haystack) {
			usHits++
		}
	}
	switch {
	case ukHits > usHits:
		return entryir.RegionUK
	case usHits > ukHits:
		return entryir.RegionUS
	default:
		return entryir.RegionOther
	}
}

// senseNumberRe matches "1", "1.", "2.3", "(3)" and the CJK full-width forms.
var senseNumberRe = regexp.MustCompile(`^[\s(（\[]*([0-9]+(?:[.\-][0-9]+)*)[\s.)）\]、]*$`)

// ParseSenseNumber extracts a sense number from a label, or "" if it is prose.
func ParseSenseNumber(raw string) string {
	match := senseNumberRe.FindStringSubmatch(Normalize(raw))
	if match == nil {
		return ""
	}
	return match[1]
}
