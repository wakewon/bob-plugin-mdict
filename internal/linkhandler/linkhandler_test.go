package linkhandler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookupURL(t *testing.T) {
	cases := map[string]string{
		"hello":                              "bobmdict://lookup?text=hello",
		" cut and run":                       "bobmdict://lookup?text=cut%20and%20run",
		"wound²":                             "bobmdict://lookup?text=wound%C2%B2",
		"a+b & c=d":                          "bobmdict://lookup?text=a%2Bb%20%26%20c%3Dd",
		"":                                   "",
		strings.Repeat("字", maxQueryRunes+1): "",
	}
	for query, want := range cases {
		if got := LookupURL(query); got != want {
			t.Errorf("LookupURL(%q) = %q, want %q", query, got, want)
		}
	}
}

// probeLog points a probe's log at a temporary file, so tests never write to
// the user's own helper log, and returns the probe and the log's path.
func probeLog(t *testing.T, probe string) (string, string) {
	t.Helper()
	if !strings.Contains(probe, "on run argv") {
		t.Fatal("the applet's run handler moved; update this test")
	}
	const logPath = `(POSIX path of (path to library folder from user domain)) & "Logs/bob-mdict-helper.log"`
	if !strings.Contains(probe, logPath) {
		t.Fatal("the helper's log path moved; update this test")
	}
	path := filepath.Join(t.TempDir(), "helper.log")
	return strings.Replace(probe, logPath, `"`+path+`"`, 1), path
}

func requireMacOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the helper is a macOS applet")
	}
}

// The helper is built, but never registered: registering a temporary copy
// would claim the scheme away from the user's installed helper.
func TestInstallBuildsSignedURLHandlerOnce(t *testing.T) {
	requireMacOS(t)
	dir := t.TempDir()
	app, built, err := install(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !built || app != filepath.Join(dir, AppName) {
		t.Fatalf("first install: app=%q built=%v", app, built)
	}
	plist := filepath.Join(app, "Contents", "Info.plist")
	for key, want := range map[string]string{
		"CFBundleIdentifier":                      bundleID,
		"CFBundleURLTypes.0.CFBundleURLSchemes.0": Scheme,
		"LSUIElement":                             "true",
	} {
		out, err := exec.Command("/usr/bin/plutil", "-extract", key, "raw", plist).Output()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("%s = %q (%v), want %q", key, out, err, want)
		}
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		t.Fatalf("signature does not verify: %s", out)
	}

	// A current helper is left alone: rebuilding would change its signature
	// and cost the user the permission to control Bob.
	if _, built, err := install(context.Background(), dir); err != nil || built {
		t.Fatalf("second install rebuilt a current helper: built=%v err=%v", built, err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Resources", stampName), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, built, err := install(context.Background(), dir); err != nil || !built {
		t.Fatalf("stale helper was not rebuilt: built=%v err=%v", built, err)
	}
}

// The applet's routing is run by osascript with its two side effects — the
// request to Bob and the call to the service — replaced by returning what
// they would have sent, so nothing is sent anywhere.
func TestScriptRoutesLinks(t *testing.T) {
	requireMacOS(t)
	const bobCall = `tell application id bobID to «event bObSReQs» requestText`
	const playCall = `do shell script "/usr/bin/curl -fsS -m 10 -X POST " & quoted form of audioURL & " -o " & quoted form of thePath`
	if !strings.Contains(script, bobCall) || !strings.Contains(script, playCall) {
		t.Fatal("a side effect in the applet moved; update this test")
	}
	probe := strings.Replace(script, bobCall, "return requestText", 1)
	probe = strings.Replace(probe, playCall, `return "POST " & quoted form of audioURL`, 1)
	probe = strings.Replace(probe, "on run\n\tmy initialise()\nend run", "on run argv\n\tmy initialise()\n\ttell me to open location (item 1 of argv)\nend run", 1)
	if !strings.Contains(probe, "on run argv") {
		t.Fatal("the applet's run handler moved; update this test")
	}
	probe, _ = probeLog(t, probe)
	source := filepath.Join(t.TempDir(), "probe.applescript")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		LookupURL("cut and run"):    `{"path":"translate","body":{"action":"translateText","text":"cut and run"}}`,
		LookupURL("中华人民共和国"):        `{"path":"translate","body":{"action":"translateText","text":"中华人民共和国"}}`,
		LookupURL("a+b & c=d"):      `{"path":"translate","body":{"action":"translateText","text":"a+b & c=d"}}`,
		"bobmdict://lookup":         "",
		"bobmdict://lookup?other=x": "",
		"bobmdict://lookup?text=" + strings.Repeat("y", maxQueryRunes+1):                        "",
		PlayURL(15321, "Ab-_9z", PlayOptions{Volume: 150, Rate: 85}):                            `POST 'http://127.0.0.1:15321/v2/audio/Ab-_9z?volume=150&rate=85&normalize=0&leadin=300'`,
		"bobmdict://play?port=15321&token=abc":                                                  `POST 'http://127.0.0.1:15321/v2/audio/abc?volume=100&rate=100&normalize=1&leadin=300'`,
		"bobmdict://play?port=15321&token=abc&volume=1%27%3Bx&rate=9999&normalize=2&leadin=1;x": `POST 'http://127.0.0.1:15321/v2/audio/abc?volume=100&rate=100&normalize=1&leadin=300'`,
		"bobmdict://play?port=15321&token=a%27%3Brm%20-rf":                                      "",
		"bobmdict://play?port=15321;x&token=abc":                                                "",
		"bobmdict://play?token=abc":                                                             "",
		"bobmdict://elsewhere?text=hello":                                                       "",
	}
	for link, want := range cases {
		out, err := exec.Command("/usr/bin/osascript", source, link).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("%s -> %q (%v), want %q", link, out, err, want)
		}
	}
}

