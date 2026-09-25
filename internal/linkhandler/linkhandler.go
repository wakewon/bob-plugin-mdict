// Package linkhandler makes dictionary links in Markdown look words up in Bob.
//
// Bob's Markdown view opens a link through macOS, and macOS can hand a URL
// only to an application bundle that registered its scheme. bob-mdict is a
// background process, not an application, so it keeps a tiny helper beside
// its data: an AppleScript applet, compiled on this machine by the system's
// own osacompile, that receives `bobmdict://lookup?text=…` and passes the
// text to Bob through Bob's documented AppleScript request
// (path "translate", action "translateText").
//
// The applet is generated locally rather than shipped because a file created
// on the machine carries no download quarantine, so Gatekeeper never
// assesses it and an ad-hoc signature is all it needs. No developer
// certificate is involved, whichever way bob-mdict itself was installed.
package linkhandler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/wakewon/bob-plugin-mdict/internal/playback"
)

// Scheme is the URL scheme the helper registers.
const Scheme = "bobmdict"

// AppName is the helper's bundle name. It is what macOS shows when it asks
// whether the helper may control Bob, so it says what the helper is for.
const AppName = "MDict Lookup.app"

// bundleID must not contain the service's launchd label: install scripts look
// for that label among running jobs, and the stay-open helper is one.
const bundleID = "com.github.wakewon.mdict-lookup"

// idleQuitSeconds is how long the stay-open helper waits for another link
// before it quits.
const idleQuitSeconds = 600

// engineKeepSeconds is how long the helper keeps its audio engine, and with
// it the audio device and any Bluetooth link, running after a sound.
const engineKeepSeconds = 20

// maxLogBytes bounds the helper's own log.
const maxLogBytes = 256 << 10

// coldLeadInMillis of silence precede a word when the engine had stopped: a
// Bluetooth headset that has gone idle drops the start of a new stream while
// it wakes, and for a single word that can be all of it.
const coldLeadInMillis = 300

// maxQueryRunes bounds what a link may ask Bob to translate. Dictionary link
// targets are words and phrases; anything longer did not come from one.
const maxQueryRunes = 200

// LookupURL returns the link that looks query up in Bob, or "" for a query
// no dictionary link would carry.
func LookupURL(query string) string {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > maxQueryRunes {
		return ""
	}
	return Scheme + "://lookup?text=" + queryEscape(query)
}

// NavigateURL is LookupURL for a link on the page of from: the helper tells
// the service where the reader came from before asking Bob, so the page
// that opens can offer the way back.
func NavigateURL(query, from string, port int) string {
	link := LookupURL(query)
	from = strings.TrimSpace(from)
	if link == "" || from == "" || len([]rune(from)) > maxQueryRunes || port <= 0 || port > 65535 {
		return link
	}
	return fmt.Sprintf("%s&from=%s&port=%d", link, queryEscape(from), port)
}

// BackURL is the link that returns to to, one step back along the path the
// reader took.
func BackURL(to string, port int) string {
	link := LookupURL(to)
	if link == "" || port <= 0 || port > 65535 {
		return ""
	}
	return fmt.Sprintf("%s&back=1&port=%d", link, port)
}

// queryEscape encodes a query value. NSURLComponents, which decodes the link
// in the helper, does not read "+" as a space, so spaces are percent-encoded.
func queryEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// PlayOptions are the reader's playback settings a play link carries:
// volume and rate as percentages, and whether to match loudness.
type PlayOptions struct {
	Volume, Rate int
	Normalize    bool
}

// PlayURL returns the link that plays a recording through the service on
// port, identified by its opaque resource token.
func PlayURL(port int, token string, options PlayOptions) string {
	inRange := func(value, high int) bool { return value >= 0 && value <= high }
	if port <= 0 || port > 65535 || !tokenRe.MatchString(token) ||
		!inRange(options.Volume, 999) || !inRange(options.Rate, 999) {
		return ""
	}
	flag := 0
	if options.Normalize {
		flag = 1
	}
	return fmt.Sprintf("%s://play?port=%d&token=%s&volume=%d&rate=%d&normalize=%d",
		Scheme, port, token, options.Volume, options.Rate, flag)
}

// tokenRe admits a resource token (lowercase base32) and nothing that could
// break out of a URL or a shell argument. The applet checks the same pattern
// before passing a token on.
var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

