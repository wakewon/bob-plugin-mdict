package service

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/htmlmd"
	"github.com/wakewon/bob-plugin-mdict/internal/linkhandler"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/parser"
	"github.com/wakewon/bob-plugin-mdict/internal/playback"
	"golang.org/x/net/html"
)

// HTMLMarkdownOptions configures the dictionary-layout Markdown view. It reads
// the same record selection fields as every other presentation, so a selector
// names the same record whichever view the reader chose.
type HTMLMarkdownOptions struct {
	MultiRecordMode htmlmd.MultiRecordMode
	RecordOrdinal   int
}

// maxStylesheetBytes bounds a stylesheet read from a dictionary folder. Real
// dictionary stylesheets are tens of kilobytes; anything far larger is not
// one.
const maxStylesheetBytes = 4 << 20

// renderHTMLMarkdown converts the records of an EntrySet from their own HTML.
//
// The EntrySet still decides which records exist and in what order: a record
// the parser found empty is not shown here either, so `run²` is the same
// record in the dictionary-layout view as on the Bob card. Only the HTML is
// re-read, because the EntrySet cache deliberately does not keep it.
func (s *Service) renderHTMLMarkdown(dict *mdict.Dictionary, set *entryir.EntrySet, opts HTMLMarkdownOptions, settings playback.Settings) string {
	if dict == nil || set == nil || len(set.Records) == 0 {
		return ""
	}
	lookupSet, err := dict.LookupAll(set.LookupKey)
	if err != nil {
		return ""
	}
	type identity struct {
		key    string
		offset int64
	}
	raw := make(map[identity][]byte, len(lookupSet.Records))
	for _, record := range lookupSet.Records {
		raw[identity{record.MatchedKey, record.RecordStartOffset}] = record.HTML
	}
	records := make([]htmlmd.Record, 0, len(set.Records))
	for index, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		source := record.Entry.Source
		content, ok := raw[identity{source.MatchedKey, source.RecordStartOffset}]
		if !ok {
			continue
		}
		ordinal := record.RecordOrdinal
		if ordinal <= 0 {
			ordinal = index + 1
		}
		records = append(records, htmlmd.Record{Ordinal: ordinal, HTML: content})
	}

	profile := s.profileFor(dict)
	resources := &htmlResources{svc: s, dict: dict, playback: settings}
	var extra []*htmlmd.Stylesheet
	if sheet := resources.profileStylesheet(profile); sheet != nil {
		extra = append(extra, sheet)
	}
	// The page is named as the reader would select it, record and all, so a
	// step to "wound²" and the page it opens agree.
	page := set.LookupKey
	if opts.RecordOrdinal > 0 {
		page = htmlmd.Selector(set.LookupKey, opts.RecordOrdinal)
	}
	var lookupLink func(string) string
	if s.LookupLinksEnabled() {
		lookupLink = func(query string) string { return linkhandler.NavigateURL(query, page, s.cfg.Port) }
	}
	document := htmlmd.RenderRecords(records, htmlmd.RecordOptions{
		Options: htmlmd.Options{
			Resources:   resources,
			Stylesheets: extra,
			LookupLink:  lookupLink,
			Scope: func(doc *html.Node) *html.Node {
				return profile.PresentationScope(doc)
			},
		},
		Key:             set.LookupKey,
		Total:           len(set.Records),
		MultiRecordMode: opts.MultiRecordMode,
		RecordOrdinal:   opts.RecordOrdinal,
	})
	if document == "" || lookupLink == nil {
		return document
	}
	// The way back goes last, after everything the dictionary says.
	if previous := s.nav.previous(page); previous != "" {
		if back := linkhandler.BackURL(previous, s.cfg.Port); back != "" {
			document = strings.TrimRight(document, "\n") + "\n\n---\n\n" + htmlmd.Link("← "+previous, back) + "\n"
		}
	}
	return document
}

// htmlResources resolves a record's references against its own dictionary,
// through the same opaque-token resource path as the IR presentations.
type htmlResources struct {
	svc      *Service
	dict     *mdict.Dictionary
	playback playback.Settings
}

func (r *htmlResources) Audio(ref string) string {
	audio := r.svc.audioResolver(r.dict).ResolveAudio(ref)
	if audio == nil {
		return ""
	}
	if play := r.svc.playURL(audio, r.playback); play != "" {
		return play
	}
	return audio.URL
}

func (r *htmlResources) Image(ref string) string {
	if image := r.svc.imageResolver(r.dict).ResolveImage(ref, ""); image != nil {
		return image.URL
	}
	return ""
}