func TestNavigationLinks(t *testing.T) {
	if got := NavigateURL("cut and run", "run", 15321); got != "bobmdict://lookup?text=cut%20and%20run&from=run&port=15321" {
		t.Errorf("NavigateURL = %q", got)
	}
	if got := NavigateURL("belay", "", 15321); got != "bobmdict://lookup?text=belay" {
		t.Errorf("NavigateURL without an origin = %q", got)
	}
	if got := BackURL("wound²", 15321); got != "bobmdict://lookup?text=wound%C2%B2&back=1&port=15321" {
		t.Errorf("BackURL = %q", got)
	}
}

// A lookup link reports the step to the service before asking Bob.
func TestScriptReportsNavigation(t *testing.T) {
	requireMacOS(t)
	const navCall = `do shell script "/usr/bin/curl -fsS -m 2" & theArgs & " " & quoted form of ("http://127.0.0.1:" & thePort & "/v2/navigation") & " > /dev/null"`
	if !strings.Contains(script, navCall) {
		t.Fatal("the navigation report moved; update this test")
	}
	probe := strings.Replace(script, navCall, `return "NAV" & theArgs & " " & thePort`, 1)
	probe = strings.Replace(probe, `tell application id bobID to «event bObSReQs» requestText`, `return "BOB " & requestText`, 1)
	probe = strings.Replace(probe, "on run\n\tmy initialise()\nend run", "on run argv\n\tmy initialise()\n\ttell me to open location (item 1 of argv)\nend run", 1)
	probe, _ = probeLog(t, probe)
	source := filepath.Join(t.TempDir(), "probe.applescript")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		NavigateURL("cut and run", "run", 15321):   `NAV --data-urlencode 'to=cut and run' --data-urlencode 'from=run' 15321`,
		BackURL("hello", 15321):                    `NAV --data-urlencode 'to=hello' --data-urlencode back=1 15321`,
		LookupURL("hello"):                         `BOB {"path":"translate","body":{"action":"translateText","text":"hello"}}`,
		"bobmdict://lookup?text=x&from=y&port=1;2": `BOB {"path":"translate","body":{"action":"translateText","text":"x"}}`,
	}
	for link, want := range cases {
		out, err := exec.Command("/usr/bin/osascript", source, link).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("%s -> %q (%v), want %q", link, out, err, want)
		}
	}
}

func TestPlayURL(t *testing.T) {
	cases := []struct {
		port  int
		token string
		want  string
	}{
		{15321, "Ab-_9z", "bobmdict://play?port=15321&token=Ab-_9z&volume=100&rate=100&normalize=1"},
		{0, "abc", ""},
		{70000, "abc", ""},
		{15321, "a/b", ""},
		{15321, "", ""},
	}
	for _, tc := range cases {
		if got := PlayURL(tc.port, tc.token, PlayOptions{Volume: 100, Rate: 100, Normalize: true}); got != tc.want {
			t.Errorf("PlayURL(%d, %q) = %q, want %q", tc.port, tc.token, got, tc.want)
		}
	}
}

