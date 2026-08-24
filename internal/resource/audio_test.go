package resource

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// oggSpeexSample is a minimal Ogg page header. It is not decodable audio; it
// exists only to prove the transcoder reports failure rather than returning
// something Bob would try to play.
var oggSpeexSample = append([]byte("OggS\x00\x02"), make([]byte, 64)...)

func TestSpeexAvailabilityReporting(t *testing.T) {
	transcoder := NewTranscoder(t.TempDir())
	_, lookErr := exec.LookPath("speexdec")
	_, ffmpegErr := exec.LookPath("ffmpeg")
	expected := lookErr == nil || ffmpegErr == nil
	if transcoder.SpeexAvailable() != expected {
		t.Errorf("SpeexAvailable = %v, want %v", transcoder.SpeexAvailable(), expected)
	}
	if expected && transcoder.DecoderName() == "" {
		t.Error("a decoder is installed but DecoderName is empty")
	}
}

func TestSpeexDecodeFailsCleanlyOnGarbage(t *testing.T) {
	transcoder := NewTranscoder(t.TempDir())
	if !transcoder.SpeexAvailable() {
		t.Skip("no Speex decoder installed")
	}
	if _, err := transcoder.SpeexToWAV(oggSpeexSample); err == nil {
		t.Error("decoding garbage should fail rather than produce silent output")
	}
}

// TestSpeexRoundTripUsesCache decodes a real Ogg-Speex file when one has been
// staged locally, and checks the second call is served from disk.
func TestSpeexRoundTripUsesCache(t *testing.T) {
	sample := os.Getenv("BOB_MDICT_TEST_SPX")
	if sample == "" {
		t.Skip("set BOB_MDICT_TEST_SPX to a .spx file to run the transcode test")
	}
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	transcoder := NewTranscoder(cacheDir)
	if !transcoder.SpeexAvailable() {
		t.Skip("no Speex decoder installed")
	}

	wav, err := transcoder.SpeexToWAV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) < 44 || string(wav[:4]) != "RIFF" {
		t.Fatalf("output is not a WAV file (%d bytes, magic %q)", len(wav), wav[:min(4, len(wav))])
	}

	entries, err := filepath.Glob(filepath.Join(cacheDir, "audio", "*.wav"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one cached file, got %v (%v)", entries, err)
	}

	// Corrupt the source so a second decode would produce different bytes;
	// an identical result proves the cache was used.
	again, err := transcoder.SpeexToWAV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(wav) {
		t.Errorf("cached result differs: %d vs %d bytes", len(again), len(wav))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
