package parser

import "testing"

// Enumeration is the parser's second-strongest evidence after class names, and
// it is evidence read out of ordinary prose. These tests pin down where the
// line falls between a sense number and a number that merely happens to be at
// the start of a sentence.

func TestSplitEnumMarkerAccepts(t *testing.T) {
	cases := []struct {
		text   string
		marker string
		rest   string
	}{
		{"1. to leave someone behind", "1", "to leave someone behind"},
		{"2) a second meaning", "2", "a second meaning"},
		{"(3) a third meaning", "3", "a third meaning"},
		{"1.2 a subdivided meaning", "1.2", "a subdivided meaning"},
		{"① a circled meaning", "1", "a circled meaning"},
		{"❷ another circled meaning", "2", "another circled meaning"},
		{"a) a lettered meaning", "a", "a lettered meaning"},
		{"b. another lettered meaning", "b", "another lettered meaning"},
		{"4 a meaning after a plain space", "4", "a meaning after a plain space"},
		{"5、a meaning after an ideographic comma", "5", "a meaning after an ideographic comma"},
	}
	for _, test := range cases {
		marker, rest, ok := splitEnumMarker(test.text)
		if !ok {
			t.Errorf("splitEnumMarker(%q) found no marker", test.text)
			continue
		}
		if marker.Text != test.marker || rest != test.rest {
			t.Errorf("splitEnumMarker(%q) = %q, %q; want %q, %q",
				test.text, marker.Text, rest, test.marker, test.rest)
		}
	}
}

func TestSplitEnumMarkerRejects(t *testing.T) {
	// Every one of these was found in the corpus being read as a sense number
	// by an earlier draft of this rule.
	cases := []string{
		"a barbecue area",       // an article, not a marker
		"an early example",      // ditto, longer
		"1 000 metres of cable", // a quantity written with a space
		"(2 unclosed",           // a bracket that never closes
		"7",                     // a number with nothing after it
		"1",                     // ditto
		"twofold; dual",         // ordinary prose
		"v. transitive",         // a part of speech, not a letter marker
	}
	for _, text := range cases {
		if marker, rest, ok := splitEnumMarker(text); ok {
			t.Errorf("splitEnumMarker(%q) wrongly found marker %q with rest %q", text, marker.Text, rest)
		}
	}
}

func TestMarkerOnly(t *testing.T) {
	for _, text := range []string{"1", "1.", " 2 ", "(3)", "①", "a.", "II", "IV."} {
		if _, ok := markerOnly(text); !ok {
			t.Errorf("markerOnly(%q) = false, want true", text)
		}
	}
	for _, text := range []string{"", "noun", "1 to fasten", "abcdefghi", "12345678901"} {
		if _, ok := markerOnly(text); ok {
			t.Errorf("markerOnly(%q) = true, want false", text)
		}
	}
}

func markers(t *testing.T, texts ...string) []enumMarker {
	t.Helper()
	out := make([]enumMarker, 0, len(texts))
	for _, text := range texts {
		marker, ok := markerOnly(text)
		if !ok {
			t.Fatalf("markerOnly(%q) failed", text)
		}
		out = append(out, marker)
	}
	return out
}

func TestPlausibleSequence(t *testing.T) {
	if !plausibleSequence(markers(t, "1", "2", "3")) {
		t.Error("a plain ascending run should be a sequence")
	}
	if !plausibleSequence(markers(t, "1", "1.1", "1.2", "2")) {
		t.Error("dotted subdivisions should keep their place in the sequence")
	}
	// Numbering restarts under each part of speech in most dictionaries.
	if !plausibleSequence(markers(t, "1", "2", "3", "1", "2")) {
		t.Error("a restart at the start value should be allowed")
	}
	if plausibleSequence(markers(t, "7", "8", "9")) {
		t.Error("a run that begins at 7 is a fragment of something else")
	}
	if plausibleSequence(markers(t, "3", "2", "1")) {
		t.Error("a descending run is not an enumeration")
	}
	if plausibleSequence(markers(t, "1", "1", "1")) {
		t.Error("a repeated label is not an enumeration")
	}
	// A run that restarts as often as it counts is a repeated label, not a
	// sense list: five markers spread over three runs enumerate nothing.
	if plausibleSequence(markers(t, "1", "2", "1", "2", "1")) {
		t.Error("a sequence that restarts as often as it counts is not one")
	}
}

func TestPlausibleSequenceRejectsMixedNumbering(t *testing.T) {
	mixed := append(markers(t, "1", "2"), markers(t, "a", "b")...)
	if plausibleSequence(mixed) {
		t.Error("arabic and lettered markers belong to different levels")
	}
}

func TestPlausibleSenseSizes(t *testing.T) {
	if !plausibleSenseSizes([]int{40, 90, 120}) {
		t.Error("ordinary sense lengths should be accepted")
	}
	if plausibleSenseSizes([]int{9000, 11000, 10000}) {
		t.Error("a split whose pieces are whole articles is at the wrong level")
	}
}
