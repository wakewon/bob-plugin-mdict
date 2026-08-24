// Package config resolves the on-disk locations and runtime settings used by
// the bob-mdict service.
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/wakewon/bob-plugin-mdict/internal/version"
)

// Config holds everything the service needs to start.
type Config struct {
	// DictionaryDir is scanned recursively for .mdx files.
	DictionaryDir string
	// CacheDir holds transcoded audio and other derived artifacts.
	CacheDir string
	// Port is the loopback TCP port to listen on.
	Port int
	// Debug enables verbose logging and the debug lookup fields.
	Debug bool
}

// appSupportDir returns ~/Library/Application Support/bob-mdict.
func appSupportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "bob-mdict")
	}
	return filepath.Join(home, "Library", "Application Support", "bob-mdict")
}

// Default builds a Config from environment overrides, falling back to the
// standard macOS Application Support layout.
func Default() Config {
	base := appSupportDir()

	dictDir := os.Getenv("BOB_MDICT_DICTIONARY_DIR")
	if dictDir == "" {
		dictDir = filepath.Join(base, "dictionaries")
	}

	cacheDir := os.Getenv("BOB_MDICT_CACHE_DIR")
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			cacheDir = filepath.Join(home, "Library", "Caches", "bob-mdict")
		} else {
			cacheDir = filepath.Join(base, "cache")
		}
	}

	port := version.DefaultPort
	if raw := os.Getenv("BOB_MDICT_PORT"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed < 65536 {
			port = parsed
		}
	}

	return Config{
		DictionaryDir: dictDir,
		CacheDir:      cacheDir,
		Port:          port,
		Debug:         os.Getenv("BOB_MDICT_DEBUG") == "1",
	}
}

// EnsureDirs creates the dictionary and cache directories if they are missing,
// so a fresh install has somewhere for the user to drop dictionaries.
func (c Config) EnsureDirs() error {
	if err := os.MkdirAll(c.DictionaryDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(c.CacheDir, 0o755)
}
