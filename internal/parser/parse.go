package parser

import (
	"bytes"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// AudioResolver turns a resource reference found in entry HTML into a playable
// asset, or nil when the user's MDD does not contain it.
//
// Returning nil is the whole point: a pronunciation with no backing MDD file is
// reported without audio rather than being filled in from any other source.
type AudioResolver interface {
	ResolveAudio(ref string) *entryir.Audio
}

// AudioResolverFunc adapts a function to AudioResolver.
type AudioResolverFunc func(ref string) *entryir.Audio

// ResolveAudio implements AudioResolver.
func (f AudioResolverFunc) ResolveAudio(ref string) *entryir.Audio { return f(ref) }

// ImageResolver turns an inline image reference into a dictionary-bound MDD
// resource. Missing MDD assets and non-dictionary URLs resolve to nil.
type ImageResolver interface {
	ResolveImage(ref, alt string) *entryir.Image
}

// ImageResolverFunc adapts a function to ImageResolver.
type ImageResolverFunc func(ref, alt string) *entryir.Image

// ResolveImage implements ImageResolver.
func (f ImageResolverFunc) ResolveImage(ref, alt string) *entryir.Image { return f(ref, alt) }

// Options configures one parse.
type Options struct {
	// Headword is the key that was looked up, used when the entry does not
	// state its own headword.
	Headword string
	// Profile is the matched dictionary profile, or nil for generic parsing.
	Profile *Profile
	// Audio resolves pronunciation references. May be nil.
	Audio AudioResolver
	// Image resolves inline illustrations in free-form prose. May be nil.
	Image ImageResolver
	// MaxExamplesPerSense caps example output. Some dictionaries ship twenty
	// corpus sentences per sense, which is unreadable in a popup.
	MaxExamplesPerSense int
	// Debug records rule provenance and confidence notes on the entry.
	Debug bool
}

const defaultMaxExamples = 4

// audioAttrs are the attributes that can carry a resource reference. `addr` is
// non-standard but is how several Oxford repacks point at their audio.
var audioAttrs = []string{"href", "src", "data-src-mp3", "addr", "data-src"}

// Parse converts raw entry HTML into the Entry IR.
func Parse(raw []byte, opts Options) (*entryir.Entry, error) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if opts.MaxExamplesPerSense <= 0 {
		opts.MaxExamplesPerSense = defaultMaxExamples
	}

	entry := &entryir.Entry{Headword: opts.Headword}
	profile := opts.Profile
	if profile != nil {
		profile.Compile()
		entry.Source.Profile = profile.ID
	} else {
		entry.Source.Profile = "generic"
	}

	state := &parseState{doc: doc, opts: opts, entry: entry, recordRunes: -1}
	if profile != nil {
		state.profile = profile.compiled
	}

	state.applyRoot()
	state.stripChrome()
	state.parseHeadword()
	state.entryScript = dominantScript(firstNonEmpty(opts.Headword, entry.Headword))
	state.parsePronunciations()
	state.parseForms()
	state.parseWordFamily()
	// Sections are lifted out (and detached) before sense parsing so their
	// internal structure cannot be mistaken for the entry's own senses.
	state.parseSections()
	state.parseGenericCrossReferences()
	state.parseGenericSemanticSections()
	state.parseParts()
	state.finalize()

	return entry, nil
}

type parseState struct {
	doc          *html.Node
	opts         Options
	profile      *compiledProfile
	entry        *entryir.Entry
	notes        []string
	partsHandled bool
	// entryScript is the writing system the headword is in. It is what tells
	// generic bilingual extraction which side of an entry is the gloss.
	entryScript script
	// recordRunes memoizes the record's text length; -1 means not yet measured.
	recordRunes int
}

func (s *parseState) note(format string, args ...any) {
	if !s.opts.Debug {
		return
	}
	s.notes = append(s.notes, sprintf(format, args...))
}

func (s *parseState) finalize() {
	if s.opts.Debug {
		s.entry.Notes = s.notes
	}
	if s.entry.Headword == "" {
		s.entry.Headword = s.opts.Headword
	}
	dedupeStrings(&s.entry.Synonyms)
	dedupeStrings(&s.entry.Antonyms)
	dedupeStrings(&s.entry.CrossReferences)
	dedupeStrings(&s.entry.Related)
	dedupeStrings(&s.entry.Collocations)
	dedupeStrings(&s.entry.WordFamily)
}

// applyRoot narrows the parse to the profile's declared entry root.
func (s *parseState) applyRoot() {
	if s.profile == nil || s.profile.root.IsEmpty() {
		return
	}
	matches := QueryAll(s.doc, s.profile.root)
	if len(matches) == 0 {
		return
	}
	s.doc = matches[0]
	s.note("narrowed to entry root (%d candidates)", len(matches))
}

// stripChrome removes scripts, styles and profile-declared noise, plus nodes
// hidden by inline styles, before any text extraction runs.
func (s *parseState) stripChrome() {
	RemoveMatching(s.doc, ParseSelector("script, style, head, link"))
	if s.profile != nil {
		removed := RemoveMatching(s.doc, s.profile.ignore)
		s.note("profile ignore removed %d nodes", removed)
	}
	// Dictionaries hide the language variant the user did not pick with inline
	// display:none. Keeping it would duplicate every definition.
	var hidden []*html.Node
	Walk(s.doc, func(node *html.Node) bool {
		if hiddenByStyle(node) {
			hidden = append(hidden, node)
			return false
		}
		return true
	})
	for _, node := range hidden {
		if node.Parent != nil {
			node.Parent.RemoveChild(node)
		}
	}
}