// script is the applet. It handles two routes and reads nothing else:
//
//   - lookup?text=… asks Bob to translate the text, through Bob's documented
//     AppleScript request. The request is sent by its raw event code so the
//     applet compiles whether or not Bob's scripting dictionary is available.
//     With from=… (or back=1) and port=…, it first tells the local service
//     where the reader came from, so the page that opens can lead back.
//   - play?port=…&token=…&volume=…&rate=…&normalize=… fetches the recording,
//     prepared by the local service, and plays it in an audio engine the
//     applet keeps running between clicks, at the requested speed through
//     Apple's time-stretch unit. Only a loopback address is contacted, and
//     every value is checked against its exact format before it reaches a
//     shell.
//
// The engine stays running for a while after the last sound, so the next
// word plays at once and a Bluetooth headset is not woken again; only when
// it had stopped does the applet ask for a lead-in of silence. Anything too
// long to have come from a dictionary link is ignored.
var script = fmt.Sprintf(`use AppleScript version "2.4"
use framework "Foundation"
use framework "AVFoundation"
use scripting additions

-- State lives in globals, not properties: an applet saves its properties
-- back into its own bundle when it quits, which would rewrite the signed
-- script and cannot store the audio objects at all.
global lastUse, lastSound, audioEngine, playerNode, timePitch, lastFile, fileCounter

on open location theURL
	my initialise()
	set lastUse to current date
	-- An error must never reach the default handler: in a stay-open applet it
	-- shows an alert, and the applet handles nothing else until it is closed.
	try
		return my route(theURL)
	on error errText number errNumber
		my logLine("error " & errNumber & ": " & errText)
	end try
end open location

on route(theURL)
	set components to current application's NSURLComponents's componentsWithString:theURL
	if components is missing value then return
	set theHost to components's |host|()
	if theHost is missing value then return
	set theHost to theHost as text
	if theHost is "lookup" then
		set theText to my queryValue(components, "text")
		if theText is "" or (length of theText) > %[1]d then return
		set thePort to my queryValue(components, "port")
		set theFrom to my queryValue(components, "from")
		set theBack to my queryValue(components, "back")
		if (my matches(thePort, "^[0-9]{1,5}$")) and (length of theFrom) ≤ %[1]d and (theFrom is not "" or theBack is "1") then
			-- Tell the service where the reader is going, and from where, before
			-- Bob looks the word up; a failure here must not stop the lookup.
			set theArgs to " --data-urlencode " & quoted form of ("to=" & theText)
			if theBack is "1" then
				set theArgs to theArgs & " --data-urlencode back=1"
			else
				set theArgs to theArgs & " --data-urlencode " & quoted form of ("from=" & theFrom)
			end if
			try
				do shell script "/usr/bin/curl -fsS -m 2" & theArgs & " " & quoted form of ("http://127.0.0.1:" & thePort & "/v2/navigation") & " > /dev/null"
			end try
		end if
		set theRequest to {|path|:"translate", body:{action:"translateText", |text|:theText}}
		set requestData to current application's NSJSONSerialization's dataWithJSONObject:theRequest options:0 |error|:(missing value)
		if requestData is missing value then return
		set requestText to ((current application's NSString's alloc()'s initWithData:requestData encoding:4) as text)
		my logLine("lookup " & theText)
		return my askBob(requestText)
	else if theHost is "play" then
		set thePort to my queryValue(components, "port")
		set theToken to my queryValue(components, "token")
		set theVolume to my queryValue(components, "volume")
		set theRate to my queryValue(components, "rate")
		set theNormalize to my queryValue(components, "normalize")
		if not (my matches(thePort, "^[0-9]{1,5}$")) then return
		if not (my matches(theToken, "^[A-Za-z0-9_-]{1,512}$")) then return
		if not (my matches(theVolume, "^[0-9]{1,3}$")) then set theVolume to "100"
		if not (my matches(theRate, "^[0-9]{1,3}$")) then set theRate to "100"
		if not (my matches(theNormalize, "^[01]$")) then set theNormalize to "1"
		set theLeadIn to "0"
		if not (my engineRunning()) then set theLeadIn to "%[2]d"
		set fileCounter to fileCounter + 1
		set thePath to (POSIX path of (path to temporary items)) & "mdict-lookup-" & fileCounter & ".wav"
		set audioURL to "http://127.0.0.1:" & thePort & "/v2/audio/" & theToken & "?volume=" & theVolume & "&rate=" & theRate & "&normalize=" & theNormalize & "&leadin=" & theLeadIn
		try
			do shell script "/usr/bin/curl -fsS -m 10 -X POST " & quoted form of audioURL & " -o " & quoted form of thePath
			set played to my playFile(thePath, (theRate as integer) / 100)
		on error errText number errNumber
			set played to false
			my logLine("play failed " & errNumber & ": " & errText)
		end try
		if played then
			my logLine("play " & theRate & "%% lead-in " & theLeadIn & "ms")
		else
			-- A file that is not playing is not lastFile, so nothing else
			-- would ever remove it.
			do shell script "/bin/rm -f " & quoted form of thePath
		end if
	end if
end route

-- The request is sent without waiting for Bob's reply. Bob shows the result
-- in its own window, and waiting — up to two minutes when Bob is slow to
-- answer — would hold every later click, lookups and pronunciations alike,
-- behind this one.
on askBob(requestText)
	ignoring application responses
		tell application id "com.hezongyidev.Bob" to «event bObSReQs» requestText
	end ignoring
end askBob

-- logLine appends to a small log beside the service's own, so a click that
-- seemed to do nothing can be traced. It starts over past %[6]d bytes.
on logLine(theText)
	try
		set logPath to (POSIX path of (path to library folder from user domain)) & "Logs/bob-mdict-helper.log"
		set fileManager to current application's NSFileManager's defaultManager()
		set attributes to fileManager's attributesOfItemAtPath:logPath |error|:(missing value)
		if attributes is missing value or ((attributes's fileSize()) as integer) > %[6]d then
			fileManager's createFileAtPath:logPath |contents|:(missing value) attributes:(missing value)
		end if
		set theLine to ((current date) as «class isot» as string) & " " & theText & linefeed
		set fileHandle to current application's NSFileHandle's fileHandleForWritingAtPath:logPath
		fileHandle's seekToEndOfFile()
		fileHandle's writeData:((current application's NSString's stringWithString:theLine)'s dataUsingEncoding:4)
		fileHandle's closeFile()
	end try
end logLine

on playFile(thePath, theRate)
	set audioFile to current application's AVAudioFile's alloc()'s initForReading:(current application's |NSURL|'s fileURLWithPath:thePath) |error|:(missing value)
	if audioFile is missing value then
		my logLine("play failed: the service sent no audio")
		return false
	end if
	if audioEngine is missing value then
		set audioEngine to current application's AVAudioEngine's alloc()'s init()
		set playerNode to current application's AVAudioPlayerNode's alloc()'s init()
		set timePitch to current application's AVAudioUnitTimePitch's alloc()'s init()
		audioEngine's attachNode:playerNode
		audioEngine's attachNode:timePitch
		set theFormat to current application's AVAudioFormat's alloc()'s initStandardFormatWithSampleRate:%[3]d channels:1
		audioEngine's connect:playerNode |to|:timePitch format:theFormat
		audioEngine's connect:timePitch |to|:(audioEngine's mainMixerNode()) format:theFormat
	end if
	playerNode's |stop|()
	timePitch's setRate:theRate
	if not (my engineRunning()) then
		if not ((audioEngine's startAndReturnError:(missing value)) as boolean) then
			my logLine("play failed: the audio engine did not start")
			return false
		end if
	end if
	playerNode's scheduleFile:audioFile atTime:(missing value) completionHandler:(missing value)
	playerNode's play()
	if lastFile is not "" and lastFile is not thePath then do shell script "/bin/rm -f " & quoted form of lastFile
	set lastFile to thePath
	set lastSound to current date
	return true
end playFile

on engineRunning()
	if audioEngine is missing value then return false
	return (audioEngine's isRunning()) as boolean
end engineRunning

on queryValue(components, theName)
	set queryItems to components's queryItems()
	if queryItems is missing value then return ""
	repeat with i from 1 to (queryItems's |count|())
		set queryItem to (queryItems's objectAtIndex:(i - 1))
		if ((queryItem's |name|()) as text) is theName then
			set theValue to queryItem's value()
			if theValue is missing value then return ""
			return theValue as text
		end if
	end repeat
	return ""
end queryValue

on matches(theText, thePattern)
	set theString to current application's NSString's stringWithString:theText
	set theRange to theString's rangeOfString:thePattern options:(current application's NSRegularExpressionSearch) range:{0, theString's |length|()}
	return (|length| of theRange) > 0
end matches

on run
	my initialise()
end run

on initialise()
	try
		lastFile
	on error
		set lastUse to current date
		set lastSound to missing value
		set audioEngine to missing value
		set playerNode to missing value
		set timePitch to missing value
		set lastFile to ""
		set fileCounter to 0
	end try
end initialise

-- The applet stays open: a link that arrives while an applet is quitting is
-- lost, which is what quick successive clicks did to a launch-per-click
-- applet. It stops the engine once nothing has played for a while, so the
-- headset can sleep, and quits once it has been idle for longer.
on idle
	try
		set now to current date
		if (my engineRunning()) and lastSound is not missing value and (now - lastSound) > %[4]d then
			playerNode's |stop|()
			audioEngine's |stop|()
		end if
		if lastUse is not missing value and (now - lastUse) > %[5]d then quit
	on error errText number errNumber
		my logLine("idle error " & errNumber & ": " & errText)
	end try
	return 5
end idle

on quit
	try
		if lastFile is not "" then do shell script "/bin/rm -f " & quoted form of lastFile
	end try
	continue quit
end quit
`, maxQueryRunes, coldLeadInMillis, playback.SampleRate, engineKeepSeconds, idleQuitSeconds, maxLogBytes)

