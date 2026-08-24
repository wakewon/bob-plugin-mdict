package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	inFlight map[string]*sync.Mutex
}

// NewTranscoder creates a transcoder writing into cacheDir/audio.
func NewTranscoder(cacheDir string) *Transcoder {
	return &Transcoder{
		cacheDir: filepath.Join(cacheDir, "audio"),
		inFlight: make(map[string]*sync.Mutex),
	}
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
func (t *Transcoder) keyLock(key string) *sync.Mutex {
	t.mu.Lock()
	defer t.mu.Unlock()
	lock, ok := t.inFlight[key]
	if !ok {
		lock = &sync.Mutex{}
		t.inFlight[key] = lock
	}
	return lock
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

	lock := t.keyLock(key)
	lock.Lock()
	defer lock.Unlock()

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

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