// The applet's audio code runs for real, muted: the engine starts, plays at
// a changed speed, and a second word replaces the first and its file.
func TestScriptPlaysThroughAPersistentEngine(t *testing.T) {
	requireMacOS(t)
	dir := t.TempDir()
	tone := filepath.Join(dir, "tone.wav")
	if out, err := exec.Command("/usr/bin/afconvert", "-f", "WAVE", "-d", "LEF32@44100", "-c", "1",
		"/System/Library/Sounds/Tink.aiff", tone).CombinedOutput(); err != nil {
		t.Fatalf("make a test sound: %v: %s", err, out)
	}
	second := filepath.Join(dir, "second.wav")
	if data, err := os.ReadFile(tone); err != nil || os.WriteFile(second, data, 0o644) != nil {
		t.Fatal("copy test sound")
	}
	const connect = "audioEngine's connect:timePitch |to|:(audioEngine's mainMixerNode()) format:theFormat"
	if !strings.Contains(script, connect) {
		t.Fatal("the engine wiring moved; update this test")
	}
	probe := strings.Replace(script, connect, connect+"\n\t\t(audioEngine's mainMixerNode())'s setOutputVolume:0", 1)
	probe = strings.Replace(probe, "on run\n\tmy initialise()\nend run", `on run argv
	my initialise()
	my playFile(item 1 of argv, 0.5)
	if not (my engineRunning()) then return "no-engine"
	delay 0.2
	my playFile(item 2 of argv, 1.0)
	delay 0.2
	set firstGone to not ((current application's NSFileManager's defaultManager()'s fileExistsAtPath:(item 1 of argv)) as boolean)
	return "running=" & (my engineRunning()) & " playing=" & ((playerNode's isPlaying()) as boolean) & " firstRemoved=" & firstGone
end run`, 1)
	probe, _ = probeLog(t, probe)
	source := filepath.Join(dir, "probe.applescript")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/osascript", source, tone, second).CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) == "no-engine" {
		// A build machine without an audio output device cannot start an
		// engine; that says nothing about the applet.
		t.Skip("no audio output device")
	}
	if err != nil || strings.TrimSpace(string(out)) != "running=true playing=true firstRemoved=true" {
		t.Fatalf("engine probe: %q (%v)", out, err)
	}
}

// An error inside a route is logged and goes no further: in a stay-open
// applet an uncaught error shows an alert that blocks every later click.
func TestScriptContainsErrors(t *testing.T) {
	requireMacOS(t)
	const ask = "return my askBob(requestText)"
	if !strings.Contains(script, ask) || !strings.Contains(script, "ignoring application responses") {
		t.Fatal("the request to Bob changed; update this test")
	}
	probe := strings.Replace(script, ask, `error "boom" number 42`, 1)
	probe = strings.Replace(probe, "on run\n\tmy initialise()\nend run", "on run argv\n\tmy initialise()\n\ttell me to open location (item 1 of argv)\n\treturn \"survived\"\nend run", 1)
	probe, logPath := probeLog(t, probe)
	source := filepath.Join(t.TempDir(), "probe.applescript")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/osascript", source, LookupURL("hello")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "survived" {
		t.Fatalf("an error escaped the route: %q (%v)", out, err)
	}
	logged, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logged), "lookup hello") || !strings.Contains(string(logged), "error 42: boom") {
		t.Fatalf("log = %q", logged)
	}
}

// A play that fails leaves no file behind: only the file that is playing is
// remembered, and so only it would otherwise ever be removed.
func TestScriptRemovesTheFileOfAFailedPlay(t *testing.T) {
	requireMacOS(t)
	probe := strings.Replace(script, "on run\n\tmy initialise()\nend run", `on run argv
	my initialise()
	set fileCounter to 900000 + (random number from 1 to 99999)
	tell me to open location (item 1 of argv)
	set thePath to (POSIX path of (path to temporary items)) & "mdict-lookup-" & fileCounter & ".wav"
	return ((current application's NSFileManager's defaultManager()'s fileExistsAtPath:thePath) as boolean)
end run`, 1)
	probe, logPath := probeLog(t, probe)
	source := filepath.Join(t.TempDir(), "probe.applescript")
	if err := os.WriteFile(source, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	// The download succeeds but is not audio, so the file exists and then
	// fails to open.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not audio"))
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	out, err := exec.Command("/usr/bin/osascript", source, fmt.Sprintf("bobmdict://play?port=%d&token=abc", port)).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "false" {
		t.Fatalf("failed play left its file: %q (%v)", out, err)
	}
	if logged, _ := os.ReadFile(logPath); !strings.Contains(string(logged), "play failed") {
		t.Fatalf("log = %q", logged)
	}
}

// The applet must compile on a Mac without Bob — a build machine, or a user
// who installs the service first. A literal application reference is
// resolved by osacompile and fails when the application is absent.
func TestScriptCompilesWithoutBob(t *testing.T) {
	requireMacOS(t)
	absent := strings.Replace(script, bobBundleID, "com.github.wakewon.bob-mdict.absent-test-app", 1)
	if absent == script {
		t.Fatal("the Bob bundle ID moved; update this test")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "absent.applescript")
	if err := os.WriteFile(source, []byte(absent), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/osacompile", "-o", filepath.Join(dir, "absent.scpt"), source).CombinedOutput(); err != nil {
		t.Fatalf("the applet needs its target application to compile: %v: %s", err, out)
	}
}