// plistEdits are applied to the applet's Info.plist after compilation, in
// plutil's "-replace key -type value" form.
var plistEdits = [][]string{
	{"CFBundleIdentifier", "-string", bundleID},
	{"CFBundleName", "-string", "MDict Lookup"},
	{"CFBundleURLTypes", "-json", `[{"CFBundleURLName":"MDict Lookup","CFBundleURLSchemes":["` + Scheme + `"]}]`},
	// No Dock icon flashes on every click.
	{"LSUIElement", "-bool", "YES"},
	{"NSAppleEventsUsageDescription", "-string", "MDict Lookup asks Bob to look up the word you clicked in a dictionary entry."},
}

// stampName records what the installed helper was built from, so it is only
// rebuilt when that changes. Rebuilding changes the ad-hoc signature, and
// with it the macOS permission to control Bob, which the user would have to
// grant again.
const stampName = "bob-mdict-helper.stamp"

// compileArgs are how the applet is compiled; they are part of the stamp, so a
// change to them rebuilds installed helpers just as a change to the script
// does.
var compileArgs = []string{"-s"}

func stamp() string {
	digest := sha256.New()
	digest.Write([]byte(script))
	digest.Write([]byte(strings.Join(compileArgs, "\x00")))
	for _, edit := range plistEdits {
		digest.Write([]byte(strings.Join(edit, "\x00")))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// ErrUnsupported means links cannot be handled on this system.
var ErrUnsupported = errors.New("dictionary lookup links need macOS")

// Ensure installs or refreshes the helper in dir and registers it with
// Launch Services. It returns the helper's path. It is cheap when the helper
// is already current.
func Ensure(ctx context.Context, dir string) (string, error) {
	app, _, err := install(ctx, dir)
	if err != nil {
		return "", err
	}
	if err := register(ctx, app); err != nil {
		return "", err
	}
	return app, nil
}

// install builds the helper in dir unless the one there is current, and
// reports whether it built.
func install(ctx context.Context, dir string) (app string, built bool, err error) {
	if runtime.GOOS != "darwin" {
		return "", false, ErrUnsupported
	}
	if strings.TrimSpace(dir) == "" {
		return "", false, errors.New("no directory for the lookup helper")
	}
	app = filepath.Join(dir, AppName)
	want := stamp()
	if have, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", stampName)); err == nil && string(have) == want {
		return app, false, nil
	}
	if err := build(ctx, dir, app, want); err != nil {
		return "", false, err
	}
	return app, true, nil
}

func build(ctx context.Context, dir, app, want string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(dir, ".lookup-helper-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	source := filepath.Join(work, "lookup.applescript")
	if err := os.WriteFile(source, []byte(script), 0o644); err != nil {
		return err
	}
	staged := filepath.Join(work, AppName)
	// -s makes a stay-open applet; see the idle handler in script.
	if err := run(ctx, "/usr/bin/osacompile", append(append([]string(nil), compileArgs...), "-o", staged, source)...); err != nil {
		return err
	}
	plist := filepath.Join(staged, "Contents", "Info.plist")
	for _, edit := range plistEdits {
		args := append([]string{"-replace", edit[0], edit[1]}, edit[2:]...)
		if err := run(ctx, "/usr/bin/plutil", append(args, plist)...); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(staged, "Contents", "Resources", stampName), []byte(want), 0o644); err != nil {
		return err
	}
	// Editing Info.plist invalidates the applet's signature; an ad-hoc one
	// replaces it. Apple Silicon runs nothing unsigned.
	if err := run(ctx, "/usr/bin/codesign", "--force", "--deep", "--sign", "-", staged); err != nil {
		return err
	}
	// The helper stays open, so a copy built from the old source may still be
	// running; it would keep handling links until it idled out. A signal
	// stops it without asking macOS for permission to script it.
	_ = exec.CommandContext(ctx, "/usr/bin/pkill", "-f", "^"+regexp.QuoteMeta(filepath.Join(app, "Contents", "MacOS")+"/")).Run()
	if err := os.RemoveAll(app); err != nil {
		return err
	}
	return os.Rename(staged, app)
}

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

func register(ctx context.Context, app string) error {
	return run(ctx, lsregister, "-f", app)
}

func run(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(output.String()))
	}
	return nil
}
