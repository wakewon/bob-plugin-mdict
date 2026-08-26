package validate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/diagnose"
)

// Review artifacts are Markdown files on disk, not a dashboard.
//
// Markdown is diffable, greppable, readable in any editor, and costs nothing
// to build. It is also the format the experimental renderer already produces,
// so a reviewer comparing the source record with the parse is reading one of
// them beside the other in the same window.
//
// These files contain real dictionary text — that is the whole point of them —
// so they are written only into a git-ignored location and never committed.

// Write produces the complete review artifact set under dir.
func Write(dir string, run *Run) error {
	// A rerun must not leave last run's snapshots behind: the queue changes as
	// the parser does, and a stale file next to a fresh one is exactly how a
	// fixed problem gets reported twice.
	for _, sub := range []string{"snapshots", "dictionaries"} {
		if err := os.RemoveAll(filepath.Join(dir, sub)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "dictionaries"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "run.json"), []byte(encodeJSON(run)+"\n"), 0o644); err != nil {
		return err
	}

	paths := make(map[string]string, len(run.Queue))
	for i, snapshot := range run.Queue {
		name := fmt.Sprintf("%03d-%s-%s.md", i+1, snapshot.DictionaryID[:8], slug(snapshot.Key))
		if err := os.WriteFile(filepath.Join(dir, "snapshots", name),
			[]byte(renderSnapshot(snapshot, run)), 0o644); err != nil {
			return err
		}
		paths[snapshot.identity()] = "snapshots/" + name
	}
	for _, dictionary := range run.Dictionaries {
		if dictionary.Report.Container.ID == "" {
			continue
		}
		name := dictionary.Report.Container.ID + ".md"
		if err := os.WriteFile(filepath.Join(dir, "dictionaries", name),
			[]byte(renderDictionary(dictionary, run, paths)), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "README.md"), []byte(renderIndex(run, paths)), 0o644)
}

