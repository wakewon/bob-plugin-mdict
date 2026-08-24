package parser

import (
	"fmt"
	"io"
	"strings"
)

func fprintf(w io.Writer, format string, args ...any) { fmt.Fprintf(w, format, args...) }

// dedupeStrings removes duplicates in place, preserving first-seen order and
// dropping empties.
func dedupeStrings(values *[]string) {
	if values == nil || len(*values) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(*values))
	out := (*values)[:0]
	for _, value := range *values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	*values = out
}

// splitList breaks a comma/semicolon separated list into trimmed items.
func splitList(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '、' || r == '|'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.Trim(Normalize(field), " .:"); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
