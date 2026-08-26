// Package mdrender renders the dictionary-neutral Entry IR as Markdown.
//
// It is a sibling of internal/bobadapter, not a layer on top of it:
//
//	EntrySet → bobadapter → Bob toDict
//	EntrySet → mdrender   → Markdown
//
// Both read the same canonical IR and neither can see the other. That is the
// whole point of the arrangement. Markdown is not produced by converting entry
// HTML — a second semantic path would drift from the parser the moment either
// changed — and the parser needs no knowledge that Markdown exists.
//
// The renderer has two callers: deterministic diagnostics, which keep
// per-process resource URLs out, and user presentation, which enables resolved
// loopback audio/image links without enabling parser provenance.
package mdrender

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// Options configures rendering. The zero value renders senses only.
type Options struct {
	IncludeExamples     bool
	IncludeExtras       bool
	MaxExamplesPerSense int
	// AudioLinks writes the playable loopback URL for a resolved recording.
	//
	// It is off by default because those URLs carry a per-process resource
	// token: including one would make the same EntrySet render differently on
	// every run and destroy snapshot comparison. With it off, an available
	// recording is still reported — just as a fact rather than as a link.
	AudioLinks bool
	// ImageLinks writes resolved inline MDD illustrations at their original
	// prose position. It is disabled for deterministic diagnostic snapshots.
	ImageLinks bool
	// RecordOrdinal selects one visible record when a record selector was used.
	// Zero renders every record with explicit boundaries.
	RecordOrdinal int
	// IncludeProvenance annotates each structure with the parser rule that
	// produced it. Development only; it is what makes a snapshot answer "which
	// heuristic did this?".
	IncludeProvenance bool
}

// DefaultOptions renders a complete entry for a human reader.
func DefaultOptions() Options {
	return Options{
		IncludeExamples:     true,
		IncludeExtras:       true,
		MaxExamplesPerSense: 8,
	}
}

// UserOptions returns complete, user-facing Markdown. It intentionally does
// not enable IncludeProvenance: parser rules and confidence belong only in
// diagnostic artifacts.
func UserOptions() Options {
	opts := DefaultOptions()
	opts.AudioLinks = true
	opts.ImageLinks = true
	return opts
}

// RenderEntry renders one entry as a standalone document.
func RenderEntry(entry *entryir.Entry, opts Options) string {
	if entry == nil {
		return ""
	}
	return RenderEntrySet(&entryir.EntrySet{
		LookupKey: firstNonEmpty(entry.Source.MatchedKey, entry.Headword),
		Headword:  entry.Headword,
		Records:   []entryir.EntryRecord{{RecordOrdinal: 1, Entry: entry}},
	}, opts)
}

// RenderEntrySet renders a complete EntrySet, preserving record boundaries.
//
// Distinct records are never merged: each keeps its own heading and its own
// ordinal, because two records under one key are two things the dictionary
// chose to say separately.
func RenderEntrySet(set *entryir.EntrySet, opts Options) string {
	if set == nil || len(set.Records) == 0 {
		return ""
	}
	if opts.MaxExamplesPerSense <= 0 {
		opts.MaxExamplesPerSense = DefaultOptions().MaxExamplesPerSense
	}
	doc := &document{opts: opts}

	title := firstNonEmpty(set.LookupKey, set.Headword)
	if primary := set.Primary(); title == "" && primary != nil {
		title = primary.Headword
	}
	doc.heading(1, title)

	records := set.Records
	if opts.RecordOrdinal > 0 && opts.RecordOrdinal <= len(records) {
		records = records[opts.RecordOrdinal-1 : opts.RecordOrdinal]
	}
	multi := len(set.Records) > 1
	// Nesting each record under its own heading costs one level, so a
	// single-record entry keeps its parts one step closer to the surface.
	base := 2
	if multi {
		base = 3
	}
	for index, record := range records {
		if record.Entry == nil {
			continue
		}
		ordinal := record.RecordOrdinal
		if ordinal <= 0 {
			ordinal = index + 1
		}
		if multi {
			doc.heading(2, fmt.Sprintf("Record %d of %d", ordinal, len(set.Records)))
		}
		doc.renderEntry(record.Entry, base, set.LookupKey)
	}
	return doc.String()
}