func renderIndex(run *Run, paths map[string]string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# MDX validation run\n\n")
	fmt.Fprintf(&out, "- corpus: `%s`\n", run.Root)
	fmt.Fprintf(&out, "- generated: %s\n", run.GeneratedAt)
	fmt.Fprintf(&out, "- dictionaries: **%d** (healthy %d, unavailable %d)\n", run.Total, run.Healthy, run.Unavailable)
	fmt.Fprintf(&out, "- records validated: **%d**\n", run.Aggregate.Records)
	fmt.Fprintf(&out, "- review queue: **%d**\n\n", len(run.Queue))

	out.WriteString(`Three different things are measured here and they must not be confused.

**Structural coverage** is whether the parser produced a field of a given kind.
**Automated semantic consistency** is whether that output is a plausible
reading of the record — retention, duplication, dominance. **Semantic
correctness** is whether it is *right*, and nothing automatic in this
directory measures it; that is what the review queue is for.

`)

	out.WriteString("## Product priority\n\n")
	out.WriteString("| tier | dictionaries |\n|---|---:|\n")
	for _, item := range run.Tiers {
		fmt.Fprintf(&out, "| %s | %d |\n", item.Name, item.Count)
	}

	out.WriteString("\n## Quality by tier\n\n")
	out.WriteString("| tier | dicts | records | structured | fallback | retention | duplication | flagged |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, tier := range run.Aggregate.ByTier {
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %d | %.0f%% | %.1f%% | %d |\n",
			tier.Tier, tier.Dictionaries, tier.Records, tier.Structured, tier.Fallback,
			100*tier.MeanRetention, 100*tier.MeanDuplication, tier.Flagged)
	}

	out.WriteString("\n## Parser selection\n\n")
	for _, item := range run.Parsers {
		fmt.Fprintf(&out, "- `%s`: %d\n", item.Name, item.Count)
	}

	out.WriteString("\n## Which rule produced the structure\n\n")
	out.WriteString("| rule | structures |\n|---|---:|\n")
	for _, item := range run.Rules {
		fmt.Fprintf(&out, "| `%s` | %d |\n", item.Name, item.Count)
	}

	if len(run.Signals) > 0 {
		out.WriteString("\n## Automatic signals\n\n")
		out.WriteString("| signal | records |\n|---|---:|\n")
		for _, item := range run.Signals {
			fmt.Fprintf(&out, "| `%s` | %d |\n", item.Name, item.Count)
		}
	}
	if len(run.Failures) > 0 {
		out.WriteString("\n## Backend parity failures\n\n")
		out.WriteString("| invariant | records |\n|---|---:|\n")
		for _, item := range run.Failures {
			fmt.Fprintf(&out, "| `%s` | %d |\n", item.Name, item.Count)
		}
	} else {
		out.WriteString("\n## Backend parity\n\nEvery invariant held on every validated record.\n")
	}

	if run.Comparison != nil {
		fmt.Fprintf(&out, "\n## Against the baseline of %s\n\n", run.Comparison.BaselineAt)
		out.WriteString("| classification | records |\n|---|---:|\n")
		for _, item := range run.Comparison.Counts {
			fmt.Fprintf(&out, "| %s | %d |\n", item.Name, item.Count)
		}
		if len(run.Comparison.Missing) > 0 {
			fmt.Fprintf(&out, "\n%d baseline records were not reproduced in this run.\n", len(run.Comparison.Missing))
		}
	}

	out.WriteString("\n## Review queue\n\n")
	out.WriteString("| # | tier | dictionary | key | parser | ret | dup | signals |\n")
	out.WriteString("|---:|---|---|---|---|---:|---:|---|\n")
	for i, snapshot := range run.Queue {
		link := paths[snapshot.identity()]
		fmt.Fprintf(&out, "| [%d](%s) | %s | %s | %s | `%s` | %.0f%% | %.0f%% | %s |\n",
			i+1, link, snapshot.Tier, escapeCell(snapshot.DictionaryTitle), escapeCell(snapshot.Key),
			snapshot.Parser, 100*snapshot.Metrics.Retention, 100*snapshot.Metrics.Duplication,
			strings.Join(snapshot.Signals, " "))
	}

	out.WriteString("\n## Every dictionary\n\n")
	out.WriteString("| tier | dictionary | records | structured | retention | duplication | parser | link |\n")
	out.WriteString("|---|---|---:|---:|---:|---:|---|---|\n")
	for _, dictionary := range run.Dictionaries {
		container := dictionary.Report.Container
		structured := 0
		for _, snapshot := range dictionary.Snapshots {
			if snapshot.Fields.Senses > 0 {
				structured++
			}
		}
		health := ""
		if container.Health != "ok" {
			health = " ⚠️unavailable"
		}
		fmt.Fprintf(&out, "| %s | %s%s | %d | %d | %.0f%% | %.0f%% | `%s` | [detail](dictionaries/%s.md) |\n",
			dictionary.Language.Tier.Short(), escapeCell(container.Title), health,
			len(dictionary.Snapshots), structured,
			100*dictionary.MeanRetention, 100*dictionary.MeanDuplication,
			dictionary.RuntimeProfile, container.ID)
	}
	return out.String()
}

