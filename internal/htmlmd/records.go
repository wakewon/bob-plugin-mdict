package htmlmd

import (
	"strconv"
	"strings"
)

// MultiRecordMode mirrors the option of the same name in the IR renderers:
// show one record and how to reach the others, or show them all.
type MultiRecordMode string

const (
	MultiRecordCombined MultiRecordMode = "combined"
	MultiRecordSeparate MultiRecordMode = "separate"
)

// Record is one visible record under a key: its ordinal in the EntrySet, so
// selectors mean the same record in every presentation, and its HTML.
type Record struct {
	Ordinal int
	HTML    []byte
}

// RecordOptions configures RenderRecords.
type RecordOptions struct {
	Options
	// Key is the lookup key sibling selectors are built from.
	Key string
	// Total is how many records the key has, for "Record n of total"; zero
	// means len(records).
	Total           int
	MultiRecordMode MultiRecordMode
	// RecordOrdinal selects one record; non-zero always wins over the mode.
	RecordOrdinal int
}

// RenderRecords renders the records under one key with the record boundaries
// and navigation of the IR Markdown, so switching views never changes how
// several records are presented (selectors become lookup links when
// Options.LookupLink provides them):
//
//   - combined: each record under a "Record n of total" heading, divided by a
//     thematic break;
//   - separate: one record, then "Other entries" listing its siblings'
//     selectors as copyable query text.
//
// The navigation is added even when the dictionary links its homographs
// itself: many do not, and a record reached through a redirect never shows
// the siblings it was merged with. Unlike the IR view there is no title
// heading — the dictionary's page already prints its headword. Only records
// that are shown are converted.
func RenderRecords(records []Record, opts RecordOptions) string {
	if len(records) == 0 {
		return ""
	}
	if opts.MultiRecordMode != MultiRecordSeparate && opts.RecordOrdinal <= 0 {
		total := opts.Total
		if total <= 0 {
			total = len(records)
		}
		var parts []string
		for _, record := range records {
			text := Convert(record.HTML, opts.Options)
			if text == "" {
				continue
			}
			if len(records) > 1 {
				if len(parts) > 0 {
					parts = append(parts, "---")
				}
				parts = append(parts, "## Record "+strconv.Itoa(record.Ordinal)+" of "+strconv.Itoa(total))
			}
			parts = append(parts, text)
		}
		return joinDocument(parts)
	}

	selected := opts.RecordOrdinal
	if selected <= 0 {
		selected = records[0].Ordinal
	}
	var body string
	var siblings []string
	for _, record := range records {
		if record.Ordinal == selected {
			body = Convert(record.HTML, opts.Options)
			continue
		}
		if key := strings.TrimSpace(opts.Key); key != "" {
			siblings = append(siblings, "- "+siblingSelector(key+superscriptOrdinal(record.Ordinal), opts.LookupLink))
		}
	}
	if body == "" {
		return ""
	}
	parts := []string{body}
	if len(siblings) > 0 {
		parts = append(parts, "## Other entries", strings.Join(siblings, "\n"))
	}
	return joinDocument(parts)
}

// Selector is the reserved query form that names one record: the key with
// the record ordinal in superscript.
func Selector(key string, ordinal int) string { return key + superscriptOrdinal(ordinal) }

// Link writes a Markdown link, escaping what its text could break.
func Link(text, target string) string {
	return "[" + strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(text) + "](" + target + ")"
}

// siblingSelector is a record selector the reader can click when lookup
// links are available, and copyable query text when they are not.
func siblingSelector(selector string, lookupLink func(string) string) string {
	if lookupLink != nil {
		if link := lookupLink(selector); link != "" {
			return Link(selector, link)
		}
	}
	return codeSpan(selector)
}

func joinDocument(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// superscriptOrdinal renders a record ordinal in the reserved selector form.
// The IR renderers keep their own copies on purpose: every surface must agree
// on the user-visible selector, but none may import another.
func superscriptOrdinal(value int) string {
	if value <= 0 {
		return ""
	}
	digits := [...]rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}
	var out []rune
	for _, digit := range strconv.Itoa(value) {
		out = append(out, digits[digit-'0'])
	}
	return string(out)
}

// codeSpan wraps navigation text in an inline code span, choosing a fence the
// content itself cannot terminate.
func codeSpan(text string) string {
	value := strings.Join(strings.Fields(text), " ")
	if value == "" {
		return ""
	}
	fence := "`"
	for strings.Contains(value, fence) {
		fence += "`"
	}
	pad := ""
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") {
		pad = " "
	}
	return fence + pad + value + pad + fence
}
