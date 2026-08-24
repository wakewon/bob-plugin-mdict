package mdict

import (
	"path"
	"regexp"
	"strings"
)

// linkPrefixes are the redirect markers seen in real MDX files. MDict writes
// "@@@LINK=", but a few builders emit the two-@ variant.
var linkPrefixes = []string{"@@@LINK=", "@@LINK="}

// ParseLinkTarget reports whether a record is a redirect stub and, if so, the
// headword it points at.
//
// Real records look like "@@@LINK=Hello!\r\n\x00" — note the trailing NUL,
// which ordinary whitespace trimming leaves behind.
func ParseLinkTarget(content []byte) (string, bool) {
	text := strings.Trim(string(content), "\x00\ufeff \t\r\n")
	for _, prefix := range linkPrefixes {
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		target := strings.Trim(strings.TrimPrefix(text, prefix), "\x00 \t\r\n")
		if target == "" {
			return "", false
		}
		return target, true
	}
	return "", false
}

// schemeRe matches the leading protocol of a resource reference.
var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

// NormalizeResourceKey converts any MDD key or entry reference into the single
// canonical form used by the resource index: lowercase, forward slashes, no
// leading separator, no scheme.
//
// MDD keys are stored Windows-style (for example "\\synthetic\\uk\\clip.mp3") while entry HTML
// refers to them with a `sound://` URL, so both forms must fold together.
func NormalizeResourceKey(ref string) string {
	value := strings.TrimSpace(ref)
	if value == "" {
		return ""
	}
	if loc := schemeRe.FindStringIndex(value); loc != nil {
		value = value[loc[1]:]
	}
	value = strings.ReplaceAll(value, "\\", "/")
	// Collapse repeated separators produced by escaped Windows paths.
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	value = strings.TrimPrefix(value, "/")
	// Strip a query or fragment appended by the dictionary's own JavaScript.
	if idx := strings.IndexAny(value, "?#"); idx >= 0 {
		value = value[:idx]
	}
	return strings.ToLower(value)
}

// ResourceCandidates expands a reference into the normalized keys worth trying,
// most specific first.
func ResourceCandidates(ref string) []string {
	primary := NormalizeResourceKey(ref)
	if primary == "" {
		return nil
	}
	candidates := []string{primary}
	// Some dictionaries reference a resource by bare filename while the MDD
	// stores it under a directory, and vice versa.
	if base := path.Base(primary); base != primary && base != "." && base != "/" {
		candidates = append(candidates, base)
	}
	return candidates
}

// audioExtensions are the container formats that appear in real MDDs.
var audioExtensions = map[string]string{
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".spx":  "audio/ogg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
}

// IsAudioRef reports whether a reference points at a pronunciation asset.
func IsAudioRef(ref string) bool {
	_, ok := audioExtensions[strings.ToLower(path.Ext(NormalizeResourceKey(ref)))]
	return ok
}

// IsSpeexRef reports whether a reference needs Speex decoding before macOS can
// play it.
func IsSpeexRef(ref string) bool {
	return strings.EqualFold(path.Ext(NormalizeResourceKey(ref)), ".spx")
}

// MIMEType maps a resource reference to the content type the resource endpoint
// should serve. SPX is reported as audio/wav because it is transcoded on the
// way out.
func MIMEType(ref string) string {
	ext := strings.ToLower(path.Ext(NormalizeResourceKey(ref)))
	if ext == ".spx" {
		return "audio/wav"
	}
	if mime, ok := audioExtensions[ext]; ok {
		return mime
	}
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