type document struct {
	out  strings.Builder
	opts Options
}

func (d *document) String() string {
	// One trailing newline, never more: byte-stability is the property this
	// renderer is being trusted for.
	return strings.TrimRight(d.out.String(), "\n") + "\n"
}

func (d *document) heading(level int, text string) {
	d.headingWith(level, text, "")
}

// headingWith escapes the label but not the suffix, which already carries its
// own Markdown (a provenance annotation is code-spanned on purpose).
func (d *document) headingWith(level int, text, suffix string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	d.line(strings.Repeat("#", level) + " " + escape(text) + suffix)
}

func (d *document) line(text string) {
	d.out.WriteString(text)
	d.out.WriteString("\n\n")
}

func (d *document) renderEntry(entry *entryir.Entry, base int, lookupKey string) {
	// The headword is a content fact and the lookup key is a presentation
	// alias; when they disagree that difference is worth seeing, and inventing
	// agreement between them would hide a real parser signal.
	if headword := strings.TrimSpace(entry.Headword); headword != "" && headword != lookupKey {
		d.line("*headword:* " + escape(headword))
	}
	d.renderPronunciations(entry.Pronunciations)

	for _, part := range entry.Parts {
		label := strings.TrimSpace(part.POS)
		if label == "" {
			label = "(unlabelled)"
		}
		if part.Grammar != "" {
			label += " · " + part.Grammar
		}
		d.headingWith(base, label, d.provenance(part.Rule, part.Confidence))
		var lines []string
		for i, sense := range part.Senses {
			lines = append(lines, d.renderSense(sense, strconv.Itoa(i+1), 0)...)
		}
		d.block(lines)
	}

	if !d.opts.IncludeExtras {
		return
	}
	d.renderForms(entry.Forms, base)
	d.renderPhrases("Phrases", entry.Phrases, base)
	d.renderPhrases("Idioms", entry.Idioms, base)
	d.renderPhrases("Phrasal verbs", entry.PhrasalVerbs, base)
	d.renderPhrases("Derivatives", entry.Derivatives, base)
	d.renderList("Collocations", entry.Collocations, base)
	d.renderList("Synonyms", entry.Synonyms, base)
	d.renderList("Antonyms", entry.Antonyms, base)
	d.renderList("See also", entry.CrossReferences, base)
	d.renderList("Related", entry.Related, base)
	d.renderList("Word family", entry.WordFamily, base)
	for _, note := range entry.UsageNotes {
		d.renderSection("Usage · "+note.Title, note, base)
	}
	for _, note := range entry.GrammarNotes {
		d.renderSection("Grammar · "+note.Title, note, base)
	}
	d.renderSection("Origin", entryir.Section{Body: entry.Etymology}, base)
	for _, section := range entry.Sections {
		d.renderSection(section.Title, section, base)
	}
}

func (d *document) renderPronunciations(items []entryir.Pronunciation) {
	var lines []string
	for _, item := range items {
		var parts []string
		if item.IPA != "" {
			parts = append(parts, regionLabel(item.IPARegion)+" /"+escape(item.IPA)+"/")
		}
		if item.Label != "" {
			parts = append(parts, "*"+escape(item.Label)+"*")
		}
		if item.Audio != nil {
			parts = append(parts, d.audio(item.Audio, item.AudioRegion))
		}
		if len(parts) == 0 {
			continue
		}
		lines = append(lines, "- "+strings.Join(parts, " · ")+d.provenance(item.Rule, item.Confidence))
	}
	d.block(lines)
}

