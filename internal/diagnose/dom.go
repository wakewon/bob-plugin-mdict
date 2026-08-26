package diagnose

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/wakewon/bob-plugin-mdict/internal/parser"
)

// Count is one name and how often it occurred across the samples.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DOMSummary describes the markup conventions a dictionary uses.
//
// Everything in it is structural — tag names, class names, attribute names,
// reference schemes and counts. No dictionary text is recorded, which is what
// makes a corpus report safe to keep and to talk about.
type DOMSummary struct {
	Tags       []Count `json:"tags"`
	Classes    []Count `json:"classes"`
	Attributes []Count `json:"attributes"`
	// Signatures are the most common tag/class combinations, the closest thing
	// to "what this dictionary's entries are made of".
	Signatures []string `json:"signatures"`
	// ReferenceSchemes counts sound://, snd://, entry:// and similar links.
	ReferenceSchemes []Count `json:"referenceSchemes,omitempty"`
	MaxDepth         int     `json:"maxDepth"`
	MedianBytes      int     `json:"medianBytes"`
	// ClassVocabulary is the trimmed class list the family key is derived from.
	ClassVocabulary []string `json:"classVocabulary,omitempty"`
	// FamilyKey is a stable hash of that vocabulary. Two dictionaries sharing
	// it are built from the same template.
	FamilyKey string `json:"familyKey,omitempty"`
}

// familyVocabularySize is how many class names define a template family.
// Too few and unrelated dictionaries collide on ".def"; too many and two
// repacks of one dictionary look different because one of them added a class.
const familyVocabularySize = 12

// SummarizeDOM aggregates the markup conventions across a dictionary's samples.
func SummarizeDOM(samples []Sample) DOMSummary {
	var summary DOMSummary
	if len(samples) == 0 {
		return summary
	}
	tags := map[string]int{}
	classes := map[string]int{}
	attrs := map[string]int{}
	signatures := map[string]int{}
	schemes := map[string]int{}
	sizes := make([]int, 0, len(samples))

	for _, sample := range samples {
		sizes = append(sizes, len(sample.HTML))
		depth := 0
		var walk func(node *html.Node, level int)
		walk = func(node *html.Node, level int) {
			if level > depth {
				depth = level
			}
			if node.Type == html.ElementNode {
				tags[node.Data]++
				var classList []string
				for _, attr := range node.Attr {
					name := strings.ToLower(attr.Key)
					attrs[name]++
					switch name {
					case "class":
						classList = strings.Fields(attr.Val)
						for _, class := range classList {
							classes[class]++
						}
					case "href", "src", "addr", "data-src", "data-src-mp3":
						if scheme := referenceScheme(attr.Val); scheme != "" {
							schemes[scheme]++
						}
					}
				}
				for _, class := range classList {
					signatures[node.Data+"."+class]++
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child, level+1)
			}
		}
		walk(sample.Doc, 0)
		if depth > summary.MaxDepth {
			summary.MaxDepth = depth
		}
	}

	summary.Tags = topCounts(tags, 15)
	summary.Classes = topCounts(classes, 25)
	summary.Attributes = topCounts(attrs, 12)
	summary.ReferenceSchemes = topCounts(schemes, 8)
	for _, item := range topCounts(signatures, 10) {
		summary.Signatures = append(summary.Signatures, item.Name)
	}
	sort.Ints(sizes)
	summary.MedianBytes = sizes[len(sizes)/2]

	vocabulary := make([]string, 0, familyVocabularySize)
	for _, item := range topCounts(classes, familyVocabularySize) {
		vocabulary = append(vocabulary, item.Name)
	}
	sort.Strings(vocabulary)
	summary.ClassVocabulary = vocabulary
	if len(vocabulary) >= 4 {
		// Fewer than four shared class names is not a template, it is a
		// coincidence: ".def" and ".sense" alone are shared by everyone.
		digest := sha256.Sum256([]byte(strings.Join(vocabulary, "\x00")))
		summary.FamilyKey = hex.EncodeToString(digest[:])[:12]
	}
	return summary
}

