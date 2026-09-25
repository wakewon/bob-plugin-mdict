// Package httpapi exposes the service over loopback HTTP.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/htmlmd"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/mdrender"
	"github.com/wakewon/bob-plugin-mdict/internal/playback"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/textrender"
	"github.com/wakewon/bob-plugin-mdict/internal/version"
)

// maxRequestBytes bounds a lookup request body. Bob sends a selected word, not
// a document, so anything larger is a bug or an abuse.
const maxRequestBytes = 64 << 10

// Server routes HTTP requests to the service.
type Server struct {
	svc *service.Service
	log *slog.Logger
}

// New creates a server.
func New(svc *service.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// Handler builds the routed handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/status", s.handleStatus)
	mux.HandleFunc("GET /v2/dictionaries", s.handleDictionaries)
	mux.HandleFunc("POST /v2/lookup", s.handleLookup)
	mux.HandleFunc("POST /v2/rescan", s.handleRescan)
	mux.HandleFunc("GET /v2/resource/{token}", s.handleResource)
	mux.HandleFunc("HEAD /v2/resource/{token}", s.handleResource)
	mux.HandleFunc("POST /v2/audio/{token}", s.handleAudio)
	mux.HandleFunc("POST /v2/navigation", s.handleNavigation)
	return s.withGuards(mux)
}

