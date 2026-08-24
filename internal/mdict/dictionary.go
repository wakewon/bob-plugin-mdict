// Package mdict wraps the low-level MDX/MDD engine with the behaviour this
// project needs: recursive discovery, stable dictionary IDs, correct @@@LINK
// redirection, normalized lookup, and O(1) MDD resource resolution.
package mdict

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib-x/mdx"
	"golang.org/x/text/unicode/norm"
)

// Health describes whether a dictionary is usable.
type Health string

const (
	HealthOK          Health = "ok"
	HealthUnavailable Health = "unavailable"
)

// Info is the user-visible description of one dictionary.
type Info struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	EntryCount  int64    `json:"entryCount"`
	Encoding    string   `json:"encoding,omitempty"`
	Version     string   `json:"version,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	HasMDD      bool     `json:"hasMDD"`
	MDDVolumes  int      `json:"mddVolumes"`
	Profile     string   `json:"profile"`
	Health      Health   `json:"health"`
	Diagnostics []string `json:"diagnostics,omitempty"`
	// LoadedAt is when the index finished building.
	LoadedAt time.Time `json:"loadedAt,omitzero"`
}

// Dictionary is one MDX file plus its MDD volume chain.
type Dictionary struct {
	info Info

	mdxPath  string
	mddPaths []string

	// dirName is the containing folder, used for the display title fallback.
	dirName string

	mu   sync.RWMutex
	mdx  *mdx.Mdict
	mdds []*mdx.Mdict

	// exact maps each headword to its first record. exactExtras is allocated
	// only for keys with duplicate records, so the hundreds of thousands of
	// ordinary single-record keys do not each pay for a slice header. folded
	// remains the case- and diacritic-normalized fallback index.
	exact       map[string]*mdx.MDictKeywordEntry
	exactExtras map[string][]*mdx.MDictKeywordEntry
	folded      map[string]*mdx.MDictKeywordEntry
	// mddIdx maps a normalized resource key to the volume holding it.
	mddIdx map[string]mddLocation

	loaded bool
	err    error
}

type mddLocation struct {
	volume int
	entry  *mdx.MDictKeywordEntry
}

// NormalizeExactKey produces the canonical identity used by exact lookup,
// redirect cycle detection, cache keys and suggestion deduplication. Unicode
// canonically equivalent spellings share identity; letter case does not.
func NormalizeExactKey(word string) string {
	trimmed := strings.TrimSpace(word)
	if trimmed == "" {
		return ""
	}
	return norm.NFC.String(trimmed)
}

// foldKey is deliberately separate from exact identity. It exists only for
// the case-insensitive fallback after an exact spelling did not match.
func foldKey(word string) string {
	return strings.ToLower(NormalizeExactKey(word))
}

// Info returns a snapshot of the dictionary metadata.
func (d *Dictionary) Info() Info {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.info
}

// ID returns the stable dictionary identifier.
func (d *Dictionary) ID() string { return d.info.ID }

// SourcePath returns the MDX path. It is used for diagnostics and CLI output
// only and is never exposed through the HTTP API.
func (d *Dictionary) SourcePath() string { return d.mdxPath }

// MDDPaths returns the MDD volume chain.
func (d *Dictionary) MDDPaths() []string { return append([]string(nil), d.mddPaths...) }

const dictionaryFingerprintSampleSize int64 = 64 << 10

// stableID derives a short path-independent content fingerprint. It samples
// the MDX header and three spread-out file regions plus file size: moving the
// dictionary or renaming either its folder or MDX file leaves the ID unchanged,
// while different editions normally differ without hashing a multi-gigabyte
// dictionary or any MDD volume.
func stableID(mdxPath string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, "bob-mdict-id-v2\x00")
	info, statErr := os.Stat(mdxPath)
	var size int64
	if statErr == nil {
		size = info.Size()
	}
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(size))
	_, _ = hash.Write(encoded[:])

	file, err := os.Open(mdxPath)
	if err == nil {
		defer file.Close()
		last := size - dictionaryFingerprintSampleSize
		if last < 0 {
			last = 0
		}
		offsets := []int64{0, size / 3, (size * 2) / 3, last}
		seen := make(map[int64]struct{}, len(offsets))
		buffer := make([]byte, dictionaryFingerprintSampleSize)
		for _, offset := range offsets {
			if _, duplicate := seen[offset]; duplicate {
				continue
			}
			seen[offset] = struct{}{}
			binary.BigEndian.PutUint64(encoded[:], uint64(offset))
			_, _ = hash.Write(encoded[:])
			read, readErr := file.ReadAt(buffer, offset)
			if read > 0 {
				_, _ = hash.Write(buffer[:read])
			}
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				_, _ = io.WriteString(hash, "read-error")
			}
		}
	} else {
		// The entry remains listable as unavailable. This fallback intentionally
		// avoids the path; size and modtime are the only obtainable file facts.
		_, _ = io.WriteString(hash, "unreadable")
		if info != nil {
			binary.BigEndian.PutUint64(encoded[:], uint64(info.ModTime().UnixNano()))
			_, _ = hash.Write(encoded[:])
		}
	}

	return hex.EncodeToString(hash.Sum(nil))[:16]
}

// Load opens the MDX and its MDD volumes and builds all indexes. It is safe to
// call concurrently; only the first call does work.
func (d *Dictionary) Load() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loaded {
		return d.err
	}
	d.loaded = true

	dict, err := mdx.New(d.mdxPath)
	if err != nil {
		d.err = fmt.Errorf("open mdx: %w", err)
		d.markUnavailable(d.err)
		return d.err
	}
	// PrepareForExternalIndex reads the key and record blocks without building
	// the engine's own lookup tables. Those tables are three maps over every
	// entry, one of which only serves resource files and is pure overhead for
	// an MDX. Indexing here instead costs two maps rather than three, which on
	// a large library is hundreds of megabytes of resident memory.
	if err := dict.PrepareForExternalIndex(); err != nil {
		d.err = fmt.Errorf("build mdx index: %w", err)
		d.markUnavailable(d.err)
		return d.err
	}
	entries, err := dict.GetKeyWordEntries()
	if err != nil {
		d.err = fmt.Errorf("read mdx entries: %w", err)
		d.markUnavailable(d.err)
		return d.err
	}
	d.exact = make(map[string]*mdx.MDictKeywordEntry, len(entries))
	d.exactExtras = make(map[string][]*mdx.MDictKeywordEntry)
	d.folded = make(map[string]*mdx.MDictKeywordEntry)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if _, exists := d.exact[entry.KeyWord]; !exists {
			d.exact[entry.KeyWord] = entry
		} else {
			d.exactExtras[entry.KeyWord] = append(d.exactExtras[entry.KeyWord], entry)
		}
		// Only headwords whose folded form differs from themselves need a
		// second entry: for an already-lowercase headword the fallback pass
		// finds it by probing the exact map with the folded query. Most
		// headwords are lowercase, so this avoids a duplicate map entry and a
		// duplicate string for the large majority of a dictionary.
		key := foldKey(entry.KeyWord)
		if key == "" || key == entry.KeyWord {
			continue
		}
		if _, exists := d.folded[key]; !exists {
			d.folded[key] = entry
		}
	}
	d.mdx = dict

	title := strings.TrimSpace(dict.Title())
	if title == "" {
		title = d.dirName
	}
	d.info.Title = title
	d.info.Description = strings.TrimSpace(dict.Description())
	d.info.EntryCount = dict.GetKeyWordEntriesSize()
	d.info.Version = dict.Version()
	d.info.CreatedAt = dict.CreationDate()
	d.info.Encoding = "UTF-8"
	if dict.IsUTF16() {
		d.info.Encoding = "UTF-16"
	}
	d.info.Health = HealthOK
	d.info.LoadedAt = time.Now()

	// MDD volumes are optional. A missing or broken volume degrades audio and
	// images but must never take the dictionary itself down.
	d.mddIdx = make(map[string]mddLocation)
	for i, mddPath := range d.mddPaths {
		volume, err := mdx.New(mddPath)
		if err != nil {
			d.info.Diagnostics = append(d.info.Diagnostics,
				fmt.Sprintf("MDD %s could not be opened: %v", filepath.Base(mddPath), err))
			continue
		}
		if err := volume.PrepareForExternalIndex(); err != nil {
			d.info.Diagnostics = append(d.info.Diagnostics,
				fmt.Sprintf("MDD %s index failed: %v", filepath.Base(mddPath), err))
			continue
		}
		entries, err := volume.GetKeyWordEntries()
		if err != nil {
			d.info.Diagnostics = append(d.info.Diagnostics,
				fmt.Sprintf("MDD %s entries unreadable: %v", filepath.Base(mddPath), err))
			continue
		}
		idx := len(d.mdds)
		d.mdds = append(d.mdds, volume)
		// Index every resource key up front. Without this the engine falls back
		// to a linear scan over 180k+ entries on every audio request.
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			key := NormalizeResourceKey(entry.KeyWord)
			if key == "" {
				continue
			}
			if _, exists := d.mddIdx[key]; !exists {
				d.mddIdx[key] = mddLocation{volume: idx, entry: entry}
			}
		}
		_ = i
	}
	d.info.HasMDD = len(d.mdds) > 0
	d.info.MDDVolumes = len(d.mdds)
	return nil
}

func (d *Dictionary) markUnavailable(err error) {
	d.info.Health = HealthUnavailable
	d.info.Diagnostics = append(d.info.Diagnostics, err.Error())
	if d.info.Title == "" {
		d.info.Title = d.dirName
	}
}

// ErrNotFound is returned when a headword or resource is absent.
var ErrNotFound = errors.New("not found")

// LookupResult carries a resolved entry plus how it was reached.
type LookupResult struct {
	// MatchedKey is the dictionary key that actually matched.
	MatchedKey string
	// RedirectedFrom is the original key when redirection was followed.
	RedirectedFrom string
	// HTML is the raw record content.
	HTML []byte
	// RawRecordOrdinal is the one-based source position among exact records
	// with the same MatchedKey. It is stable diagnostic provenance, not the
	// visible ordinal assigned after semantic filtering.
	RawRecordOrdinal int
	// RecordStartOffset is a stable low-level locator useful in debug reports.
	RecordStartOffset int64
}

// LookupSet is the duplicate-aware result of resolving one query. MatchedKey
// is chosen once using exact-case, NFC, then deterministic case fallback.
// Records contains only that key's records (plus records reached through their
// redirects), in source order and deduplicated by resolved bytes.
type LookupSet struct {
	MatchedKey string
	Records    []*LookupResult
}

const maxRedirectDepth = 8

// LookupAll resolves a headword and returns every exact record belonging to the
// selected key. Case resolution happens before duplicate expansion: folded
// candidates with other spellings are never mixed into the result.
//
// The bundled engine's own redirect handling matches the prefix "@@LINK=" while
// real MDict files use "@@@LINK=", and it does not strip the trailing NUL that
// terminates these records, so redirects are resolved here instead.
func (d *Dictionary) LookupAll(word string) (*LookupSet, error) {
	if err := d.Load(); err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.mdx == nil {
		return nil, ErrNotFound
	}

	_, matchedKey, ok := d.findEntry(word)
	if !ok {
		return nil, ErrNotFound
	}
	records, err := d.resolveRecords(word, matchedKey, 0, nil)
	if err != nil {
		return nil, err
	}

	seenContent := make(map[[32]byte][][]byte, len(records))
	unique := records[:0]
	for _, record := range records {
		hash := sha256.Sum256(record.HTML)
		duplicate := false
		for _, content := range seenContent[hash] {
			if bytes.Equal(content, record.HTML) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seenContent[hash] = append(seenContent[hash], record.HTML)
		unique = append(unique, record)
	}
	if len(unique) == 0 {
		return nil, ErrNotFound
	}
	return &LookupSet{MatchedKey: matchedKey, Records: unique}, nil
}

// Lookup is the single-record convenience API. LookupAll owns all matching,
// redirect and deduplication semantics so the two paths cannot diverge.
func (d *Dictionary) Lookup(word string) (*LookupResult, error) {
	set, err := d.LookupAll(word)
	if err != nil {
		return nil, err
	}
	return set.Records[0], nil
}

// resolveRecords expands every record under the key selected for word. A
// redirect record fans out to every record at its target. Each branch owns its
// visited-key set, allowing sibling records to resolve independently while
// retaining case-sensitive, actual-MatchedKey cycle detection.
func (d *Dictionary) resolveRecords(word, original string, depth int, seen map[string]struct{}) ([]*LookupResult, error) {
	if depth > maxRedirectDepth {
		return nil, ErrNotFound
	}
	_, key, ok := d.findEntry(word)
	if !ok {
		return nil, ErrNotFound
	}
	identity := NormalizeExactKey(key)
	if _, looped := seen[identity]; looped {
		return nil, ErrNotFound
	}
	branchSeen := make(map[string]struct{}, len(seen)+1)
	for visited := range seen {
		branchSeen[visited] = struct{}{}
	}
	branchSeen[identity] = struct{}{}

	entries := d.exactRecords(key)
	var out []*LookupResult
	var firstErr error
	for index, entry := range entries {
		content, err := d.mdx.ResolveEntry(entry)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		target, redirect := ParseLinkTarget(content)
		if redirect {
			resolved, resolveErr := d.resolveRecords(target, original, depth+1, branchSeen)
			if resolveErr != nil {
				if firstErr == nil && !errors.Is(resolveErr, ErrNotFound) {
					firstErr = resolveErr
				}
				continue
			}
			out = append(out, resolved...)
			continue
		}
		result := &LookupResult{
			MatchedKey:        key,
			HTML:              content,
			RawRecordOrdinal:  index + 1,
			RecordStartOffset: entry.RecordStartOffset,
		}
		if depth > 0 {
			result.RedirectedFrom = original
		}
		out = append(out, result)
	}
	if len(out) > 0 {
		return out, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, ErrNotFound
}

func (d *Dictionary) exactRecords(key string) []*mdx.MDictKeywordEntry {
	first := d.exact[key]
	if first == nil {
		return nil
	}
	extras := d.exactExtras[key]
	entries := make([]*mdx.MDictKeywordEntry, 1, 1+len(extras))
	entries[0] = first
	return append(entries, extras...)
}

// findEntry tries exact, then Unicode-normalized, then case-insensitive match.
// The caller must hold at least a read lock.
func (d *Dictionary) findEntry(word string) (*mdx.MDictKeywordEntry, string, bool) {
	word = strings.TrimSpace(word)
	if word == "" || d.exact == nil {
		return nil, "", false
	}
	if entry, ok := d.exact[word]; ok {
		return entry, entry.KeyWord, true
	}
	// NFC is the form MDict files overwhelmingly use, while text selected from
	// browsers and PDFs is frequently NFD.
	if nfc := NormalizeExactKey(word); nfc != word {
		if entry, ok := d.exact[nfc]; ok {
			return entry, entry.KeyWord, true
		}
	}
	folded := foldKey(word)
	if entry, ok := d.exact[folded]; ok {
		return entry, entry.KeyWord, true
	}
	if entry, ok := d.folded[folded]; ok {
		return entry, entry.KeyWord, true
	}
	return nil, "", false
}

// Resource resolves an MDD resource reference such as "sound://synthetic/uk/example.mp3"
// to its raw bytes. Lookup is O(1) against the prebuilt volume index.
func (d *Dictionary) Resource(ref string) ([]byte, error) {
	if err := d.Load(); err != nil {
		return nil, err
	}
	d.mu.RLock()
	idx := d.mddIdx
	volumes := d.mdds
	d.mu.RUnlock()
	if len(volumes) == 0 {
		return nil, ErrNotFound
	}

	for _, candidate := range ResourceCandidates(ref) {
		loc, ok := idx[candidate]
		if !ok {
			continue
		}
		if loc.volume < 0 || loc.volume >= len(volumes) || loc.entry == nil {
			continue
		}
		data, err := volumes[loc.volume].LocateByKeywordEntry(loc.entry)
		if err != nil {
			continue
		}
		return data, nil
	}
	return nil, ErrNotFound
}

// HasResource reports whether a reference resolves, without decompressing it.
// The audio pipeline uses this to avoid advertising pronunciations that the
// user's MDD cannot actually deliver.
func (d *Dictionary) HasResource(ref string) bool {
	if err := d.Load(); err != nil {
		return false
	}
	d.mu.RLock()
	idx := d.mddIdx
	d.mu.RUnlock()
	for _, candidate := range ResourceCandidates(ref) {
		if _, ok := idx[candidate]; ok {
			return true
		}
	}
	return false
}

// ResourceKinds counts the MDD resources by file extension.
//
// It is what makes "this dictionary has 100k recordings but no images" a
// statement the service can actually make, rather than a guess.
func (d *Dictionary) ResourceKinds() map[string]int {
	if err := d.Load(); err != nil {
		return nil
	}
	d.mu.RLock()
	idx := d.mddIdx
	d.mu.RUnlock()

	counts := make(map[string]int)
	for key := range idx {
		ext := strings.ToLower(path.Ext(key))
		if ext == "" {
			ext = "(none)"
		}
		counts[ext]++
	}
	return counts
}

// Prefix returns up to limit original headword spellings starting with the
// prefix. Matching is case-insensitive; returned spelling is never folded.
func (d *Dictionary) Prefix(prefix string, limit int) []string {
	if err := d.Load(); err != nil {
		return nil
	}
	d.mu.RLock()
	dict := d.mdx
	d.mu.RUnlock()
	if dict == nil || limit <= 0 {
		return nil
	}
	entries, err := dict.GetKeyWordEntries()
	if err != nil {
		return nil
	}
	lower := foldKey(prefix)
	if lower == "" {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if strings.HasPrefix(foldKey(entry.KeyWord), lower) {
			out = append(out, entry.KeyWord)
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// Close releases the underlying file handles.
func (d *Dictionary) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mdx = nil
	d.mdds = nil
	d.mddIdx = nil
	d.exact = nil
	d.exactExtras = nil
	d.folded = nil
}

var _ = os.ErrNotExist
