// Package service is the application core: it owns the dictionary registry,
// picks parser profiles, resolves lookups and mints resource URLs.
package service

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/profiles"
	"github.com/wakewon/bob-plugin-mdict/internal/resource"
)

// Service coordinates every component behind the HTTP API.
type Service struct {
	cfg        config.Config
	registry   *mdict.Registry
	tokenizer  *resource.Tokenizer
	transcoder *resource.Transcoder
	startedAt  time.Time

	// baseURL is the loopback origin resource URLs are built from.
	baseURL string

	profileMu    sync.RWMutex
	profileByDic map[string]*parser.Profile
	profileDone  map[string]bool

	cacheMu sync.Mutex
	cache   *entryCache
}

// New builds a service. Dictionaries are discovered but not yet indexed.
func New(cfg config.Config) (*Service, error) {
	tokenizer, err := resource.NewTokenizer()
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:          cfg,
		registry:     mdict.NewRegistry(cfg.DictionaryDir),
		tokenizer:    tokenizer,
		transcoder:   resource.NewTranscoder(cfg.CacheDir),
		startedAt:    time.Now(),
		baseURL:      fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
		profileByDic: make(map[string]*parser.Profile),
		profileDone:  make(map[string]bool),
		cache:        newEntryCache(256),
	}, nil
}

// Config exposes the resolved configuration.
func (s *Service) Config() config.Config { return s.cfg }

// Registry exposes the dictionary registry.
func (s *Service) Registry() *mdict.Registry { return s.registry }

// Transcoder exposes the audio transcoder.
func (s *Service) Transcoder() *resource.Transcoder { return s.transcoder }

// Uptime returns how long the service has been running.
func (s *Service) Uptime() time.Duration { return time.Since(s.startedAt) }

// Rescan rediscovers dictionaries and rebuilds every index.
func (s *Service) Rescan() error {
	if err := s.registry.Scan(); err != nil {
		return err
	}
	s.profileMu.Lock()
	s.profileByDic = make(map[string]*parser.Profile)
	s.profileDone = make(map[string]bool)
	s.profileMu.Unlock()

	s.cacheMu.Lock()
	s.cache = newEntryCache(256)
	s.cacheMu.Unlock()

	s.registry.LoadAll()
	// Resolve profiles up front so the first user lookup is not slowed by
	// fingerprinting, and so /v2/dictionaries can report them.
	for _, dict := range s.registry.All() {
		s.profileFor(dict)
	}
	return nil
}

// sampleWords are probed when fingerprinting a dictionary. They are chosen to
// exist in essentially every English dictionary while being cheap to resolve.
var sampleWords = []string{"abandon", "hello", "run", "water", "book", "the", "good"}

// profileFor returns the parser profile for a dictionary, detecting it once.
func (s *Service) profileFor(dict *mdict.Dictionary) *parser.Profile {
	id := dict.ID()
	s.profileMu.RLock()
	if s.profileDone[id] {
		profile := s.profileByDic[id]
		s.profileMu.RUnlock()
		return profile
	}
	s.profileMu.RUnlock()

	profile := s.detectProfile(dict)

	s.profileMu.Lock()
	s.profileDone[id] = true
	s.profileByDic[id] = profile
	s.profileMu.Unlock()
	return profile
}

func (s *Service) detectProfile(dict *mdict.Dictionary) *parser.Profile {
	info := dict.Info()
	if info.Health != mdict.HealthOK {
		return nil
	}
	for _, word := range sampleWords {
		set, err := dict.LookupAll(word)
		if err != nil {
			continue
		}
		for _, result := range set.Records {
			if len(result.HTML) < 200 {
				continue
			}
			doc, parseErr := html.Parse(bytes.NewReader(result.HTML))
			if parseErr != nil {
				continue
			}
			title := info.Title + " " + path.Base(dict.SourcePath())
			if profile := profiles.Match(title, doc); profile != nil {
				return profile
			}
		}
	}
	return nil
}

// ProfileID returns the profile identifier for a dictionary, or "generic".
func (s *Service) ProfileID(dict *mdict.Dictionary) string {
	if profile := s.profileFor(dict); profile != nil {
		return profile.ID
	}
	return "generic"
}

// LookupMode controls how hard the service tries to find a headword.
type LookupMode string

const (
	// ModeExact prefers exact headword spelling, then accepts Unicode-normalized
	// and case-insensitive fallback matches. It never adds prefix suggestions.
	ModeExact LookupMode = "exact"
	// ModeSmart additionally offers prefix suggestions when nothing matched.
	ModeSmart LookupMode = "smart"
)