func (s *parseState) translationSelector() Selector {
	if s.profile != nil {
		return s.profile.translation
	}
	return Selector{}
}

// textOf extracts a node's text with the profile's translation nodes removed.
func (s *parseState) textOf(node *html.Node) string {
	return Text(node, TextOptions{Skip: s.translationSelector(), SkipHidden: true})
}

// splitTranslation returns a node's own-language text and the translation
// carried inside it, which bilingual dictionaries interleave in one element.
func (s *parseState) splitTranslation(node *html.Node) (string, string) {
	sel := s.translationSelector()
	if sel.IsEmpty() {
		// With no profile to name the gloss element, the scripts in play are
		// the only evidence of one. Prefer the element-level split: a gloss in
		// its own span is stated structure, and only when there is none does
		// the seam have to be found inside the text.
		if own, translated, ok := s.splitByScript(node); ok {
			return own, translated
		}
		text := s.textOf(node)
		if own, translated, ok := s.splitTextByScript(text); ok {
			return own, translated
		}
		return text, ""
	}
	main := s.textOf(node)
	var parts []string
	for _, match := range QueryAll(node, sel) {
		if text := Text(match, TextOptions{SkipHidden: true}); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		// The profile names the gloss element and it is not here. Repacks of a
		// profiled dictionary reuse its structure while renaming that one
		// class, and the result is a definition with its translation glued to
		// the end of it. Script evidence is weaker than a profile selector,
		// which is why it is consulted only where the selector found nothing.
		if own, translated, ok := s.splitByScript(node); ok {
			return own, translated
		}
		if own, translated, ok := s.splitTextByScript(main); ok {
			return own, translated
		}
		return main, ""
	}
	// Bilingual dictionaries often emit the same gloss twice, once before and
	// once after the source-language definition, and hide one of them with CSS
	// the reader never sees.
	dedupeStrings(&parts)
	return main, Normalize(strings.Join(parts, " "))
}

func (s *parseState) parseHeadword() {
	if s.profile != nil && !s.profile.headword.IsEmpty() {
		if node := Query(s.doc, s.profile.headword); node != nil {
			// Hyphenation markers split "a·ban·don" across child spans.
			if text := cleanHeadword(s.textOf(node)); text != "" {
				s.entry.Headword = text
				s.note("headword from profile selector %q", s.profile.headword)
				return
			}
		}
	}
	// Generic: an element that says it holds the headword, before any heading.
	// A heading is a position; a class named "hw" or "entry_title" is a claim.
	//
	// The claim still has to survive comparison with the key the record was
	// found under. "entry_title" is used both for the headword and for a page
	// banner reading "Definition of 'below'", and "headword" is used both for
	// the word and for the block that contains the word, its word classes and
	// its pronunciation. Taking the first match on trust turns either into the
	// entry's name.
	found := ""
	Walk(s.doc, func(node *html.Node) bool {
		if found != "" {
			return false
		}
		if !classTokenMatches(node, headwordClassHints) {
			return true
		}
		text := cleanHeadword(s.textOf(node))
		if text == "" || len([]rune(text)) > 60 {
			return true
		}
		if s.opts.Headword != "" && !HeadwordMatchesKey(text, s.opts.Headword) {
			// Keep looking: the real headword is often nested inside the
			// block that carries the class.
			return true
		}
		found = text
		return false
	})
	if found != "" {
		s.entry.Headword = found
		s.note("headword from generic headword class")
		return
	}

	// Otherwise the first h1/h2 short enough to be a headword — but only when
	// it resembles the key the record was reached by. Dictionaries put "Examples"
	// and "Word History" in headings too, and a heading that has nothing to do
	// with the key is a section title, not the entry's name.
	for _, node := range QueryAll(s.doc, ParseSelector("h1, h2")) {
		text := cleanHeadword(Text(node, TextOptions{SkipHidden: true}))
		if text == "" || len([]rune(text)) > 60 || strings.Contains(text, " in English") {
			continue
		}
		if s.opts.Headword != "" && !HeadwordMatchesKey(text, s.opts.Headword) {
			continue
		}
		s.entry.Headword = text
		s.note("headword from generic heading")
		return
	}
}

// cleanHeadword removes the hyphenation markers dictionaries scatter through a
// headword to show where it may be broken across lines.
func cleanHeadword(raw string) string {
	return Normalize(strings.NewReplacer("·", "", "‧", "", "|", "", "•", "").Replace(raw))
}

// HeadwordMatchesKey reports whether a parsed headword plausibly names the same
// entry as the key it was looked up under.
//
// The comparison is deliberately loose. Hyphenation dots, letter case,
// diacritics rendered as separate spans and a trailing homograph number are all
// normal differences between the two; a completely different string is not.
func HeadwordMatchesKey(headword, key string) bool {
	left, right := headwordIdentity(headword), headwordIdentity(key)
	if left == "" || right == "" {
		return true
	}
	return strings.HasPrefix(left, right) || strings.HasPrefix(right, left)
}

func headwordIdentity(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(cleanHeadword(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	var builder strings.Builder
	fprintf(&builder, format, args...)
	return builder.String()
}
