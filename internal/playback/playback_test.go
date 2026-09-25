package playback

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// No test here plays anything: a test run must stay silent.

func TestUnsupportedRecordingsAreRefusedBeforeAnythingIsWritten(t *testing.T) {
	dir := t.TempDir()
	for _, contentType := range []string{"audio/ogg", "image/png", ""} {
		if Supported(contentType) {
			t.Errorf("%q reported as playable", contentType)
		}
		if _, err := Prepare(dir, []byte("x"), contentType, Defaults()); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: err = %v", contentType, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("refused recordings left files behind: %v", entries)
	}
}

func decibels(gain float64) float64 { return 20 * math.Log10(gain) }

func TestGain(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		name           string
		settings       Settings
		loudness, peak float64
		wantDB         float64
	}{
		{"untouched", Settings{Volume: 100, Rate: 100}, nan, -6, 0},
		{"quiet recording is matched up", Settings{Volume: 100, Normalize: true}, -26, -12, 10},
		{"loud recording is matched down", Settings{Volume: 100, Normalize: true}, -10, -0.5, -6},
		{"matching stops at the peak ceiling", Settings{Volume: 100, Normalize: true}, -26, -3, 2},
		{"volume adds to matching", Settings{Volume: 50, Normalize: true}, -16, -10, -6.0206},
		{"matching is bounded", Settings{Volume: 100, Normalize: true}, -70, -60, 20},
		{"unknown loudness only applies volume", Settings{Volume: 200, Normalize: true}, nan, -20, 6.0206},
		{"louder volume still never clips", Settings{Volume: 200}, nan, -2, 1},
	}
	for _, tc := range cases {
		got := decibels(Gain(tc.settings, tc.loudness, tc.peak))
		if math.Abs(got-tc.wantDB) > 0.01 {
			t.Errorf("%s: %.3f dB, want %.3f dB", tc.name, got, tc.wantDB)
		}
	}
}

func TestSettingsAreClamped(t *testing.T) {
	got := Settings{Volume: 1000, Rate: 1}.Clamped()
	if got.Volume != MaxVolume || got.Rate != MinRate {
		t.Errorf("clamped = %+v", got)
	}
}

func TestParseMeasurement(t *testing.T) {
	info := `    main loudness parameters         :
        aa itu true peak                 : -0.138402
        aa itu sample peak               : -0.157548
        aa itu loudness                  : -14.7328
`
	got := parseMeasurement(info)
	if got.loudness != -14.7328 || got.truePeak != -0.138402 {
		t.Errorf("measurement = %+v", got)
	}
	if missing := parseMeasurement("no analysis"); !math.IsNaN(missing.loudness) || !math.IsNaN(missing.truePeak) {
		t.Errorf("missing values should be NaN: %+v", missing)
	}
}

// The whole adjustment runs through the system tools on a synthetic tone,
// and its result is measured by the same analysis.
func TestAdjustMatchesLoudnessWithoutClipping(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("uses macOS audio tools")
	}
	work := t.TempDir()
	tone := pcm{sampleRate: 16000, channels: 1, samples: make([]float32, 16000)}
	for i := range tone.samples {
		tone.samples[i] = float32(0.02 * math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	source := filepath.Join(work, "tone.wav")
	if err := os.WriteFile(source, tone.wav(), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := adjust(work, source, Settings{Volume: 100, Rate: 100, Normalize: true})
	if err != nil {
		t.Fatal(err)
	}
	adjusted := filepath.Join(work, "prepared.wav")
	if err := os.WriteFile(adjusted, prepared, 0o644); err != nil {
		t.Fatal(err)
	}
	analysed := filepath.Join(work, "check.caf")
	if err := run("/usr/bin/afconvert", adjusted, "-f", "caff", "-d", "LEF32", "--soundcheck-generate", analysed); err != nil {
		t.Fatal(err)
	}
	info, err := output("/usr/bin/afinfo", analysed)
	if err != nil {
		t.Fatal(err)
	}
	got := parseMeasurement(info)
	if math.IsNaN(got.loudness) || got.truePeak > peakCeiling+0.2 {
		t.Fatalf("adjusted tone: %+v", got)
	}
	// A quiet tone is raised towards the target, and never past it.
	if got.loudness < TargetLoudness-3 || got.loudness > TargetLoudness+0.5 {
		t.Errorf("adjusted loudness %.2f LUFS, want close to %.0f", got.loudness, TargetLoudness)
	}
}

func TestLeadInPutsSilenceBeforeTheAudio(t *testing.T) {
	audio := pcm{sampleRate: 1000, channels: 2, samples: []float32{1, 1, 1, 1}}
	audio.leadIn(0.003)
	want := []float32{0, 0, 0, 0, 0, 0, 1, 1, 1, 1}
	if len(audio.samples) != len(want) {
		t.Fatalf("padded to %d samples, want %d", len(audio.samples), len(want))
	}
	for i := range want {
		if audio.samples[i] != want[i] {
			t.Fatalf("padded samples = %v", audio.samples)
		}
	}
}

// Every recording comes out in the one format the helper's engine is wired
// for, and the lead-in is stretched along with the word, so the file carries
// it shortened by the rate and it lasts the same real time at any speed.
func TestPreparedAudioHasTheFixedFormatAndLeadIn(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("uses macOS audio tools")
	}
	work := t.TempDir()
	// Half a second of stereo at 16 kHz: both the rate and the channel
	// count differ from the prepared format.
	tone := pcm{sampleRate: 16000, channels: 2, samples: make([]float32, 16000)}
	for i := range tone.samples {
		tone.samples[i] = float32(0.3 * math.Sin(2*math.Pi*440*float64(i/2)/16000))
	}
	source := filepath.Join(work, "tone.wav")
	if err := os.WriteFile(source, tone.wav(), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := adjust(work, source, Settings{Volume: 100, Rate: 50, LeadIn: 400})
	if err != nil {
		t.Fatal(err)
	}
	channels := int(binary.LittleEndian.Uint16(raw[22:24]))
	rate := int(binary.LittleEndian.Uint32(raw[24:28]))
	if channels != 1 || rate != SampleRate {
		t.Fatalf("prepared format: %d channels at %d Hz", channels, rate)
	}
	frames := (len(raw) - 44) / 4
	want := SampleRate/2 + int(0.4*0.5*SampleRate)
	if frames < want-64 || frames > want+64 {
		t.Errorf("prepared audio has %d frames, want about %d", frames, want)
	}
}