// Match is one dictionary's answer to a lookup.
type Match struct {
	DictionaryID    string `json:"dictionaryId"`
	DictionaryTitle string `json:"dictionaryTitle"`
	// LookupKey is the actual aggregate MDX key selected before duplicate
	// expansion. It remains distinct from parsed Headword and record-level
	// Source.MatchedKey provenance.
	LookupKey string                `json:"lookupKey"`
	Headword  string                `json:"headword"`
	Records   []entryir.EntryRecord `json:"records"`
	// ParseMillis is how long parsing took, for diagnostics.
	ParseMillis float64 `json:"parseMillis,omitempty"`
}

func matchFromSet(dict *mdict.Dictionary, set *entryir.EntrySet, parseMillis float64) Match {
	return Match{
		DictionaryID:    dict.ID(),
		DictionaryTitle: dict.Info().Title,
		LookupKey:       set.LookupKey,
		Headword:        set.Headword,
		Records:         set.Records,
		ParseMillis:     parseMillis,
	}
}

func (m *Match) entrySet() *entryir.EntrySet {
	if m == nil {
		return nil
	}
	return &entryir.EntrySet{LookupKey: m.LookupKey, Headword: m.Headword, Records: m.Records}
}

// Result is the full answer to a lookup request.
type Result struct {
	Query   string  `json:"query"`
	Matches []Match `json:"matches"`
	// Bob is the ready-to-return toDict, present only when the caller asked
	// for it. Keeping it optional means the IR stays the canonical contract
	// and any other client can ignore Bob entirely.
	Bob         *bobadapter.Dict `json:"bob,omitempty"`
	Suggestions []string         `json:"suggestions,omitempty"`
}

// ErrNoDictionaries means the user has not installed any dictionaries yet.
var ErrNoDictionaries = errors.New("no dictionaries available")

// ErrDictionaryNotFound and ErrDictionaryUnavailable make a saved explicit ID
// failure distinguishable from a normal headword miss.
var (
	ErrDictionaryNotFound    = errors.New("dictionary ID not found")
	ErrDictionaryUnavailable = errors.New("dictionary unavailable")
	ErrRecordNotFound        = errors.New("record ordinal not found")
)

// RecordNotFoundError distinguishes an out-of-range presentation selector
// from a missing base headword.
type RecordNotFoundError struct {
	Query     string
	Requested int
	Available int
}

func (e *RecordNotFoundError) Error() string {
	return fmt.Sprintf("record %d not found for %q (%d available)", e.Requested, e.Query, e.Available)
}

func (e *RecordNotFoundError) Unwrap() error { return ErrRecordNotFound }

// LookupOptions configures a lookup.
type LookupOptions struct {
	DictionaryIDs []string
	Mode          LookupMode
	// Limit caps how many dictionaries answer. Zero means all selected.
	Limit int
	// MaxExamples caps examples per sense.
	MaxExamples int
	Debug       bool
	// RenderBob adds a rendered toDict to the result.
	RenderBob bool
	// BobOptions configures that rendering.
	BobOptions bobadapter.Options
}

// Lookup resolves a query across the selected dictionaries.
func (s *Service) Lookup(query string, opts LookupOptions) (*Result, error) {
	query = mdict.NormalizeExactKey(query)
	if query == "" {
		return nil, errors.New("empty query")
	}
	if total, _ := s.registry.Counts(); total == 0 {
		return nil, ErrNoDictionaries
	}
	for _, id := range opts.DictionaryIDs {
		id = strings.TrimSpace(id)
		dict, ok := s.registry.ByID(id)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrDictionaryNotFound, id)
		}
		if dict.Info().Health != mdict.HealthOK {
			return nil, fmt.Errorf("%w: %s", ErrDictionaryUnavailable, id)
		}
	}
	dicts := s.registry.Select(opts.DictionaryIDs)
	if len(dicts) == 0 {
		return nil, errors.New("no matching dictionaries")
	}

	result := &Result{Query: query}
	for _, dict := range dicts {
		if opts.Limit > 0 && len(result.Matches) >= opts.Limit {
			break
		}
		if dict.Info().Health != mdict.HealthOK {
			continue
		}
		match, ok := s.lookupOne(dict, query, opts)
		if !ok {
			continue
		}
		result.Matches = append(result.Matches, match)
	}

	if len(result.Matches) == 0 && opts.Mode == ModeSmart {
		result.Suggestions = s.suggest(dicts, query, 8)
	}
	if opts.RenderBob && len(result.Matches) > 0 {
		// Bob presents one result card per configured service instance. Even
		// when an API client asks the server for several matches, the Bob view is
		// deliberately rendered from the first match only.
		set := result.Matches[0].entrySet()
		if ordinal := opts.BobOptions.RecordOrdinal; ordinal > len(set.Records) {
			return nil, &RecordNotFoundError{
				Query:     query,
				Requested: ordinal,
				Available: len(set.Records),
			}
		}
		result.Bob = bobadapter.RenderEntrySet(set, opts.BobOptions)
	}
	return result, nil
}