// withGuards applies the cross-cutting protections every route needs.
func (s *Server) withGuards(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The service binds to loopback, but a bound socket is not by itself a
		// guarantee about who connected, so the origin is checked too.
		if !isLoopbackRequest(r) {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		// A browser page could otherwise POST here from any site. Requiring an
		// absent or loopback Origin blocks drive-by requests without needing
		// credentials the user would have to configure.
		if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
			http.Error(w, "cross-origin requests are not accepted", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || host == ""
}

// loopbackHosts are the only hostnames an Origin header may name.
var loopbackHosts = map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}

// isLoopbackOrigin reports whether an Origin header names this machine.
//
// The host is compared after parsing rather than by prefix: a prefix test
// accepts "http://127.0.0.1.evil.example", which is an attacker-controlled
// domain that merely starts with the loopback address.
func isLoopbackOrigin(origin string) bool {
	if strings.EqualFold(origin, "null") {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return loopbackHosts[strings.ToLower(parsed.Hostname())]
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// ErrorResponse is the uniform error shape the plugin understands.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	// Hint tells the user what to actually do about it.
	Hint string `json:"hint,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message, hint string) {
	writeJSON(w, status, ErrorResponse{Error: code, Message: message, Hint: hint})
}

// StatusResponse describes the running service.
type StatusResponse struct {
	Service                string `json:"service"`
	ServiceVersion         string `json:"serviceVersion"`
	BuildCommit            string `json:"buildCommit"`
	APIVersion             string `json:"apiVersion"`
	Platform               string `json:"platform"`
	Architecture           string `json:"architecture"`
	DictionaryDirectory    string `json:"dictionaryDirectory"`
	DictionaryCount        int    `json:"dictionaryCount"`
	HealthyDictionaryCount int    `json:"healthyDictionaryCount"`
	AudioAvailable         bool   `json:"audioAvailable"`
	SpeexAvailable         bool   `json:"speexAvailable"`
	SpeexDecoder           string `json:"speexDecoder,omitempty"`
	// LookupLinks reports whether dictionary links in the web-layout view
	// are clickable Bob lookups rather than text.
	LookupLinks   bool    `json:"lookupLinks"`
	UptimeSeconds float64 `json:"uptimeSeconds"`
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	total, healthy := s.svc.Registry().Counts()
	audioAvailable := false
	for _, dict := range s.svc.Registry().All() {
		if dict.Info().HasMDD {
			audioAvailable = true
			break
		}
	}
	writeJSON(w, http.StatusOK, StatusResponse{
		Service:                "bob-mdict",
		ServiceVersion:         version.Version,
		BuildCommit:            version.Commit,
		APIVersion:             version.APIVersion,
		Platform:               runtime.GOOS,
		Architecture:           runtime.GOARCH,
		DictionaryDirectory:    s.svc.Config().DictionaryDir,
		DictionaryCount:        total,
		HealthyDictionaryCount: healthy,
		AudioAvailable:         audioAvailable,
		SpeexAvailable:         s.svc.Transcoder().SpeexAvailable(),
		SpeexDecoder:           s.svc.Transcoder().DecoderName(),
		LookupLinks:            s.svc.LookupLinksEnabled(),
		UptimeSeconds:          s.svc.Uptime().Seconds(),
	})
}

// DictionariesResponse lists the installed dictionaries.
type DictionariesResponse struct {
	Directory    string        `json:"directory"`
	Dictionaries []mdict.Info  `json:"dictionaries"`
	Generated    time.Time     `json:"generated"`
	Elapsed      time.Duration `json:"-"`
}

func (s *Server) handleDictionaries(w http.ResponseWriter, _ *http.Request) {
	dicts := s.svc.Registry().All()
	infos := make([]mdict.Info, 0, len(dicts))
	for _, dict := range dicts {
		info := dict.Info()
		info.Profile = s.svc.ProfileID(dict)
		info.MissingStylesheets = s.svc.MissingStylesheets(dict)
		infos = append(infos, info)
	}
	writeJSON(w, http.StatusOK, DictionariesResponse{
		Directory:    s.svc.Config().DictionaryDir,
		Dictionaries: infos,
		Generated:    time.Now(),
	})
}

// LookupRequest is the body of POST /v2/lookup.
type LookupRequest struct {
	Query string `json:"query"`
	// Dictionaries restricts the search; empty means every dictionary.
	Dictionaries []string `json:"dictionaries,omitempty"`
	// Mode is "exact" or "smart".
	Mode string `json:"mode,omitempty"`
	// Limit caps how many dictionaries answer.
	Limit int `json:"limit,omitempty"`
	// MaxExamples caps examples per sense.
	MaxExamples int  `json:"maxExamples,omitempty"`
	Debug       bool `json:"debug,omitempty"`
	// Format is "ir" (default), "bob", "plain", or "markdown". Presentation formats add
	// a rendered sibling field while preserving the canonical IR matches.
	Format string `json:"format,omitempty"`
	// MarkdownSource chooses where format:"markdown" comes from: "entry"
	// (default) renders the parsed EntrySet; "html" converts the record's own
	// HTML and stylesheets, keeping the dictionary's layout. Example, extras
	// and grammar options do not apply to "html": it shows the page as
	// published. Older services ignore the field and return "entry".
	MarkdownSource string `json:"markdownSource,omitempty"`
	// IncludeExamples and IncludeExtras let the user trim what Bob displays.
	IncludeExamples *bool `json:"includeExamples,omitempty"`
	IncludeExtras   *bool `json:"includeExtras,omitempty"`
	// IncludeGrammar lets the user hide detailed grammatical qualifiers from presentation.
	IncludeGrammar *bool `json:"includeGrammar,omitempty"`
	// MultiRecordMode controls presentation only, in both the Bob card and
	// Plain/Markdown: "separate" selects one semantic record and offers sibling
	// navigation; "combined" renders every record with explicit boundaries.
	MultiRecordMode string `json:"multiRecordMode,omitempty"`
	// RecordOrdinal is one-based over the visible, deduplicated EntrySet.
	RecordOrdinal int `json:"recordOrdinal,omitempty"`
	// AudioVolume and AudioRate are percentages (default 100) and
	// AudioNormalize matches recordings' loudness (default true). They are
	// carried by 🔊 links that play in the background; they change nothing
	// when those links are not available.
	AudioVolume    int   `json:"audioVolume,omitempty"`
	AudioRate      int   `json:"audioRate,omitempty"`
	AudioNormalize *bool `json:"audioNormalize,omitempty"`
}

func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	var req LookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", "request body is not valid JSON", "")
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "badRequest", "query is required", "")
		return
	}
	if req.RecordOrdinal < 0 {
		writeError(w, http.StatusBadRequest, "badRequest", "recordOrdinal must be zero or a positive integer", "")
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "ir"
	}
	if format != "ir" && format != "bob" && format != "plain" && format != "markdown" {
		writeError(w, http.StatusBadRequest, "badRequest", "format must be ir, bob, plain, or markdown", "")
		return
	}
	markdownSource := strings.ToLower(strings.TrimSpace(req.MarkdownSource))
	if markdownSource != "" && markdownSource != "entry" && markdownSource != "html" {
		writeError(w, http.StatusBadRequest, "badRequest", "markdownSource must be entry or html", "")
		return
	}
	htmlMarkdown := format == "markdown" && markdownSource == "html"
	if req.MultiRecordMode != "" &&
		!strings.EqualFold(req.MultiRecordMode, string(bobadapter.MultiRecordSeparate)) &&
		!strings.EqualFold(req.MultiRecordMode, string(bobadapter.MultiRecordCombined)) {
		writeError(w, http.StatusBadRequest, "badRequest", "multiRecordMode must be separate or combined", "")
		return
	}

	mode := service.ModeExact
	if strings.EqualFold(req.Mode, string(service.ModeSmart)) {
		mode = service.ModeSmart
	}

	bobOpts := bobadapter.DefaultOptions()
	if req.MaxExamples > 0 {
		bobOpts.MaxExamplesPerSense = req.MaxExamples
	}
	if req.IncludeExamples != nil {
		bobOpts.IncludeExamples = *req.IncludeExamples
	}
	if req.IncludeExtras != nil {
		bobOpts.IncludeExtras = *req.IncludeExtras
	}
	if req.IncludeGrammar != nil {
		bobOpts.IncludeGrammar = *req.IncludeGrammar
	}
	if strings.EqualFold(req.MultiRecordMode, string(bobadapter.MultiRecordCombined)) {
		bobOpts.MultiRecordMode = bobadapter.MultiRecordCombined
	} else {
		bobOpts.MultiRecordMode = bobadapter.MultiRecordSeparate
	}
	bobOpts.RecordOrdinal = req.RecordOrdinal
	// Markdown reads the same request fields as the Bob card. The two
	// presentations differ in what they can draw, never in what the user asked
	// for, so multiRecordMode is mapped rather than reinterpreted.
	markdownOpts := mdrender.UserOptions()
	markdownOpts.MaxExamplesPerSense = bobOpts.MaxExamplesPerSense
	markdownOpts.IncludeExamples = bobOpts.IncludeExamples
	markdownOpts.IncludeExtras = bobOpts.IncludeExtras
	markdownOpts.IncludeGrammar = bobOpts.IncludeGrammar
	markdownOpts.RecordOrdinal = req.RecordOrdinal
	if bobOpts.MultiRecordMode == bobadapter.MultiRecordCombined {
		markdownOpts.MultiRecordMode = mdrender.MultiRecordCombined
	} else {
		markdownOpts.MultiRecordMode = mdrender.MultiRecordSeparate
	}
	plainOpts := textrender.UserOptions()
	plainOpts.MaxExamplesPerSense = bobOpts.MaxExamplesPerSense
	plainOpts.IncludeExamples = bobOpts.IncludeExamples
	plainOpts.IncludeExtras = bobOpts.IncludeExtras
	plainOpts.IncludeGrammar = bobOpts.IncludeGrammar
	plainOpts.RecordOrdinal = req.RecordOrdinal
	if bobOpts.MultiRecordMode == bobadapter.MultiRecordCombined {
		plainOpts.MultiRecordMode = textrender.MultiRecordCombined
	} else {
		plainOpts.MultiRecordMode = textrender.MultiRecordSeparate
	}

	htmlMarkdownOpts := service.HTMLMarkdownOptions{
		MultiRecordMode: htmlmd.MultiRecordSeparate,
		RecordOrdinal:   req.RecordOrdinal,
	}
	if bobOpts.MultiRecordMode == bobadapter.MultiRecordCombined {
		htmlMarkdownOpts.MultiRecordMode = htmlmd.MultiRecordCombined
	}

	result, err := s.svc.Lookup(req.Query, service.LookupOptions{
		DictionaryIDs:   req.Dictionaries,
		Mode:            mode,
		Limit:           req.Limit,
		MaxExamples:     req.MaxExamples,
		Debug:           req.Debug,
		RenderBob:       format == "bob",
		BobOptions:      bobOpts,
		RenderMarkdown:  format == "markdown" && !htmlMarkdown,
		MarkdownOptions: markdownOpts,
		RenderPlain:     format == "plain",
		PlainOptions:    plainOpts,

		RenderHTMLMarkdown:  htmlMarkdown,
		HTMLMarkdownOptions: htmlMarkdownOpts,
		Playback:            playbackSettings(req.AudioVolume, req.AudioRate, req.AudioNormalize),
	})
	if err != nil {
		if errors.Is(err, service.ErrNoDictionaries) {
			writeError(w, http.StatusServiceUnavailable, "noDictionaries",
				"no dictionaries are installed",
				fmt.Sprintf("Copy a folder containing .mdx/.mdd files into %s, then rescan.",
					s.svc.Config().DictionaryDir))
			return
		}
		if errors.Is(err, service.ErrDictionaryNotFound) {
			writeError(w, http.StatusNotFound, "dictionaryNotFound", err.Error(),
				"Query /list in Bob to see the current dictionaries and IDs.")
			return
		}
		if errors.Is(err, service.ErrDictionaryUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "dictionaryUnavailable", err.Error(),
				"Query /list in Bob to see diagnostics, or choose another dictionary ID.")
			return
		}
		if errors.Is(err, service.ErrRecordNotFound) {
			var detail *service.RecordNotFoundError
			if errors.As(err, &detail) {
				writeError(w, http.StatusNotFound, "recordNotFound",
					fmt.Sprintf("“%s” 只有 %d 个可用词条记录。", detail.Query, detail.Available),
					fmt.Sprintf("请选择第 1 到第 %d 条记录。", detail.Available))
				return
			}
		}
		writeError(w, http.StatusBadRequest, "badRequest", err.Error(), "")
		return
	}
	if len(result.Matches) == 0 {
		writeJSON(w, http.StatusNotFound, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRescan(w http.ResponseWriter, _ *http.Request) {
	// Rescan takes no path argument by design: the directory it walks is fixed
	// by configuration, so this endpoint cannot be pointed at the filesystem.
	started := time.Now()
	if err := s.svc.Rescan(); err != nil {
		writeError(w, http.StatusInternalServerError, "rescanFailed", err.Error(), "")
		return
	}
	total, healthy := s.svc.Registry().Counts()
	writeJSON(w, http.StatusOK, map[string]any{
		"dictionaryCount":        total,
		"healthyDictionaryCount": healthy,
		"elapsedSeconds":         time.Since(started).Seconds(),
	})
}

// handleAudio returns a recording prepared for the link helper, which plays
// it in its own audio engine so pronunciation needs no browser: decoded to
// one format, matched in loudness, at the reader's volume and, when asked,
// after a lead-in of silence. It is POST so that following a link can never
// reach it, and the guards reject any browser page that tries.
func (s *Server) handleAudio(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	volume, _ := strconv.Atoi(query.Get("volume"))
	rate, _ := strconv.Atoi(query.Get("rate"))
	var normalize *bool
	if value := query.Get("normalize"); value != "" {
		flag := value == "1"
		normalize = &flag
	}
	settings := playbackSettings(volume, rate, normalize)
	settings.LeadIn, _ = strconv.Atoi(query.Get("leadin"))
	audio, err := s.svc.PrepareAudio(r.PathValue("token"), settings.Clamped())
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(audio)
	case errors.Is(err, playback.ErrUnsupported):
		writeError(w, http.StatusUnsupportedMediaType, "unsupportedAudio", "this recording cannot be played on this system", "")
	case errors.Is(err, mdict.ErrNotFound):
		writeError(w, http.StatusNotFound, "resourceNotFound", "resource not found", "")
	default:
		if _, _, resolveErr := s.svc.ResolveResource(r.PathValue("token")); resolveErr == nil {
			s.log.Warn("audio preparation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "audioFailed", "the recording could not be prepared", "")
			return
		}
		writeError(w, http.StatusBadRequest, "badToken", "invalid resource token", "")
	}
}

// maxNavigationRunes matches what a lookup link may carry.
const maxNavigationRunes = 200

// handleNavigation records a step the reader took through a dictionary link,
// reported by the link helper just before it asks Bob to look the word up:
// to=…&from=… for a link, to=…&back=1 for the way back.
func (s *Server) handleNavigation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", "navigation must be a form", "")
		return
	}
	to := strings.TrimSpace(r.PostFormValue("to"))
	from := strings.TrimSpace(r.PostFormValue("from"))
	back := r.PostFormValue("back") == "1"
	if to == "" || (!back && from == "") ||
		len([]rune(to)) > maxNavigationRunes || len([]rune(from)) > maxNavigationRunes {
		writeError(w, http.StatusBadRequest, "badRequest", "navigation needs to and either from or back=1", "")
		return
	}
	s.svc.Navigate(from, to, back)
	w.WriteHeader(http.StatusNoContent)
}

// playbackSettings fills unset values with the defaults and clamps the rest.
func playbackSettings(volume, rate int, normalize *bool) playback.Settings {
	settings := playback.Defaults()
	if volume > 0 {
		settings.Volume = volume
	}
	if rate > 0 {
		settings.Rate = rate
	}
	if normalize != nil {
		settings.Normalize = *normalize
	}
	return settings.Clamped()
}

func (s *Server) handleResource(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	data, contentType, err := s.svc.ResolveResource(token)
	if err != nil {
		status := http.StatusNotFound
		code := "resourceNotFound"
		message := "resource not found"
		if errors.Is(err, mdict.ErrNotFound) {
			// Keep the default.
		} else if strings.Contains(err.Error(), "speex") {
			status = http.StatusServiceUnavailable
			code = "speexUnavailable"
			message = "this pronunciation is stored as Ogg-Speex and no decoder is installed"
		} else {
			status = http.StatusBadRequest
			code = "badToken"
			message = "invalid resource token"
		}
		writeError(w, status, code, message, "")
		return
	}

	w.Header().Set("Content-Type", contentType)
	// Resource bytes are immutable for the life of the token, so aggressive
	// caching is safe and keeps repeat playback instant.
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	w.Header().Set("ETag", etagFor(data))
	w.Header().Set("Accept-Ranges", "bytes")

	if match := r.Header.Get("If-None-Match"); match != "" && match == etagFor(data) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, newByteSeeker(data))
}