// Stylesheet parses a stylesheet once per dictionary and keeps the result,
// including the fact that it could not be found.
func (r *htmlResources) Stylesheet(href string) *htmlmd.Stylesheet {
	ref := stylesheetPath(href)
	if ref == "" {
		return nil
	}
	id := r.dict.ID()
	r.svc.styleMu.Lock()
	sheet, ok := r.svc.styleCache[id][strings.ToLower(ref)]
	r.svc.styleMu.Unlock()
	if ok {
		return sheet
	}
	if data := r.readStylesheet(ref); data != nil {
		sheet = htmlmd.ParseStylesheet(data)
	}
	r.svc.styleMu.Lock()
	if r.svc.styleCache[id] == nil {
		r.svc.styleCache[id] = make(map[string]*htmlmd.Stylesheet)
	}
	r.svc.styleCache[id][strings.ToLower(ref)] = sheet
	r.svc.styleMu.Unlock()
	return sheet
}

// profileStylesheetKey caches a profile's presentation CSS beside the
// dictionary's own sheets. It cannot collide with a path, which never
// contains a NUL.
const profileStylesheetKey = "\x00profile"

func (r *htmlResources) profileStylesheet(profile *parser.Profile) *htmlmd.Stylesheet {
	if profile == nil || strings.TrimSpace(profile.PresentationCSS) == "" {
		return nil
	}
	id := r.dict.ID()
	r.svc.styleMu.Lock()
	defer r.svc.styleMu.Unlock()
	if sheet, ok := r.svc.styleCache[id][profileStylesheetKey]; ok {
		return sheet
	}
	sheet := htmlmd.ParseStylesheet([]byte(profile.PresentationCSS))
	if r.svc.styleCache[id] == nil {
		r.svc.styleCache[id] = make(map[string]*htmlmd.Stylesheet)
	}
	r.svc.styleCache[id][profileStylesheetKey] = sheet
	return sheet
}

// stylesheetPath reduces a <link href> to a dictionary-relative path, or ""
// for anything that is not a local stylesheet. Remote stylesheets are never
// fetched: rendering a record makes no network requests.
func stylesheetPath(href string) string {
	ref := strings.TrimSpace(href)
	lower := strings.ToLower(ref)
	if ref == "" || strings.Contains(lower, "://") || strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "//") {
		return ""
	}
	if index := strings.IndexAny(ref, "?#"); index >= 0 {
		ref = ref[:index]
	}
	ref = strings.TrimLeft(strings.ReplaceAll(ref, "\\", "/"), "/")
	ref = path.Clean(ref)
	if ref == "." || strings.HasPrefix(ref, "../") || ref == ".." || !strings.EqualFold(path.Ext(ref), ".css") {
		return ""
	}
	return ref
}

// stylesheetFile returns the dictionary-folder file a stylesheet path names,
// or "" when there is none. Only a regular .css file inside the dictionary's
// own folder qualifies.
func stylesheetFile(dict *mdict.Dictionary, ref string) string {
	dir := filepath.Dir(dict.SourcePath())
	for _, candidate := range []string{ref, path.Base(ref)} {
		full := filepath.Join(dir, filepath.FromSlash(candidate))
		if rel, err := filepath.Rel(dir, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxStylesheetBytes {
			continue
		}
		return full
	}
	return ""
}

// missingStylesheets collects the local stylesheets sampled records link to
// that resolve neither beside the MDX nor in an MDD, in first-seen order.
func missingStylesheets(dict *mdict.Dictionary, samples []diagnose.Sample) []string {
	var missing []string
	seen := make(map[string]bool)
	for _, sample := range samples {
		htmlmd.StylesheetLinks(sample.Doc, func(href string) {
			ref := stylesheetPath(href)
			if ref == "" || seen[strings.ToLower(ref)] {
				return
			}
			seen[strings.ToLower(ref)] = true
			if stylesheetFile(dict, ref) == "" && !dict.HasResource(ref) {
				missing = append(missing, ref)
			}
		})
	}
	return missing
}

// readStylesheet looks next to the MDX first, then in the MDD. That is the
// order MDict itself uses, and it is how readers customise a dictionary's
// look: by dropping an edited stylesheet beside it.
//
// Only a .css file inside the dictionary's own folder can be read, and its
// bytes never leave the process — only the handful of properties the
// converter interprets reach the output.
func (r *htmlResources) readStylesheet(ref string) []byte {
	if full := stylesheetFile(r.dict, ref); full != "" {
		if data, err := os.ReadFile(full); err == nil {
			return data
		}
	}
	if data, err := r.dict.Resource(ref); err == nil && len(data) <= maxStylesheetBytes {
		return data
	}
	return nil
}