func renderDictionary(dictionary DictionaryResult, run *Run, paths map[string]string) string {
	var out strings.Builder
	container := dictionary.Report.Container
	fmt.Fprintf(&out, "# %s\n\n", container.Title)
	fmt.Fprintf(&out, "[← index](../README.md)\n\n")
	fmt.Fprintf(&out, "- id: `%s`\n- health: %s\n- entries: %d\n- MDD volumes: %d\n",
		container.ID, container.Health, container.EntryCount, container.MDDVolumes)
	fmt.Fprintf(&out, "- runtime parser: `%s`\n", dictionary.RuntimeProfile)
	fmt.Fprintf(&out, "- diagnostic evidence: `%s` (%s over %d samples)\n",
		dictionary.Report.Profile.Selected, dictionary.Report.Profile.Strength, dictionary.Report.Profile.Samples)
	fmt.Fprintf(&out, "- priority: **%s**\n", dictionary.Language.Tier.Label())
	fmt.Fprintf(&out, "- key script: %s · content scripts: %s\n",
		dictionary.Language.KeyScript, strings.Join(dictionary.Language.ContentScripts, ", "))
	if len(dictionary.Language.Evidence) > 0 {
		fmt.Fprintf(&out, "- classification evidence: %s\n", strings.Join(dictionary.Language.Evidence, "; "))
	}
	fmt.Fprintf(&out, "- mean retention: %.0f%% · mean duplication: %.0f%%\n\n",
		100*dictionary.MeanRetention, 100*dictionary.MeanDuplication)

	if len(dictionary.Report.Warnings) > 0 {
		out.WriteString("## Dictionary-level signals\n\n")
		for _, warning := range dictionary.Report.Warnings {
			fmt.Fprintf(&out, "- `%s` — %s\n", warning.Code, warning.Detail)
		}
		out.WriteString("\n")
	}
	if len(dictionary.Rules) > 0 {
		out.WriteString("## Rules used\n\n")
		for _, rule := range dictionary.Rules {
			fmt.Fprintf(&out, "- `%s`: %d\n", rule.Name, rule.Count)
		}
		out.WriteString("\n")
	}

	out.WriteString("## Validated records\n\n")
	out.WriteString("| key | records | senses | defs | tr | ex | ret | dup | largest field | signals | snapshot |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---|---|---|\n")
	for _, snapshot := range dictionary.Snapshots {
		link := ""
		if path, ok := paths[snapshot.identity()]; ok {
			link = "[open](../" + path + ")"
		}
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %d | %d | %.0f%% | %.0f%% | %s %.0f%% | %s | %s |\n",
			escapeCell(snapshot.Key), snapshot.Records,
			snapshot.Fields.Senses+snapshot.Fields.Subsenses, snapshot.Fields.Definitions,
			snapshot.Fields.Translations, snapshot.Fields.Examples,
			100*snapshot.Metrics.Retention, 100*snapshot.Metrics.Duplication,
			snapshot.Metrics.LargestFieldKind, 100*snapshot.Metrics.LargestFieldShare,
			strings.Join(snapshot.Signals, " "), link)
	}
	return out.String()
}

