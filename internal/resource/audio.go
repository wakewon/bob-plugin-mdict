package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ErrNoSpeexDecoder means an Ogg-Speex asset was requested but no decoder is
// installed. The service degrades to reporting the asset as unavailable rather
// than substituting synthesized speech.
var ErrNoSpeexDecoder = errors.New("speex decoder not available")

// Transcoder converts Ogg-Speex pronunciation assets to WAV, which macOS and
// Bob can play, and caches the result on disk so each asset is decoded once.
type Transcoder struct {
	cacheDir string

	once       sync.Once
	decoderCmd string

	mu       sync.Mutex
	inFlight map[string]*flightLock
}

type flightLock struct {
	mu   sync.Mutex
	refs int
}

const (
	maxAudioCacheBytes = int64(256 << 20)
	maxAudioCacheAge   = 30 * 24 * time.Hour
)

// NewTranscoder creates a transcoder writing into cacheDir/audio.
func NewTranscoder(cacheDir string) *Transcoder {
	transcoder := &Transcoder{
		cacheDir: filepath.Join(cacheDir, "audio"),
		inFlight: make(map[string]*flightLock),
	}
	transcoder.cleanupDiskCache(time.Now())
	return transcoder
}

// speexDecoders are the binaries able to decode .spx, in preference order.
var speexDecoders = []string{"speexdec", "ffmpeg"}

func (t *Transcoder) resolveDecoder() string {
	t.once.Do(func() {
		for _, name := range speexDecoders {
			if path, err := exec.LookPath(name); err == nil {
				t.decoderCmd = path
				return
			}
		}
	})
	return t.decoderCmd
}

// SpeexAvailable reports whether Speex assets can be served.
func (t *Transcoder) SpeexAvailable() bool { return t.resolveDecoder() != "" }

// DecoderName returns the decoder binary in use, or "" when none is installed.
func (t *Transcoder) DecoderName() string {
	path := t.resolveDecoder()
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// keyLock serializes concurrent decodes of the same asset so two simultaneous
// plays do not race on the same cache file.
func (t *Transcoder) acquireKeyLock(key string) func() {
	t.mu.Lock()
	lock, ok := t.inFlight[key]
	if !ok {
		lock = &flightLock{}
		t.inFlight[key] = lock
	}
	lock.refs++
	t.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		t.mu.Lock()
		lock.refs--
		if lock.refs == 0 && t.inFlight[key] == lock {
			delete(t.inFlight, key)
		}
		t.mu.Unlock()
	}
}

// SpeexToWAV decodes Ogg-Speex bytes to WAV, using the disk cache when warm.
func (t *Transcoder) SpeexToWAV(data []byte) ([]byte, error) {
	decoder := t.resolveDecoder()
	if decoder == "" {
		return nil, ErrNoSpeexDecoder
	}

	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:])
	cachePath := filepath.Join(t.cacheDir, key+".wav")

	release := t.acquireKeyLock(key)
	defer release()

	if cached, err := os.ReadFile(cachePath); err == nil && len(cached) > 0 {
		return cached, nil
	}

	if err := os.MkdirAll(t.cacheDir, 0o755); err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "bob-mdict-spx-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	inPath := filepath.Join(tmpDir, "in.spx")
	outPath := filepath.Join(tmpDir, "out.wav")
	if err := os.WriteFile(inPath, data, 0o600); err != nil {
		return nil, err
	}

	var args []string
	if filepath.Base(decoder) == "ffmpeg" {
		args = []string{"-loglevel", "error", "-i", inPath, "-f", "wav", outPath}
	} else {
		args = []string{inPath, outPath}
	}

	cmd := exec.Command(decoder, args...)
	// A pronunciation clip is a couple of seconds; anything slower is a hang.
	timer := time.AfterFunc(15*time.Second, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	output, err := cmd.CombinedOutput()
	timer.Stop()
	if err != nil {
		return nil, fmt.Errorf("speex decode failed: %w (%s)", err, truncate(string(output), 200))
	}

	wav, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("speex decode produced no output: %w", err)
	}
	if len(wav) == 0 {
		return nil, errors.New("speex decode produced an empty file")
	}

	// Write to a temp file then rename so a crashed decode never leaves a
	// truncated entry in the cache.
	stagePath := cachePath + ".tmp"
	if err := os.WriteFile(stagePath, wav, 0o644); err == nil {
		_ = os.Rename(stagePath, cachePath)
	}
	return wav, nil
}

// cleanupDiskCache applies a deliberately small startup policy: remove stale
// WAVs, then evict the oldest remaining files until the cache is under 256 MiB.
func (t *Transcoder) cleanupDiskCache(now time.Time) {
	entries, err := os.ReadDir(t.cacheDir)
	if err != nil {
		return
	}
	type cachedFile struct {
		path    string
		size    int64
		modTime time.Time
	}
	var files []cachedFile
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".wav" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(t.cacheDir, entry.Name())
		if now.Sub(info.ModTime()) > maxAudioCacheAge {
			_ = os.Remove(path)
			continue
		}
		files = append(files, cachedFile{path: path, size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, file := range files {
		if total <= maxAudioCacheBytes {
			break
		}
		if err := os.Remove(file.path); err == nil {
			total -= file.size
		}
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