// referenceScheme classifies a link target without recording the target.
func referenceScheme(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return ""
	}
	for _, scheme := range []string{"sound://", "snd://", "entry://", "file://", "bword://", "http://", "https://"} {
		if strings.HasPrefix(lower, scheme) {
			return strings.TrimSuffix(scheme, "//")
		}
	}
	if idx := strings.IndexAny(lower, "?#"); idx >= 0 {
		lower = lower[:idx]
	}
	for _, suffix := range []string{".mp3", ".spx", ".wav", ".ogg"} {
		if strings.HasSuffix(lower, suffix) {
			return "bare-audio"
		}
	}
	for _, suffix := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg"} {
		if strings.HasSuffix(lower, suffix) {
			return "bare-image"
		}
	}
	return ""
}

func topCounts(counts map[string]int, limit int) []Count {
	out := make([]Count, 0, len(counts))
	for name, count := range counts {
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, Count{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// PronunciationEvidence separates what the MDX claims from what the MDD can
// actually deliver.
//
// A corpus of MDX files without their MDD volumes is the normal case for
// compatibility analysis. Reporting "no audio" for those would confuse a
// missing resource pack with a parser failure, so reference detection and
// resource resolution are counted apart and never combined into one verdict.
type PronunciationEvidence struct {
	// IPASamples is how many samples yielded a transcription.
	IPASamples int `json:"ipaSamples"`
	// AudioRefSamples is how many samples reference a recording in their markup.
	AudioRefSamples int `json:"audioRefSamples"`
	// RegionMarkers are the accent markers found around those references.
	RegionMarkers []string `json:"regionMarkers,omitempty"`
	// MDDVolumes is how many resource volumes accompany the MDX.
	MDDVolumes int `json:"mddVolumes"`
	// AudioResolved counts references that resolve to a real MDD asset. It is
	// only meaningful when MDDVolumes is non-zero.
	AudioResolved int `json:"audioResolved"`
}

// collectPronunciationEvidence inspects the markup for pronunciation facts.
func collectPronunciationEvidence(samples []Sample, resolve func(ref string) bool) PronunciationEvidence {
	var evidence PronunciationEvidence
	regions := map[string]struct{}{}
	for _, sample := range samples {
		hasRef, hasIPA := false, false
		parser.Walk(sample.Doc, func(node *html.Node) bool {
			if !hasIPA && parser.LooksLikeIPA(parser.Text(node, parser.TextOptions{SkipHidden: true})) {
				hasIPA = true
			}
			for _, name := range []string{"href", "src", "addr", "data-src", "data-src-mp3"} {
				value := parser.Attr(node, name)
				if value == "" || referenceScheme(value) == "" {
					continue
				}
				if !isAudioReference(value) {
					continue
				}
				hasRef = true
				switch parser.DetectRegion(parser.DescriptorText(node), strings.ToLower(value)) {
				case "uk":
					regions["uk"] = struct{}{}
				case "us":
					regions["us"] = struct{}{}
				default:
					regions["unmarked"] = struct{}{}
				}
				if resolve != nil && resolve(value) {
					evidence.AudioResolved++
				}
			}
			return true
		})
		if hasRef {
			evidence.AudioRefSamples++
		}
		if hasIPA {
			evidence.IPASamples++
		}
	}
	for region := range regions {
		evidence.RegionMarkers = append(evidence.RegionMarkers, region)
	}
	sort.Strings(evidence.RegionMarkers)
	return evidence
}

func isAudioReference(value string) bool {
	lower := strings.ToLower(value)
	if idx := strings.IndexAny(lower, "?#"); idx >= 0 {
		lower = lower[:idx]
	}
	for _, suffix := range []string{".mp3", ".spx", ".wav", ".ogg", ".m4a"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return strings.HasPrefix(lower, "sound://") || strings.HasPrefix(lower, "snd://")
}
