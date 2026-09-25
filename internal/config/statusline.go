package config

import (
	"fmt"
	"slices"
	"strings"
)

// StatusFields are the names a status-left or status-right template may use
// in braces. The viewer draws each; a test keeps the two lists agreeing.
var StatusFields = []string{
	"file", "path", "line", "lines", "percent", "size",
	"flags", "progress", "error", "keys", "profile",
}

// IsStatusField reports whether name may appear in a status template.
func IsStatusField(name string) bool { return slices.Contains(StatusFields, name) }

// StatusPart is one piece of a status template: literal Text, or a Field
// the viewer fills in.
type StatusPart struct {
	Text  string
	Field string
}

// ParseStatus splits a status template into literal text and {field}
// references. {{ writes a literal brace.
func ParseStatus(tmpl string) ([]StatusPart, error) {
	var parts []StatusPart
	var text strings.Builder
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '{' {
			text.WriteByte(tmpl[i])
			continue
		}
		if strings.HasPrefix(tmpl[i:], "{{") {
			text.WriteByte('{')
			i++
			continue
		}
		end := strings.IndexByte(tmpl[i:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unclosed { at column %d", i+1)
		}
		name := tmpl[i+1 : i+end]
		if !IsStatusField(name) {
			return nil, fmt.Errorf("unknown field %q; fields are %s", name, strings.Join(StatusFields, " "))
		}
		if text.Len() > 0 {
			parts = append(parts, StatusPart{Text: text.String()})
			text.Reset()
		}
		parts = append(parts, StatusPart{Field: name})
		i += end
	}
	if text.Len() > 0 {
		parts = append(parts, StatusPart{Text: text.String()})
	}
	return parts, nil
}