// renderSnapshot writes the whole semantic chain for one record, so a reviewer
// can compare the source against every representation of it without leaving
// the file.
func renderSnapshot(snapshot Snapshot, run *Run) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s — %s\n\n", snapshot.Key, snapshot.DictionaryTitle)
	fmt.Fprintf(&out, "[← index](../README.md) · [dictionary](../dictionaries/%s.md)\n\n", snapshot.DictionaryID)

	out.WriteString("## Metadata\n\n")
	fmt.Fprintf(&out, "| | |\n|---|---|\n")
	fmt.Fprintf(&out, "| dictionary | %s |\n", escapeCell(snapshot.DictionaryTitle))
	fmt.Fprintf(&out, "| dictionary id | `%s` |\n", snapshot.DictionaryID)
	fmt.Fprintf(&out, "| priority tier | %s |\n", snapshot.Tier)
	fmt.Fprintf(&out, "| lookup key | %s |\n", escapeCell(snapshot.Key))
	if snapshot.MatchedKey != snapshot.Key {
		fmt.Fprintf(&out, "| matched MDX key | %s |\n", escapeCell(snapshot.MatchedKey))
	}
	fmt.Fprintf(&out, "| parser | `%s` (evidence %s) |\n", snapshot.Parser, snapshot.Evidence)
	for _, candidate := range snapshot.profileEv.Candidates {
		fmt.Fprintf(&out, "| profile candidate | `%s` matched %d/%d (score %d) |\n",
			candidate.ID, candidate.Matched, snapshot.profileEv.Samples, candidate.Score)
	}
	fmt.Fprintf(&out, "| source record hash | `%s` |\n", snapshot.RecordHash)
	fmt.Fprintf(&out, "| EntrySet hash | `%s` |\n", snapshot.EntryHash)
	fmt.Fprintf(&out, "| records | %d visible of %d raw |\n", snapshot.Records, snapshot.RawRecords)
	fmt.Fprintf(&out, "| rules used | %s |\n", joinNames(snapshot.Rules, 8))
	fmt.Fprintf(&out, "| queue score | %d |\n", snapshot.Score)
	if len(snapshot.Reasons) > 0 {
		fmt.Fprintf(&out, "| queued because | %s |\n", escapeCell(strings.Join(snapshot.Reasons, "; ")))
	}
	if len(snapshot.Signals) > 0 {
		fmt.Fprintf(&out, "| signals | `%s` |\n", strings.Join(snapshot.Signals, "` `"))
	}
	if run.Comparison != nil {
		if change, reason := run.Comparison.Verdict(snapshot); change != "" {
			fmt.Fprintf(&out, "| versus baseline | **%s** %s |\n", change, escapeCell(reason))
		}
	}

	out.WriteString("\n## Automated consistency\n\n")
	metrics := snapshot.Metrics
	fmt.Fprintf(&out, "| measure | value | meaning |\n|---|---:|---|\n")
	fmt.Fprintf(&out, "| content retention | %.0f%% | share of the record's tokens the parse accounts for |\n", 100*metrics.Retention)
	fmt.Fprintf(&out, "| duplication | %.0f%% | share of output tokens beyond what the record contains |\n", 100*metrics.Duplication)
	fmt.Fprintf(&out, "| largest field | %.0f%% (%s) | biggest single extracted field, as a share of the record |\n",
		100*metrics.LargestFieldShare, metrics.LargestFieldKind)
	fmt.Fprintf(&out, "| source / output tokens | %d / %d | |\n", metrics.SourceTokens, metrics.OutputTokens)
	if metrics.Scope < 0.999 {
		fmt.Fprintf(&out, "| in scope | %.0f%% of %d record tokens | the profile's root/ignore rules exclude the rest |\n",
			100*metrics.Scope, metrics.RecordTokens)
	}
	if metrics.RepeatedDefinitions > 0 {
		fmt.Fprintf(&out, "| repeated definitions | %d | |\n", metrics.RepeatedDefinitions)
	}
	if metrics.RepeatedExamples > 0 {
		fmt.Fprintf(&out, "| repeated examples | %d | |\n", metrics.RepeatedExamples)
	}
	if metrics.SubsenseEchoesParent > 0 {
		fmt.Fprintf(&out, "| subsenses repeating their parent | %d | |\n", metrics.SubsenseEchoesParent)
	}
	if metrics.SectionEchoesSense > 0 {
		fmt.Fprintf(&out, "| sections repeating a sense | %d | |\n", metrics.SectionEchoesSense)
	}
	fmt.Fprintf(&out, "\nFields: %s\n", describeFields(snapshot.Fields))

	out.WriteString("\n## Backend parity\n\n")
	for _, check := range snapshot.checks {
		mark := "✅"
		detail := ""
		if !check.OK {
			mark = "❌"
			detail = " — " + check.Detail
		}
		fmt.Fprintf(&out, "- %s `%s`%s\n", mark, check.Name, detail)
	}

	out.WriteString("\n## Source record\n\n")
	fmt.Fprintf(&out, "%s\n\n", describeRecord(snapshot))
	out.WriteString("### Visible text\n\n```text\n")
	out.WriteString(clip(snapshot.sourceText, 6000))
	out.WriteString("\n```\n\n### HTML\n\n```html\n")
	out.WriteString(clip(prettyHTML(snapshot.sourceHTML), 24000))
	out.WriteString("\n```\n")

	out.WriteString("\n## Markdown rendering\n\n")
	out.WriteString("Rendered from the canonical EntrySet by `internal/mdrender`, the sibling of the Bob adapter.\n\n")
	out.WriteString("---\n\n")
	out.WriteString(indentQuote(snapshot.markdown))
	out.WriteString("\n---\n")

	out.WriteString("\n## Plain Text rendering\n\n")
	out.WriteString("Rendered directly from the canonical EntrySet by `internal/textrender`.\n\n```text\n")
	out.WriteString(clip(snapshot.plain, 12000))
	out.WriteString("\n```\n")

	out.WriteString("\n## Parsed EntrySet (canonical IR)\n\n```json\n")
	out.WriteString(clip(snapshot.irJSON, 24000))
	out.WriteString("\n```\n")

	out.WriteString("\n## Simulated Bob result\n\n")
	out.WriteString("What `internal/bobadapter` would return for this EntrySet, in Bob's default separate-record mode.\n\n```json\n")
	out.WriteString(clip(snapshot.bobJSON, 16000))
	out.WriteString("\n```\n")
	return out.String()
}

