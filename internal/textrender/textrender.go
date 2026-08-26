// Package textrender renders the canonical dictionary EntrySet as readable
// plain text. It is a sibling of bobadapter and mdrender: it consumes the IR
// directly and never strips presentation syntax from another renderer.
package textrender

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

type MultiRecordMode string

const (
	MultiRecordCombined MultiRecordMode = "combined"
	MultiRecordSeparate MultiRecordMode = "separate"
)

type Options struct {
	IncludeExamples     bool
	IncludeExtras       bool
	MaxExamplesPerSense int
	RecordOrdinal       int
	MultiRecordMode     MultiRecordMode
	IncludeGrammar      bool
}

func DefaultOptions() Options {
	return Options{IncludeExamples: true, IncludeExtras: true, MaxExamplesPerSense: 8, MultiRecordMode: MultiRecordCombined, IncludeGrammar: true}
}

func UserOptions() Options {
	opts := DefaultOptions()
	opts.MultiRecordMode = MultiRecordSeparate
	return opts
}

func RenderEntry(entry *entryir.Entry, opts Options) string {
	if entry == nil {
		return ""
	}
	key := firstNonEmpty(entry.Source.MatchedKey, entry.Headword)
	return RenderEntrySet(&entryir.EntrySet{LookupKey: key, Headword: entry.Headword, Records: []entryir.EntryRecord{{RecordOrdinal: 1, Entry: entry}}}, opts)
}

func RenderEntrySet(set *entryir.EntrySet, opts Options) string {
	if set == nil || len(set.Records) == 0 {
		return ""
	}
	if opts.MaxExamplesPerSense <= 0 {
		opts.MaxExamplesPerSense = DefaultOptions().MaxExamplesPerSense
	}
	key := firstNonEmpty(set.LookupKey, set.Headword)
	if key == "" && set.Primary() != nil {
		key = set.Primary().Headword
	}
	if opts.RecordOrdinal > 0 || opts.MultiRecordMode == MultiRecordSeparate {
		return renderSelected(set, key, opts)
	}

	doc := &document{opts: opts}
	doc.paragraph(key)
	rendered := 0
	for index, record := range set.Records {
		if record.Entry == nil {
			continue
		}
		if rendered > 0 {
			doc.paragraph("====================")
		}
		if len(set.Records) > 1 {
			ordinal := record.RecordOrdinal
			if ordinal <= 0 {
				ordinal = index + 1
			}
			doc.paragraph(fmt.Sprintf("Record %d of %d", ordinal, len(set.Records)))
		}
		doc.renderEntry(record.Entry, set.LookupKey)
		rendered++
	}
	return doc.String()
}

func renderSelected(set *entryir.EntrySet, key string, opts Options) string {
	selected := opts.RecordOrdinal
	if selected <= 0 {
		selected = 1
	}
	if selected > len(set.Records) || set.Records[selected-1].Entry == nil {
		return ""
	}
	doc := &document{opts: opts}
	title := key
	if opts.RecordOrdinal > 0 && len(set.Records) > 1 {
		title += superscriptOrdinal(selected)
	}
	doc.paragraph(title)
	doc.renderEntry(set.Records[selected-1].Entry, set.LookupKey)
	if len(set.Records) > 1 && key != "" {
		var siblings []string
		for index, record := range set.Records {
			if record.Entry == nil {
				continue
			}
			ordinal := record.RecordOrdinal
			if ordinal <= 0 {
				ordinal = index + 1
			}
			if ordinal != selected {
				siblings = append(siblings, key+superscriptOrdinal(ordinal))
			}
		}
		if len(siblings) > 0 {
			doc.section("Other entries", siblings)
		}
	}
	return doc.String()
}

type document struct {
	out  strings.Builder
	opts Options
}

func (d *document) String() string { return strings.TrimSpace(d.out.String()) + "\n" }

func (d *document) paragraph(text string) {
	if text = strings.TrimSpace(text); text != "" {
		d.out.WriteString(text)
		d.out.WriteString("\n\n")
	}
}

func (d *document) section(title string, lines []string) {
	var kept []string
	for _, line := range lines {
		if line = strings.TrimRight(line, " \t\r\n"); strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return
	}
	d.paragraph(strings.TrimSpace(title) + "\n" + strings.Join(kept, "\n"))
}

func (d *document) renderEntry(entry *entryir.Entry, lookupKey string) {
	if headword := strings.TrimSpace(entry.Headword); headword != "" && headword != lookupKey {
		d.paragraph("Headword: " + headword)
	}
	d.renderPronunciations(entry.Pronunciations)
	for _, part := range entry.Parts {
		label := strings.TrimSpace(part.POS)
		if label == "" {
			label = "Meanings"
		}
		if grammar := strings.TrimSpace(part.Grammar); d.opts.IncludeGrammar && grammar != "" {
			label += " · " + grammar
		}
		var lines []string
		for index, sense := range part.Senses {
			lines = append(lines, d.renderSense(sense, strconv.Itoa(index+1), 0)...)
		}
		d.section(label, lines)
	}
	if !d.opts.IncludeExtras {
		return
	}
	d.renderForms(entry.Forms)
	d.renderPhrases("Phrases", entry.Phrases)
	d.renderPhrases("Idioms", entry.Idioms)
	d.renderPhrases("Phrasal verbs", entry.PhrasalVerbs)
	d.renderPhrases("Derivatives", entry.Derivatives)
	d.section("Collocations", entry.Collocations)
	d.section("Synonyms", entry.Synonyms)
	d.section("Antonyms", entry.Antonyms)
	d.renderNavigation("See also", entry.CrossReferences)
	d.renderNavigation("Related", entry.Related)
	d.section("Word family", entry.WordFamily)
	for _, note := range entry.UsageNotes {
		d.renderSection("Usage · "+note.Title, note)
	}
	for _, note := range entry.GrammarNotes {
		d.renderSection("Grammar · "+note.Title, note)
	}
	if entry.Etymology != "" {
		d.section("Origin", []string{entry.Etymology})
	}
	for _, section := range entry.Sections {
		d.renderSection(section.Title, section)
	}
}

