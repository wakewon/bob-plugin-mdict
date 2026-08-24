// Command bob-mdict is the local companion service for the MDict Bob plugin.
//
// It reads the user's own .mdx/.mdd dictionaries, parses entries into a
// structured form, and serves them over loopback HTTP. It never contacts the
// network, never bundles dictionary data, and never synthesizes speech.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/httpapi"
	"github.com/wakewon/bob-plugin-mdict/internal/service"
	"github.com/wakewon/bob-plugin-mdict/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bob-mdict:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		showVersion  = flag.Bool("version", false, "print the version and exit")
		check        = flag.Bool("check", false, "verify the installation and exit")
		listDicts    = flag.Bool("list-dictionaries", false, "list discovered dictionaries and exit")
		rescanOnly   = flag.Bool("rescan", false, "rescan dictionaries, report the result and exit")
		debugLookup  = flag.String("debug-lookup", "", "parse one word, print the EntrySet IR as JSON, and exit")
		dictionaries = flag.String("dictionary-dir", "", "override the dictionary directory")
		port         = flag.Int("port", 0, "override the loopback port")
		debug        = flag.Bool("debug", false, "enable verbose logging")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("bob-mdict %s (%s) api=%s\n", version.Version, version.Commit, version.APIVersion)
		return nil
	}

	cfg := config.Default()
	if *dictionaries != "" {
		cfg.DictionaryDir = *dictionaries
	}
	if *port > 0 {
		cfg.Port = *port
	}
	if *debug {
		cfg.Debug = true
	}
	tuneGC()

	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("prepare directories: %w", err)
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	svc, err := service.New(cfg)
	if err != nil {
		return err
	}

	switch {
	case *check:
		return runCheck(svc)
	case *listDicts:
		return runList(svc)
	case *rescanOnly:
		return runRescan(svc)
	case *debugLookup != "":
		return runDebugLookup(svc, *debugLookup)
	}

	return serve(svc, log)
}

// loadDictionaries scans and indexes, reporting how long it took.
func loadDictionaries(svc *service.Service) time.Duration {
	started := time.Now()
	if err := svc.Rescan(); err != nil {
		fmt.Fprintln(os.Stderr, "scan failed:", err)
	}
	// Index building allocates far more than it retains. Returning the
	// difference to the OS matters here in a way it would not for a batch job:
	// this process then sits idle in the background for days.
	debug.FreeOSMemory()
	return time.Since(started)
}

// tuneGC trades a little CPU for a much smaller resident footprint.
//
// The live set is large and almost entirely static — dictionary indexes that
// never change — while each lookup produces a burst of short-lived garbage
// from parsing an entry. At the default GOGC the heap is allowed to grow to
// twice a live set of hundreds of megabytes before collecting, so a handful of
// lookups permanently inflate the process.
func tuneGC() {
	debug.SetGCPercent(30)
}

func runCheck(svc *service.Service) error {
	elapsed := loadDictionaries(svc)
	cfg := svc.Config()
	total, healthy := svc.Registry().Counts()

	fmt.Printf("bob-mdict %s (api %s)\n", version.Version, version.APIVersion)
	fmt.Printf("dictionary directory : %s\n", cfg.DictionaryDir)
	fmt.Printf("cache directory      : %s\n", cfg.CacheDir)
	fmt.Printf("listen               : 127.0.0.1:%d\n", cfg.Port)
	fmt.Printf("dictionaries         : %d found, %d healthy (scanned in %s)\n", total, healthy, elapsed.Round(time.Millisecond))

	if svc.Transcoder().SpeexAvailable() {
		fmt.Printf("speex decoder        : %s\n", svc.Transcoder().DecoderName())
	} else {
		fmt.Printf("speex decoder        : not installed (Ogg-Speex pronunciations will be hidden)\n")
		fmt.Printf("                       install it with: brew install speex\n")
	}

	if total == 0 {
		fmt.Printf("\nNo dictionaries yet. Copy a folder containing .mdx (and .mdd) files into:\n  %s\n", cfg.DictionaryDir)
		return nil
	}
	for _, dict := range svc.Registry().All() {
		info := dict.Info()
		status := "ok"
		if info.Health != "ok" {
			status = "UNAVAILABLE"
		}
		fmt.Printf("\n  [%s] %s\n", status, info.Title)
		fmt.Printf("    id=%s entries=%d mdd=%d profile=%s\n", info.ID, info.EntryCount, info.MDDVolumes, svc.ProfileID(dict))
		// Tell the user what their MDD actually contains, so "why is there no
		// audio?" is answerable without guesswork.
		if kinds := dict.ResourceKinds(); len(kinds) > 0 {
			exts := make([]string, 0, len(kinds))
			for ext := range kinds {
				exts = append(exts, ext)
			}
			sort.Slice(exts, func(i, j int) bool { return kinds[exts[i]] > kinds[exts[j]] })
			parts := make([]string, 0, len(exts))
			for _, ext := range exts {
				parts = append(parts, fmt.Sprintf("%s=%d", ext, kinds[ext]))
			}
			fmt.Printf("    resources: %s\n", strings.Join(parts, " "))
		}
		for _, diagnostic := range info.Diagnostics {
			fmt.Printf("    ! %s\n", diagnostic)
		}
	}
	return nil
}

