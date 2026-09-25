# Known issues and limitations

What is known not to work, or to work only partly, and why. Each entry says
what a reader sees, what is known about the cause, and what a fix would take,
so later work can start from the evidence rather than rediscover it.

## Clicking links in Bob

**A click on a Markdown link sometimes does nothing.** Clicking the same 🔊 or
dictionary link a few seconds apart does not always act. When it does not,
the helper's log (`~/Library/Logs/bob-mdict-helper.log`) has no entry for the
click, so macOS never handed the link to the helper: the click was not turned
into an open request inside Bob. Nothing in this project can make Bob act on
a click; the evidence and a reproduction belong in a report to Bob.

Two causes of the same symptom were found and fixed on our side, and are the
first things to rule out if it reappears:

- a resident helper that waited up to two minutes for Bob's reply to a lookup
  and held every later click behind it (it now sends without waiting);
- resource tokens containing `_`, which a lenient Markdown renderer can read
  as emphasis and so break a link (tokens are now lowercase base32).

**Bob asks "Open this link?" for every click** unless its setting
**Opening Links in Translation Results** is **Never Ask**. Bob confirms any
link that is not http, https or mailto. An http link would avoid the prompt,
but Bob opens those in the browser, which is what the helper exists to avoid.
The setting applies to links from every service in Bob.

## Pronunciation playback

**The start of a word can be lost on a Bluetooth headset.** When the helper's
audio engine has been stopped (20 seconds after the last sound ended), starting it
wakes the Bluetooth link. The system log has shown 0.15–0.9 s from starting
output to `STREAMING`, while the helper precedes the word with 0.3 s of
silence (`coldLeadInMillis`). A longer lead-in, or starting the engine before
fetching the audio, would cover more of it at the cost of latency.

Some headsets are also believed to mute their amplifier after a few seconds
of pure digital silence, even while the stream stays up, which would swallow
the start of a word played while the engine is warm. This is inferred, not
observed: it happens inside the headset. If it is confirmed — clicks a second
apart always sound, clicks ten seconds apart often do not — the fix is to
play an inaudible signal instead of silence while the engine is kept running.

**Only formats macOS can decode are played in the background.** Ogg Vorbis is
not among them; such a 🔊 keeps its http resource link and opens in the
browser. Speex recordings are transcoded to WAV and play normally when a
decoder is installed.

**Volume, speed and loudness matching apply only to 🔊 in Markdown.** The
dictionary card's pronunciation buttons are played by Bob itself.

## The link helper

**It is macOS-only and built on the user's machine.** `MDict Lookup.app` is
compiled by `osacompile` and signed ad hoc, so it needs no developer
certificate, but every rebuild — any change to its script, Info.plist edits
or compile flags — changes its signature, and macOS asks again whether it may
control Bob. Rebuilds happen only when the stamp changes, never on ordinary
restarts.

**`brew uninstall` leaves it behind.** `packaging/uninstall.sh` stops the
helper, unregisters its `bobmdict://` scheme and deletes it, but a Homebrew
formula cannot remove files outside its prefix, so after a Homebrew uninstall
`~/Library/Application Support/bob-mdict/MDict Lookup.app` remains registered
until it is deleted by hand.

**Its bundle ID must not contain the service's launchd label.** The install
scripts once matched that label as a substring among running jobs, and the
stay-open helper's job name contained it; they now match exactly, but the
naming rule stands.

## Web-layout view

**It is only as good as the dictionary's stylesheet.** Layout lives in CSS,
and a dictionary whose stylesheet is missing renders as a run of unstyled
text. `/list` and `/v2/dictionaries` report missing stylesheets; the fix is to
install them, not to write substitutes.

**Only a subset of CSS is read**: `display`, `visibility`, `font-weight`,
`font-style`, `list-style`, horizontal margins and padding, and string
`content` in `::before`/`::after`. Counters, `attr()`, images in `content`,
colours and sizes are ignored; `@media` blocks with conditions and state
selectors such as `:hover` are skipped. Generated text that is floated or
absolutely positioned is treated as decoration and dropped.

**Nothing interactive survives.** Collapsed panels stay collapsed, tabs and
switches do nothing, and script-built content is absent. Dictionaries whose
"switch" stylesheet hides Chinese until a click (Collins' and LDOCE's
`*_switch.css`) therefore show no Chinese when that stylesheet is installed;
leaving it out shows both languages. Profiles hide the worst of the leftover
controls with `presentationCSS`.

**Words split across styled siblings can gain a space.** Adjacent inline
elements whose edge letters would merge — `noun` + `Plural` — are separated,
because a margin kept them apart on the page. A single word styled in two
sibling elements (`<b>un</b><i>believable</i>`) is split the same way. A
letter followed by a digit (a homograph number) is exempt.

**The structured parse still runs.** The EntrySet decides which records exist,
so every web-layout lookup parses each record and then reads the records again
for their HTML. Large entries pay for a parse whose result is otherwise unused.

## Navigation

**There is one path for the whole service.** The way back is recorded per
service, not per Bob window or per service instance, and is offered only on
the page a link opens, within 30 seconds of the click. A lookup Bob makes
later than that — or the same word typed by hand — shows no way back.

**With an empty Dictionary ID, a link is looked up in the first dictionary
that has the word**, which need not be the dictionary the link was in.

**The structured Markdown view keeps navigation as copyable text** in code
spans; only the web-layout view makes dictionary links clickable.

## MDict engine

**`lib-x/mdx` leaves the last entry of every key block without a record end**,
so reading it returns every later record in its record block — fourteen
seconds of words for one OALD8 recording. `mdict.linkRecordEnds` closes those
ranges after loading. The fix belongs upstream; until it is there, removing
`linkRecordEnds` reintroduces the bug.