// audio reports a recording without depending on a live service. The reference
// is the dictionary's own; the URL exists only while the process that minted
// its token does.
func (d *document) audio(item *entryir.Audio, region entryir.Region) string {
	label := "audio " + regionLabel(region)
	if d.opts.AudioLinks && item.URL != "" {
		return "[🔊 " + label + "](" + item.URL + ")"
	}
	return "🔊 " + label
}

func regionLabel(region entryir.Region) string {
	switch region {
	case entryir.RegionUK:
		return "UK"
	case entryir.RegionUS:
		return "US"
	case entryir.RegionNeutral:
		return "shared"
	case entryir.RegionOther:
		return "unmarked"
	}
	return "unmarked"
}

// renderSense emits one sense and everything beneath it, indented by depth.
//
// The number printed is the dictionary's own when it has one. Bob's adapter
// generates display numbers instead, and deliberately so; here the source
// numbering is the more useful fact, because a snapshot exists to be compared
// against the record it came from.
func (d *document) renderSense(sense entryir.Sense, fallbackNumber string, depth int) []string {
	indent := strings.Repeat("  ", depth)
	number := strings.TrimSpace(sense.Number)
	if number == "" {
		number = fallbackNumber
	}

	var head strings.Builder
	head.WriteString(indent + "- **" + escape(number) + "**")
	if len(sense.Labels) > 0 {
		head.WriteString(" *(" + escape(strings.Join(sense.Labels, ", ")) + ")*")
	}
	if sense.Topic != "" {
		head.WriteString(" [" + escape(sense.Topic) + "]")
	}
	if sense.Definition != "" {
		head.WriteString(" " + escape(sense.Definition))
	}
	if sense.Translation != "" {
		if sense.Definition != "" {
			head.WriteString(" —")
		}
		head.WriteString(" " + escape(sense.Translation))
	}
	if sense.Grammar != "" {
		head.WriteString(" · `" + escape(sense.Grammar) + "`")
	}
	if len(sense.Patterns) > 0 {
		head.WriteString(" · `" + escape(strings.Join(sense.Patterns, " / ")) + "`")
	}
	head.WriteString(d.provenance(sense.Rule, sense.Confidence))

	lines := []string{head.String()}
	child := indent + "  - "
	if d.opts.IncludeExamples {
		for i, example := range sense.Examples {
			if i >= d.opts.MaxExamplesPerSense {
				break
			}
			line := child + "*" + escape(example.Text) + "*"
			if example.Translation != "" {
				line += " — " + escape(example.Translation)
			}
			if example.Audio != nil {
				line += " · " + d.audio(example.Audio, entryir.RegionOther)
			}
			lines = append(lines, line)
		}
	}
	if len(sense.Synonyms) > 0 {
		lines = append(lines, child+"syn: "+escape(strings.Join(sense.Synonyms, ", ")))
	}
	if len(sense.Antonyms) > 0 {
		lines = append(lines, child+"ant: "+escape(strings.Join(sense.Antonyms, ", ")))
	}
	for i, sub := range sense.Subsenses {
		lines = append(lines, d.renderSense(sub, number+"."+strconv.Itoa(i+1), depth+1)...)
	}
	return lines
}

func (d *document) renderForms(forms []entryir.Form, base int) {
	var lines []string
	for _, form := range forms {
		name := strings.TrimSpace(form.Name)
		if name == "" {
			name = "form"
		}
		line := "- **" + escape(name) + "**: " + escape(strings.Join(form.Words, ", "))
		if form.Audio != nil {
			line += " · " + d.audio(form.Audio, entryir.RegionOther)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return
	}
	d.heading(base, "Forms")
	d.block(lines)
}

func (d *document) renderPhrases(title string, entries []entryir.PhraseEntry, base int) {
	var lines []string
	for _, entry := range entries {
		var line strings.Builder
		line.WriteString("- ")
		if phrase := strings.TrimSpace(entry.Phrase); phrase != "" {
			line.WriteString("**" + escape(phrase) + "**")
			if entry.Definition != "" {
				line.WriteString(" — ")
			}
		}
		line.WriteString(escape(entry.Definition))
		if strings.TrimSpace(line.String()) == "-" {
			continue
		}
		lines = append(lines, line.String())
		if !d.opts.IncludeExamples {
			continue
		}
		for i, example := range entry.Examples {
			if i >= d.opts.MaxExamplesPerSense {
				break
			}
			item := "  - *" + escape(example.Text) + "*"
			if example.Translation != "" {
				item += " — " + escape(example.Translation)
			}
			lines = append(lines, item)
		}
	}
	if len(lines) == 0 {
		return
	}
	d.heading(base, title)
	d.block(lines)
}

func (d *document) renderList(title string, values []string, base int) {
	var lines []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			lines = append(lines, "- "+escape(trimmed))
		}
	}
	if len(lines) == 0 {
		return
	}
	d.heading(base, title)
	d.block(lines)
}