func runList(svc *service.Service) error {
	loadDictionaries(svc)
	dicts := svc.Registry().All()
	if len(dicts) == 0 {
		fmt.Printf("No dictionaries in %s\n", svc.Config().DictionaryDir)
		return nil
	}
	for _, dict := range dicts {
		info := dict.Info()
		fmt.Printf("%s  %-45s entries=%-8d mdd=%d profile=%s health=%s\n",
			info.ID, truncate(info.Title, 45), info.EntryCount, info.MDDVolumes, svc.ProfileID(dict), info.Health)
	}
	return nil
}

func runRescan(svc *service.Service) error {
	elapsed := loadDictionaries(svc)
	total, healthy := svc.Registry().Counts()
	fmt.Printf("rescanned %s in %s: %d dictionaries, %d healthy\n",
		svc.Config().DictionaryDir, elapsed.Round(time.Millisecond), total, healthy)
	return nil
}

func runDebugLookup(svc *service.Service, word string) error {
	loadDictionaries(svc)
	result, err := svc.Lookup(word, service.LookupOptions{Mode: service.ModeSmart, Debug: true, MaxExamples: 6})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func serve(svc *service.Service, log *slog.Logger) error {
	cfg := svc.Config()
	elapsed := loadDictionaries(svc)
	total, healthy := svc.Registry().Counts()
	log.Info("dictionaries loaded",
		"directory", cfg.DictionaryDir,
		"count", total,
		"healthy", healthy,
		"elapsed", elapsed.Round(time.Millisecond))

	server := &http.Server{
		Handler:           httpapi.New(svc, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Loopback only. Binding the wildcard address would expose a user's private
	// dictionary library to their whole network.
	listener, err := listenLoopback(cfg.Port)
	if err != nil {
		return err
	}
	log.Info("listening", "address", listener.Addr().String(), "version", version.Version, "api", version.APIVersion)

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	errs := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-shutdown:
		log.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

// listenLoopback binds 127.0.0.1, and ::1 as well when IPv6 is available.
func listenLoopback(port int) (net.Listener, error) {
	v4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen on 127.0.0.1:%d: %w (is another copy already running?)", port, err)
	}
	v6, err := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", port))
	if err != nil {
		// IPv6 loopback is not always present; IPv4 alone is a working service.
		return v4, nil
	}
	return &dualListener{primary: v4, secondary: v6}, nil
}

// dualListener accepts from both loopback families as a single listener.
type dualListener struct {
	primary   net.Listener
	secondary net.Listener

	once   sync.Once
	conns  chan net.Conn
	errs   chan error
	closed chan struct{}
}

func (d *dualListener) start() {
	d.conns = make(chan net.Conn)
	d.errs = make(chan error, 2)
	d.closed = make(chan struct{})
	for _, listener := range []net.Listener{d.primary, d.secondary} {
		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					select {
					case d.errs <- err:
					case <-d.closed:
					}
					return
				}
				select {
				case d.conns <- conn:
				case <-d.closed:
					conn.Close()
					return
				}
			}
		}(listener)
	}
}

func (d *dualListener) Accept() (net.Conn, error) {
	d.once.Do(d.start)
	select {
	case conn := <-d.conns:
		return conn, nil
	case err := <-d.errs:
		return nil, err
	}
}

func (d *dualListener) Close() error {
	d.once.Do(d.start)
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	err := d.primary.Close()
	if secondErr := d.secondary.Close(); err == nil {
		err = secondErr
	}
	return err
}

func (d *dualListener) Addr() net.Addr { return d.primary.Addr() }