func (d *document) renderPronunciations(items []entryir.Pronunciation) {
	var lines []string
	for _, item := range items {
		var fields []string
		if item.IPA != "" {
			fields = append(fields, regionLabel(item.IPARegion)+" /"+item.IPA+"/")
		}
		if item.Label != "" {
			fields = append(fields, item.Label)
		}
		if item.Audio != nil {
			fields = append(fields, "audio "+regionLabel(item.AudioRegion))
		}
		if len(fields) > 0 {
			lines = append(lines, strings.Join(fields, " · "))
		}
	}
	if len(lines) > 0 {
		d.section("Pronunciation", lines)
	}
}

func (d *document) renderSense(sense entryir.Sense, fallback string, depth int) []string {
	number := strings.TrimSpace(sense.Number)
	if number == "" {
		number = fallback
	}
	indent := strings.Repeat("  ", depth)
	var details []string
	if len(sense.Labels) > 0 {
		details = append(details, "("+strings.Join(sense.Labels, ", ")+")")
	}
	if sense.Topic != "" {
		details = append(details, "["+sense.Topic+"]")
	}
	if d.opts.IncludeGrammar && sense.Grammar != "" {
		details = append(details, sense.Grammar)
	}
	if len(sense.Patterns) > 0 {
		details = append(details, strings.Join(sense.Patterns, " / "))
	}
	text := strings.TrimSpace(strings.Join(details, " ") + " " + sense.Definition)
	if sense.Translation != "" {
		if text != "" {
			text += " — "
		}
		text += sense.Translation
	}
	lines := []string{indent + number + ". " + strings.TrimSpace(text)}
	childIndent := strings.Repeat("  ", depth+1)
	if d.opts.IncludeExamples {
		for index, example := range sense.Examples {
			if index >= d.opts.MaxExamplesPerSense {
				break
			}
			line := childIndent + "Example: " + example.Text
			if example.Translation != "" {
				line += " — " + example.Translation
			}
			lines = append(lines, line)
		}
	}
	if len(sense.Synonyms) > 0 {
		lines = append(lines, childIndent+"Synonyms: "+strings.Join(sense.Synonyms, ", "))
	}
	if len(sense.Antonyms) > 0 {
		lines = append(lines, childIndent+"Antonyms: "+strings.Join(sense.Antonyms, ", "))
	}
	for index, sub := range sense.Subsenses {
		lines = append(lines, d.renderSense(sub, number+"."+strconv.Itoa(index+1), depth+1)...)
	}
	return lines
}

func (d *document) renderForms(forms []entryir.Form) {
	var lines []string
	for _, form := range forms {
		if len(form.Words) > 0 {
			lines = append(lines, firstNonEmpty(form.Name, "form")+": "+strings.Join(form.Words, ", "))
		}
	}
	d.section("Forms", lines)
}

func (d *document) renderPhrases(title string, entries []entryir.PhraseEntry) {
	var lines []string
	for _, entry := range entries {
		line := strings.TrimSpace(entry.Phrase)
		if entry.Definition != "" {
			if line != "" {
				line += " — "
			}
			line += entry.Definition
		}
		if line != "" {
			lines = append(lines, line)
		}
		if d.opts.IncludeExamples {
			for index, example := range entry.Examples {
				if index >= d.opts.MaxExamplesPerSense {
					break
				}
				exampleLine := "  Example: " + example.Text
				if example.Translation != "" {
					exampleLine += " — " + example.Translation
				}
				lines = append(lines, exampleLine)
			}
		}
	}
	d.section(title, lines)
}

func (d *document) renderNavigation(title string, values []string) {
	if len(values) > 0 {
		d.paragraph(title + ": " + strings.Join(values, ", "))
	}
}

func (d *document) renderSection(title string, section entryir.Section) {
	if title = strings.TrimSpace(title); title == "" {
		title = "Note"
	}
	if len(section.Blocks) == 0 {
		d.section(title, []string{section.Body})
		return
	}
	var chunks []string
	for _, block := range section.Blocks {
		switch block.Kind {
		case entryir.RichText, entryir.RichHeading:
			if block.Text != "" {
				chunks = append(chunks, block.Text)
			}
		case entryir.RichListItem:
			if block.Text != "" {
				chunks = append(chunks, "  • "+block.Text)
			}
		case entryir.RichImage:
			if block.Image != nil {
				chunks = append(chunks, firstNonEmpty(block.Image.Alt, "[Image]"))
			}
		case entryir.RichTable:
			var lines []string
			rows := block.Rows
			if len(block.Header) > 0 {
				rows = append([][]string{block.Header}, rows...)
			}
			for _, row := range rows {
				lines = append(lines, strings.Join(row, " | "))
			}
			if len(lines) > 0 {
				chunks = append(chunks, strings.Join(lines, "\n"))
			}
		}
	}
	if len(chunks) > 0 {
		d.paragraph(title + "\n" + strings.Join(chunks, "\n\n"))
	}
}

func regionLabel(region entryir.Region) string {
	switch region {
	case entryir.RegionUK:
		return "UK"
	case entryir.RegionUS:
		return "US"
	case entryir.RegionNeutral:
		return "shared"
	default:
		return "unmarked"
	}
}

func superscriptOrdinal(value int) string {
	digits := [...]rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}
	var out []rune
	for _, digit := range strconv.Itoa(value) {
		out = append(out, digits[digit-'0'])
	}
	return string(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