func (d *document) renderSection(title string, section entryir.Section, base int) {
	body := strings.TrimSpace(section.Body)
	if body == "" && len(section.Blocks) == 0 {
		return
	}
	if strings.TrimSpace(title) == "" {
		title = "Note"
	}
	d.heading(base, title)
	if len(section.Blocks) == 0 {
		d.line(escape(body))
		return
	}
	for _, block := range section.Blocks {
		switch block.Kind {
		case entryir.RichText:
			if text := escape(block.Text); text != "" {
				d.line(text)
			}
		case entryir.RichImage:
			if d.opts.ImageLinks && block.Image != nil && strings.TrimSpace(block.Image.URL) != "" {
				d.line("![" + escapeImageAlt(block.Image.Alt) + "](" + block.Image.URL + ")")
			}
		case entryir.RichTable:
			d.renderTable(block.Rows)
		}
	}
}

func (d *document) renderTable(rows [][]string) {
	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	if columns == 0 {
		return
	}
	normalizeRow := func(row []string) string {
		cells := make([]string, columns)
		for i := 0; i < columns; i++ {
			if i < len(row) {
				cells[i] = escapeTableCell(row[i])
			}
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	var lines []string
	lines = append(lines, normalizeRow(rows[0]))
	delimiters := make([]string, columns)
	for i := range delimiters {
		delimiters[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(delimiters, " | ")+" |")
	for _, row := range rows[1:] {
		lines = append(lines, normalizeRow(row))
	}
	d.block(lines)
}

func escapeTableCell(text string) string {
	return strings.ReplaceAll(escape(text), "|", `\|`)
}

func escapeImageAlt(text string) string {
	return strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, "\n", " ", "\r", " ").Replace(strings.TrimSpace(text))
}

func (d *document) block(lines []string) {
	if len(lines) == 0 {
		return
	}
	d.out.WriteString(strings.Join(lines, "\n"))
	d.out.WriteString("\n\n")
}

// provenance annotates a structure with the rule that produced it.
func (d *document) provenance(rule string, confidence float64) string {
	if !d.opts.IncludeProvenance || strings.TrimSpace(rule) == "" {
		return ""
	}
	return fmt.Sprintf(" `‹%s %.2f›`", rule, confidence)
}

// escape neutralises the Markdown syntax characters that occur in real
// dictionary text, and flattens embedded newlines so one IR field stays one
// Markdown element.
//
// It is deliberately narrow. Escaping every punctuation mark that Markdown
// could theoretically interpret turns Chinese and IPA text into backslash
// soup, and these snapshots exist to be read.
var escaper = strings.NewReplacer(
	`\`, `\\`,
	"`", "\\`",
	`*`, `\*`,
	`_`, `\_`,
	`[`, `\[`,
	`]`, `\]`,
	`<`, `\<`,
	`>`, `\>`,
	"\n", " ",
	"\r", " ",
)

func escape(text string) string {
	return strings.TrimSpace(escaper.Replace(text))
}

// Escape exposes the renderer's escaping to tooling that needs to look for IR
// text inside rendered output. Re-implementing it in the checker would let the
// two drift, and the check would then be testing the copy.
func Escape(text string) string { return escape(text) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