func (s *Service) lookupOne(dict *mdict.Dictionary, query string, opts LookupOptions) (Match, bool) {
	cacheKey := entryCacheKey{
		dictionaryID: dict.ID(),
		query:        mdict.NormalizeExactKey(query),
		maxExamples:  opts.MaxExamples,
		debug:        opts.Debug,
	}
	s.cacheMu.Lock()
	cached, ok := s.cache.get(cacheKey)
	s.cacheMu.Unlock()
	if ok {
		return matchFromSet(dict, cached, 0), true
	}

	lookupSet, err := dict.LookupAll(query)
	if err != nil {
		return Match{}, false
	}

	started := time.Now()
	info := dict.Info()
	profile := s.profileFor(dict)
	profileID := "generic"
	if profile != nil {
		profileID = profile.ID
	}
	set := &entryir.EntrySet{LookupKey: lookupSet.MatchedKey}
	for _, lookupResult := range lookupSet.Records {
		entry, parseErr := parser.Parse(lookupResult.HTML, parser.Options{
			Headword:            lookupResult.MatchedKey,
			Profile:             profile,
			Audio:               s.audioResolver(dict),
			MaxExamplesPerSense: opts.MaxExamples,
			Debug:               opts.Debug,
		})
		if parseErr != nil {
			continue
		}
		entry.Source = entryir.Source{
			DictionaryID:      info.ID,
			DictionaryTitle:   info.Title,
			MatchedKey:        lookupResult.MatchedKey,
			RedirectedFrom:    lookupResult.RedirectedFrom,
			Profile:           profileID,
			RawRecordOrdinal:  lookupResult.RawRecordOrdinal,
			RecordStartOffset: lookupResult.RecordStartOffset,
		}
		if entry.IsEmpty() {
			continue
		}
		if set.Headword == "" {
			set.Headword = entry.Headword
		}
		set.Records = append(set.Records, entryir.EntryRecord{
			RecordOrdinal: len(set.Records) + 1,
			Entry:         entry,
		})
	}
	if len(set.Records) == 0 {
		return Match{}, false
	}
	elapsed := time.Since(started)

	s.cacheMu.Lock()
	s.cache.put(cacheKey, set)
	s.cacheMu.Unlock()

	return matchFromSet(dict, set, float64(elapsed.Microseconds())/1000), true
}

func (s *Service) suggest(dicts []*mdict.Dictionary, query string, limit int) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, dict := range dicts {
		for _, word := range dict.Prefix(query, limit) {
			key := mdict.NormalizeExactKey(word)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, word)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// audioResolver binds a dictionary to the audio resolution pipeline.
//
// A reference only produces an Audio when the dictionary's own MDD actually
// contains it, and when a Speex asset can actually be decoded on this machine.
// There is no synthesis fallback anywhere in this path.
func (s *Service) audioResolver(dict *mdict.Dictionary) parser.AudioResolver {
	return parser.AudioResolverFunc(func(ref string) *entryir.Audio {
		if !mdict.IsAudioRef(ref) {
			return nil
		}
		if !dict.HasResource(ref) {
			return nil
		}
		if mdict.IsSpeexRef(ref) && !s.transcoder.SpeexAvailable() {
			return nil
		}
		token, err := s.tokenizer.Mint(resource.Ref{
			DictionaryID: dict.ID(),
			ResourceRef:  ref,
		})
		if err != nil {
			return nil
		}
		return &entryir.Audio{
			ResourceRef: ref,
			Token:       token,
			URL:         s.baseURL + "/v2/resource/" + url.PathEscape(token),
			MIMEType:    mdict.MIMEType(ref),
		}
	})
}

// ResolveResource returns the bytes and content type behind a resource token.
func (s *Service) ResolveResource(token string) (data []byte, contentType string, err error) {
	ref, err := s.tokenizer.Open(token)
	if err != nil {
		return nil, "", err
	}
	dict, ok := s.registry.ByID(ref.DictionaryID)
	if !ok {
		return nil, "", mdict.ErrNotFound
	}
	raw, err := dict.Resource(ref.ResourceRef)
	if err != nil {
		return nil, "", err
	}
	if mdict.IsSpeexRef(ref.ResourceRef) {
		wav, err := s.transcoder.SpeexToWAV(raw)
		if err != nil {
			return nil, "", err
		}
		return wav, "audio/wav", nil
	}
	return raw, mdict.MIMEType(ref.ResourceRef), nil
}
