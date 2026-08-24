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
	"strings"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/bobadapter"
	"github.com/wakewon/bob-plugin-mdict/internal/mdict"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
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
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/dictionaries", s.handleDictionaries)
	mux.HandleFunc("POST /v1/lookup", s.handleLookup)
	mux.HandleFunc("POST /v1/rescan", s.handleRescan)
	mux.HandleFunc("GET /v1/resource/{token}", s.handleResource)
	mux.HandleFunc("HEAD /v1/resource/{token}", s.handleResource)
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
	Service                string  `json:"service"`
	ServiceVersion         string  `json:"serviceVersion"`
	APIVersion             string  `json:"apiVersion"`
	Platform               string  `json:"platform"`
	Architecture           string  `json:"architecture"`
	DictionaryDirectory    string  `json:"dictionaryDirectory"`
	DictionaryCount        int     `json:"dictionaryCount"`
	HealthyDictionaryCount int     `json:"healthyDictionaryCount"`
	AudioAvailable         bool    `json:"audioAvailable"`
	SpeexAvailable         bool    `json:"speexAvailable"`
	SpeexDecoder           string  `json:"speexDecoder,omitempty"`
	UptimeSeconds          float64 `json:"uptimeSeconds"`
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
		APIVersion:             version.APIVersion,
		Platform:               runtime.GOOS,
		Architecture:           runtime.GOARCH,
		DictionaryDirectory:    s.svc.Config().DictionaryDir,
		DictionaryCount:        total,
		HealthyDictionaryCount: healthy,
		AudioAvailable:         audioAvailable,
		SpeexAvailable:         s.svc.Transcoder().SpeexAvailable(),
		SpeexDecoder:           s.svc.Transcoder().DecoderName(),
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
		infos = append(infos, info)
	}
	writeJSON(w, http.StatusOK, DictionariesResponse{
		Directory:    s.svc.Config().DictionaryDir,
		Dictionaries: infos,
		Generated:    time.Now(),
	})
}

// LookupRequest is the body of POST /v1/lookup.
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
	// Format is "ir" (default) or "bob". "bob" adds a rendered toDict so the
	// plugin can pass it straight to Bob without interpreting the IR.
	Format string `json:"format,omitempty"`
	// IncludeExamples and IncludeExtras let the user trim what Bob displays.
	IncludeExamples *bool `json:"includeExamples,omitempty"`
	IncludeExtras   *bool `json:"includeExtras,omitempty"`
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

	mode := service.ModeExact
	if strings.EqualFold(req.Mode, string(service.ModeSmart)) {
		mode = service.ModeSmart
	}

	bobOpts := bobadapter.DefaultOptions()
	if req.MaxExamples > 0 {
		bobOpts.MaxExamplesPerPart = req.MaxExamples
	}
	if req.IncludeExamples != nil {
		bobOpts.IncludeExamples = *req.IncludeExamples
	}
	if req.IncludeExtras != nil {
		bobOpts.IncludeExtras = *req.IncludeExtras
	}

	result, err := s.svc.Lookup(req.Query, service.LookupOptions{
		DictionaryIDs: req.Dictionaries,
		Mode:          mode,
		Limit:         req.Limit,
		MaxExamples:   req.MaxExamples,
		Debug:         req.Debug,
		RenderBob:     strings.EqualFold(req.Format, "bob"),
		BobOptions:    bobOpts,
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
