package service

import (
	"runtime"
	"testing"

	"github.com/wakewon/bob-plugin-mdict/internal/config"
	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
	"github.com/wakewon/bob-plugin-mdict/internal/playback"
)

// A 🔊 link plays in the background only once the link helper is ready, and
// only for a format this Mac can play; otherwise it stays the resource URL.
func TestPlayURLNeedsTheHelperAndAPlayableFormat(t *testing.T) {
	svc, err := New(config.Config{DictionaryDir: t.TempDir(), CacheDir: t.TempDir(), Port: 15321})
	if err != nil {
		t.Fatal(err)
	}
	mp3 := &entryir.Audio{Token: "Ab-_9z", MIMEType: "audio/mpeg", URL: "http://127.0.0.1:15321/v2/resource/Ab-_9z"}
	ogg := &entryir.Audio{Token: "Cd", MIMEType: "audio/ogg"}
	if got := svc.playURL(mp3, playback.Defaults()); got != "" {
		t.Fatalf("play link before the helper is ready: %q", got)
	}
	svc.EnableLookupLinks()
	want := "bobmdict://play?port=15321&token=Ab-_9z&volume=100&rate=100&normalize=1"
	if runtime.GOOS != "darwin" {
		want = ""
	}
	if got := svc.playURL(mp3, playback.Defaults()); got != want {
		t.Errorf("playURL(mp3) = %q, want %q", got, want)
	}
	if got := svc.playURL(ogg, playback.Defaults()); got != "" {
		t.Errorf("the system tools cannot decode Ogg Vorbis, got %q", got)
	}
	if got := svc.playURL(mp3, playback.Settings{Volume: 500, Rate: 10}); runtime.GOOS == "darwin" && got != "bobmdict://play?port=15321&token=Ab-_9z&volume=200&rate=50&normalize=0" {
		t.Errorf("settings must be clamped into the link, got %q", got)
	}
	if got := svc.playURL(nil, playback.Defaults()); got != "" {
		t.Errorf("playURL(nil) = %q", got)
	}
}
