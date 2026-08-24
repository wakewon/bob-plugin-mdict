package parser

import (
	"bytes"
	"strings"

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

// Options configures one parse.
type Options struct {
	// Headword is the key that was looked up, used when the entry does not
	// state its own headword.
	Headword string
	// Profile is the matched dictionary profile, or nil for generic parsing.
	Profile *Profile
	// Audio resolves pronunciation references. May be nil.
	Audio AudioResolver
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

	state := &parseState{doc: doc, opts: opts, entry: entry}
	if profile != nil {
		state.profile = profile.compiled
	}

	state.applyRoot()
	state.stripChrome()
	state.parseHeadword()
	state.parsePronunciations()
	state.parseForms()
	state.parseWordFamily()
	// Sections are lifted out (and detached) before sense parsing so their
	// internal structure cannot be mistaken for the entry's own senses.
	state.parseSections()
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
	main := s.textOf(node)
	sel := s.translationSelector()
	if sel.IsEmpty() {
		return main, ""
	}
	var parts []string
	for _, match := range QueryAll(node, sel) {
		if text := Text(match, TextOptions{SkipHidden: true}); text != "" {
			parts = append(parts, text)
		}
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
			if text := Normalize(strings.NewReplacer("·", "", "‧", "", "|", "").Replace(s.textOf(node))); text != "" {
				s.entry.Headword = text
				s.note("headword from profile selector %q", s.profile.headword)
				return
			}
		}
	}
	// Generic: the first h1/h2 that is short enough to be a headword.
	for _, node := range QueryAll(s.doc, ParseSelector("h1, h2")) {
		text := Normalize(Text(node, TextOptions{SkipHidden: true}))
		if text != "" && len([]rune(text)) <= 60 && !strings.Contains(text, " in English") {
			s.entry.Headword = text
			s.note("headword from generic heading")
			return
		}
	}
}

func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	var builder strings.Builder
	fprintf(&builder, format, args...)
	return builder.String()
}
