// Package playback prepares a dictionary recording for the link helper to
// play.
//
// The helper keeps one audio engine running between clicks, so a word plays
// at once and a Bluetooth headset is not woken for every click; it applies
// the speed with Apple's time-stretch unit. What it plays comes from here:
// the recording decoded to one fixed format, matched in loudness, set to the
// reader's volume and, when the helper's engine is cold, preceded by a
// moment of silence.
//
// Nothing here is a new audio implementation. afconvert decodes and
// resamples the recording and measures its loudness (ITU-R BS.1770, the
// measure behind EBU R128 and Apple's Sound Check). This package only
// chooses one gain and applies it to the decoded samples.
package playback

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

// extensions maps the content types the resource endpoint serves to the
// file extensions the system tools recognise. Ogg Vorbis is absent because
// they cannot decode it; Speex is served as WAV.
var extensions = map[string]string{
	"audio/mpeg": ".mp3",
	"audio/wav":  ".wav",
	"audio/mp4":  ".m4a",
	"audio/aac":  ".aac",
	"audio/flac": ".flac",
}

// ErrUnsupported means the recording cannot be prepared here.
var ErrUnsupported = errors.New("this recording cannot be played on this system")

// Settings adjust one playback.
type Settings struct {
	// Volume is a percentage of the (matched) level; 100 leaves it alone.
	Volume int
	// Rate is a percentage of normal speed; 100 leaves it alone.
	Rate int
	// Normalize matches the recording's loudness to TargetLoudness, so
	// dictionaries recorded at different levels sound alike.
	Normalize bool
	// LeadIn is silence, in milliseconds of real time, before the word. A
	// Bluetooth headset that has gone idle drops the first few hundred
	// milliseconds of a new stream while it wakes; for a single word that can
	// be all of it. The helper asks for it only when its engine was stopped.
	LeadIn int
}

// Defaults are what a caller that expresses no preference gets.
func Defaults() Settings {
	return Settings{Volume: 100, Rate: 100, Normalize: true}
}

// Ranges accepted from callers; values outside are clamped.
const (
	MinVolume, MaxVolume = 10, 200
	MinRate, MaxRate     = 50, 200
	MaxLeadIn            = 1000
)

// Clamped returns s with every value inside its accepted range.
func (s Settings) Clamped() Settings {
	s.Volume = clamp(s.Volume, MinVolume, MaxVolume)
	s.Rate = clamp(s.Rate, MinRate, MaxRate)
	s.LeadIn = clamp(s.LeadIn, 0, MaxLeadIn)
	return s
}

func clamp(value, low, high int) int {
	return int(math.Max(float64(low), math.Min(float64(high), float64(value))))
}

const (
	// TargetLoudness is the integrated loudness recordings are matched to,
	// in LUFS: the level Apple's Sound Check uses for music and speech.
	TargetLoudness = -16.0
	// peakCeiling keeps the loudest sample, after all gain, below full scale
	// so nothing is clipped. It is in dBFS (true peak where known).
	peakCeiling = -1.0
	// maxMatchGain bounds how far matching moves a recording, so a near-silent
	// file is not amplified into noise.
	maxMatchGain = 20.0
)

// Supported reports whether a content type can be prepared here.
func Supported(contentType string) bool {
	_, ok := extensions[contentType]
	return ok && runtime.GOOS == "darwin"
}

// SampleRate and the single channel are the format every prepared recording
// has, so the helper connects its engine once and never reconfigures it.
const SampleRate = 44100