func describeFields(fields Fields) string {
	parts := []string{}
	for _, item := range []struct {
		name  string
		value int
	}{
		{"parts", fields.Parts}, {"senses", fields.Senses}, {"subsenses", fields.Subsenses},
		{"definitions", fields.Definitions}, {"translations", fields.Translations},
		{"examples", fields.Examples}, {"example translations", fields.ExampleTranslations},
		{"labels", fields.Labels}, {"IPA", fields.IPA}, {"audio", fields.Audio},
		{"forms", fields.Forms}, {"phrases", fields.Phrases},
		{"cross-references", fields.CrossReferences}, {"sections", fields.Sections},
		{"fallback sections", fields.Fallback},
	} {
		if item.value > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", item.name, item.value))
		}
	}
	if len(parts) == 0 {
		return "*nothing extracted*"
	}
	return strings.Join(parts, " · ")
}

// describeRecord summarises the source markup without reproducing it, which is
// the same structural vocabulary the corpus diagnostics report in.
func describeRecord(snapshot Snapshot) string {
	doc, err := html.Parse(bytes.NewReader(snapshot.sourceHTML))
	if err != nil {
		return ""
	}
	summary := diagnose.SummarizeDOM([]diagnose.Sample{{Key: snapshot.Key, HTML: snapshot.sourceHTML, Doc: doc}})
	var tags, classes []string
	for _, item := range summary.Tags {
		tags = append(tags, fmt.Sprintf("%s(%d)", item.Name, item.Count))
	}
	for _, item := range summary.Classes {
		classes = append(classes, fmt.Sprintf("%s(%d)", item.Name, item.Count))
	}
	return fmt.Sprintf("%d bytes, depth %d\n\n- tags: %s\n- classes: %s\n- signatures: %s",
		len(snapshot.sourceHTML), summary.MaxDepth,
		strings.Join(head(tags, 12), " "), strings.Join(head(classes, 16), " "),
		strings.Join(head(summary.Signatures, 8), " "))
}

// prettyHTML indents a record so its structure is legible. Dictionaries ship
// entries as a single line of several kilobytes; reading one unformatted is
// how a reviewer misses the element that matters.
func prettyHTML(raw []byte) string {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return string(raw)
	}
	var out strings.Builder
	var walk func(node *html.Node, depth int)
	walk = func(node *html.Node, depth int) {
		switch node.Type {
		case html.TextNode:
			text := strings.TrimSpace(node.Data)
			if text != "" {
				fmt.Fprintf(&out, "%s%s\n", strings.Repeat("  ", depth), text)
			}
			return
		case html.ElementNode:
			var attrs strings.Builder
			for _, attr := range node.Attr {
				fmt.Fprintf(&attrs, " %s=%q", attr.Key, attr.Val)
			}
			fmt.Fprintf(&out, "%s<%s%s>\n", strings.Repeat("  ", depth), node.Data, attrs.String())
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, depth+1)
		}
		if node.Type == html.ElementNode {
			fmt.Fprintf(&out, "%s</%s>\n", strings.Repeat("  ", depth), node.Data)
		}
	}
	walk(doc, 0)
	return out.String()
}

// indentQuote nests a rendered document inside the review file without its
// headings colliding with the review file's own.
func indentQuote(markdown string) string {
	lines := strings.Split(strings.TrimRight(markdown, "\n"), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			lines[i] = "###" + line
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n… clipped, " + fmt.Sprint(len(runes)-limit) + " characters omitted"
}

func escapeCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

// slug makes a filename out of a headword. Real keys contain slashes, quotes
// and every kind of space; the artifacts are local, so the text itself is kept
// where it is safe to keep.
func slug(key string) string {
	var out []rune
	for _, r := range key {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out = append(out, unicode.ToLower(r))
		case len(out) > 0 && out[len(out)-1] != '-':
			out = append(out, '-')
		}
		if len(out) >= 32 {
			break
		}
	}
	trimmed := strings.Trim(string(out), "-")
	if trimmed == "" {
		return "entry"
	}
	return trimmed
}
