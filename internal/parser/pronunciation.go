package parser

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// parsePronunciations builds the IPA-plus-audio list.
//
// The hard part is pairing: a naive implementation attaches the first audio
// file it finds to both UK and US, which silently mislabels every American clip
// as British. Pairing here is driven by region evidence on each candidate
// independently, and unmatched audio is only merged into a pronunciation that
// agrees on region.
func (s *parseState) parsePronunciations() {
	var found []entryir.Pronunciation

	if s.profile != nil && len(s.profile.pronunciation) > 0 {
		found = s.pronunciationsFromProfile()
	}
	if len(found) == 0 {
		found = s.pronunciationsGeneric()
	}

	found = mergePronunciations(found)
	// Drop entries that carry neither a transcription nor a playable file.
	kept := found[:0]
	for _, item := range found {
		if item.IPA == "" && item.Audio == nil {
			continue
		}
		kept = append(kept, item)
	}
	s.entry.Pronunciations = kept
	s.note("pronunciations: %d", len(kept))
}

func (s *parseState) pronunciationsFromProfile() []entryir.Pronunciation {
	var out []entryir.Pronunciation
	for _, rule := range s.profile.pronunciation {
		for _, node := range QueryAllNested(s.doc, rule.selector) {
			item := s.pronunciationFromNode(node, rule)
			if item.IPA == "" && item.Audio == nil {
				continue
			}
			out = append(out, item)
		}
	}
	return out
}

func (s *parseState) pronunciationFromNode(node *html.Node, rule compiledPronunciation) entryir.Pronunciation {
	item := entryir.Pronunciation{Confidence: 0.95, Rule: "profile:" + rule.selector.String()}

	if !rule.ipa.IsEmpty() {
		for _, ipaNode := range QueryAllNested(node, rule.ipa) {
			if candidate := CleanIPA(Text(ipaNode, TextOptions{SkipHidden: true})); candidate != "" {
				item.IPA = candidate
				break
			}
		}
	} else {
		item.IPA = CleanIPA(Text(node, TextOptions{SkipHidden: true}))
	}
	// A profile selector can legitimately point at an audio-only link.
	if item.IPA != "" && !LooksLikeIPA(item.IPA) && len([]rune(item.IPA)) > 40 {
		item.IPA = ""
		item.Confidence = 0.7
	}

	if !rule.noAudio {
		attrs := rule.audio
		if len(attrs) == 0 {
			attrs = audioAttrs
		}
		item.Audio = s.resolveAudioFrom(node, attrs)
	}

	switch rule.region {
	case "uk":
		item.Region = entryir.RegionUK
	case "us":
		item.Region = entryir.RegionUS
	case "other":
		item.Region = entryir.RegionOther
	default:
		item.Region = s.detectRegionFor(node, item.Audio)
		if item.Region == entryir.RegionOther {
			item.Confidence = 0.6
		}
	}
	return item
}

// detectRegionFor combines DOM context with the audio filename, which is often
// the only unambiguous signal ("breProns/run_n0205.mp3").
func (s *parseState) detectRegionFor(node *html.Node, audio *entryir.Audio) entryir.Region {
	descriptors := []string{DescriptorText(node)}
	if audio != nil {
		descriptors = append(descriptors, strings.ToLower(audio.ResourceRef))
	}
	return DetectRegion(descriptors...)
}

// resolveAudioFrom looks for a resource reference on the node or, failing that,
// on its descendants.
func (s *parseState) resolveAudioFrom(node *html.Node, attrs []string) *entryir.Audio {
	if s.opts.Audio == nil || node == nil {
		return nil
	}
	if audio := s.audioFromAttrs(node, attrs); audio != nil {
		return audio
	}
	var found *entryir.Audio
	Walk(node, func(child *html.Node) bool {
		if found != nil {
			return false
		}
		if child == node {
			return true
		}
		if audio := s.audioFromAttrs(child, attrs); audio != nil {
			found = audio
			return false
		}
		return true
	})
	return found
}