// Prepare returns the recording as a WAV file ready to play.
func Prepare(dir string, data []byte, contentType string, settings Settings) ([]byte, error) {
	ext, ok := extensions[contentType]
	if !ok || runtime.GOOS != "darwin" {
		return nil, ErrUnsupported
	}
	settings = settings.Clamped()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(dir, "prepare-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	source := filepath.Join(work, "source"+ext)
	if err := os.WriteFile(source, data, 0o644); err != nil {
		return nil, err
	}
	return adjust(work, source, settings)
}

// adjust decodes source to the prepared format, applies the gain settings
// call for, adds the lead-in, and returns the result as a WAV file.
func adjust(work, source string, settings Settings) ([]byte, error) {
	decoded := filepath.Join(work, "decoded.caf")
	args := []string{source, "-f", "caff", "-d", "LEF32@" + strconv.Itoa(SampleRate), "-c", "1"}
	if settings.Normalize {
		args = append(args, "--soundcheck-generate")
	}
	if err := run("/usr/bin/afconvert", append(args, decoded)...); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(decoded)
	if err != nil {
		return nil, err
	}
	audio, err := parseCAF(raw)
	if err != nil {
		return nil, err
	}

	measured := measurement{loudness: math.NaN(), truePeak: math.NaN()}
	if settings.Normalize {
		if info, err := output("/usr/bin/afinfo", decoded); err == nil {
			measured = parseMeasurement(info)
		}
	}
	gain := Gain(settings, measured.loudness, peakDB(audio.samples, measured.truePeak))
	for i := range audio.samples {
		audio.samples[i] *= float32(gain)
	}
	// The lead-in is stretched with the word, so it is shortened by the rate
	// to last the same real time at any speed.
	audio.leadIn(float64(settings.LeadIn) / 1000 * float64(settings.Rate) / 100)
	return audio.wav(), nil
}

// Gain is the linear gain for a recording with the given integrated
// loudness (NaN when unknown) and peak level in dBFS: loudness matching when
// asked for, then the volume setting, then as much reduction as keeps the
// peak under the ceiling.
func Gain(settings Settings, loudness, peak float64) float64 {
	settings = settings.Clamped()
	db := 20 * math.Log10(float64(settings.Volume)/100)
	if settings.Normalize && !math.IsNaN(loudness) && !math.IsInf(loudness, 0) {
		db += math.Max(-maxMatchGain, math.Min(maxMatchGain, TargetLoudness-loudness))
	}
	if !math.IsNaN(peak) && !math.IsInf(peak, 0) {
		db = math.Min(db, peakCeiling-peak)
	}
	return math.Pow(10, db/20)
}

type measurement struct {
	loudness float64
	truePeak float64
}

var (
	loudnessRe = regexp.MustCompile(`aa itu loudness\s*:\s*(-?[0-9.]+)`)
	truePeakRe = regexp.MustCompile(`aa itu true peak\s*:\s*(-?[0-9.]+)`)
)

// parseMeasurement reads the loudness analysis afinfo prints for a file
// afconvert analysed. Missing values are NaN.
func parseMeasurement(info string) measurement {
	read := func(re *regexp.Regexp) float64 {
		match := re.FindStringSubmatch(info)
		if match == nil {
			return math.NaN()
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return math.NaN()
		}
		return value
	}
	return measurement{loudness: read(loudnessRe), truePeak: read(truePeakRe)}
}

// peakDB is the recording's peak in dBFS: the measured true peak when there
// is one, else the largest sample.
func peakDB(samples []float32, truePeak float64) float64 {
	if !math.IsNaN(truePeak) {
		return truePeak
	}
	var peak float64
	for _, sample := range samples {
		peak = math.Max(peak, math.Abs(float64(sample)))
	}
	if peak == 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(peak)
}

// pcm is interleaved 32-bit float audio.
type pcm struct {
	sampleRate float64
	channels   int
	samples    []float32
}

// parseCAF reads the float PCM that afconvert writes with -f caff -d LEF32.
// A Core Audio Format file is a 8-byte header followed by chunks, each a
// four-character type and a big-endian 64-bit size; the audio description
// is in "desc" and the samples in "data", after a 4-byte edit count.
func parseCAF(raw []byte) (pcm, error) {
	var audio pcm
	if len(raw) < 8 || string(raw[:4]) != "caff" {
		return audio, errors.New("not a CAF file")
	}
	var data []byte
	for offset := 8; offset+12 <= len(raw); {
		kind := string(raw[offset : offset+4])
		size := int64(binary.BigEndian.Uint64(raw[offset+4 : offset+12]))
		body := offset + 12
		end := len(raw)
		if size >= 0 && body+int(size) <= len(raw) {
			end = body + int(size)
		}
		switch kind {
		case "desc":
			if end-body < 32 {
				return audio, errors.New("short CAF description")
			}
			desc := raw[body:end]
			audio.sampleRate = math.Float64frombits(binary.BigEndian.Uint64(desc[0:8]))
			if string(desc[8:12]) != "lpcm" || binary.BigEndian.Uint32(desc[28:32]) != 32 {
				return audio, errors.New("CAF is not 32-bit PCM")
			}
			audio.channels = int(binary.BigEndian.Uint32(desc[24:28]))
		case "data":
			if end-body >= 4 {
				data = raw[body+4 : end]
			}
		}
		offset = end
	}
	if audio.channels <= 0 || audio.sampleRate <= 0 || data == nil {
		return audio, errors.New("CAF has no playable audio")
	}
	audio.samples = make([]float32, len(data)/4)
	if err := binary.Read(bytes.NewReader(data[:len(audio.samples)*4]), binary.LittleEndian, audio.samples); err != nil {
		return audio, err
	}
	return audio, nil
}

// leadIn puts silence before the audio, in seconds of file time.
func (audio *pcm) leadIn(seconds float64) {
	frames := int(seconds*audio.sampleRate) * audio.channels
	padded := make([]float32, frames, frames+len(audio.samples))
	audio.samples = append(padded, audio.samples...)
}

// wav encodes the audio as a 32-bit float WAVE file.
func (audio pcm) wav() []byte {
	const formatFloat = 3
	dataSize := len(audio.samples) * 4
	var out bytes.Buffer
	write := func(values ...any) {
		for _, value := range values {
			_ = binary.Write(&out, binary.LittleEndian, value)
		}
	}
	out.WriteString("RIFF")
	write(uint32(36 + dataSize))
	out.WriteString("WAVEfmt ")
	write(uint32(16), uint16(formatFloat), uint16(audio.channels), uint32(audio.sampleRate),
		uint32(int(audio.sampleRate)*audio.channels*4), uint16(audio.channels*4), uint16(32))
	out.WriteString("data")
	write(uint32(dataSize))
	write(audio.samples)
	return out.Bytes()
}

func run(name string, args ...string) error {
	_, err := output(name, args...)
	return err
}

func output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", filepath.Base(name), err, bytes.TrimSpace(result))
	}
	return string(result), nil
}