func (s *parseState) audioFromAttrs(node *html.Node, attrs []string) *entryir.Audio {
	for _, name := range attrs {
		value := strings.TrimSpace(Attr(node, name))
		if value == "" || !looksLikeAudioRef(value) {
			continue
		}
		if audio := s.opts.Audio.ResolveAudio(value); audio != nil {
			return audio
		}
	}
	return nil
}

var audioSuffixes = []string{".mp3", ".wav", ".ogg", ".spx", ".m4a", ".aac", ".flac"}

func looksLikeAudioRef(value string) bool {
	lower := strings.ToLower(value)
	if idx := strings.IndexAny(lower, "?#"); idx >= 0 {
		lower = lower[:idx]
	}
	for _, suffix := range audioSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// pronunciationsGeneric finds pronunciations in a dictionary with no profile.
//
// It works from two independent kinds of evidence — text that looks like IPA,
// and links that point at audio files — then pairs them by region and by how
// close they are in the document.
func (s *parseState) pronunciationsGeneric() []entryir.Pronunciation {
	type candidate struct {
		node  *html.Node
		ipa   string
		audio *entryir.Audio
		order int
	}
	var candidates []candidate
	order := 0

	Walk(s.doc, func(node *html.Node) bool {
		order++
		var item candidate
		item.node = node
		item.order = order

		// Only consider leafy nodes for IPA so an outer wrapper does not
		// swallow the whole pronunciation block as one transcription.
		if isTextLeaf(node) {
			text := Text(node, TextOptions{SkipHidden: true})
			if LooksLikeIPA(text) {
				item.ipa = CleanIPA(text)
			}
		}
		item.audio = s.audioFromAttrs(node, audioAttrs)
		if item.ipa == "" && item.audio == nil {
			return true
		}
		candidates = append(candidates, item)
		return true
	})

	var out []entryir.Pronunciation
	for _, item := range candidates {
		region := DetectRegion(DescriptorText(item.node), audioRefOf(item.audio))
		confidence := 0.8
		if region == entryir.RegionOther {
			confidence = 0.55
		}
		out = append(out, entryir.Pronunciation{
			Region:     region,
			IPA:        item.ipa,
			Audio:      item.audio,
			Confidence: confidence,
			Rule:       "generic:evidence",
		})
	}
	return out
}

func audioRefOf(audio *entryir.Audio) string {
	if audio == nil {
		return ""
	}
	return strings.ToLower(audio.ResourceRef)
}

// isTextLeaf reports whether a node's text comes only from its own subtree of
// inline formatting, not from nested structural blocks.
func isTextLeaf(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		if blockTags[child.Data] {
			return false
		}
		if !isTextLeaf(child) {
			return false
		}
	}
	return true
}

// mergePronunciations folds the raw candidates into at most one entry per
// region, joining a transcription with the audio that shares its region.
func mergePronunciations(items []entryir.Pronunciation) []entryir.Pronunciation {
	byRegion := map[entryir.Region]*entryir.Pronunciation{}
	var order []entryir.Region

	for i := range items {
		item := items[i]
		existing, ok := byRegion[item.Region]
		if !ok {
			copied := item
			byRegion[item.Region] = &copied
			order = append(order, item.Region)
			continue
		}
		if existing.IPA == "" && item.IPA != "" {
			existing.IPA = item.IPA
		}
		if existing.Audio == nil && item.Audio != nil {
			existing.Audio = item.Audio
		}
		if item.Confidence > existing.Confidence {
			existing.Confidence = item.Confidence
		}
	}

	// A transcription with no regional evidence can borrow the audio of a
	// region only when no other transcription claims it. Anything left over
	// stays under RegionOther rather than being promoted to uk or us.
	if other, ok := byRegion[entryir.RegionOther]; ok && other.IPA != "" {
		for _, region := range []entryir.Region{entryir.RegionUK, entryir.RegionUS} {
			if target, exists := byRegion[region]; exists && target.IPA == "" {
				target.IPA = other.IPA
				target.Confidence = minFloat(target.Confidence, 0.7)
			}
		}
	}

	out := make([]entryir.Pronunciation, 0, len(order))
	for _, region := range []entryir.Region{entryir.RegionUK, entryir.RegionUS, entryir.RegionOther} {
		if item, ok := byRegion[region]; ok {
			out = append(out, *item)
		}
	}
	return out
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
