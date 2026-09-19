// Package ruleset reads and writes the rules a viewing session was built from.
//
// Working out the right set of filters over an unfamiliar log takes real
// effort, and that effort is worth more than the session it was made in. A
// rules file turns it into something that can be kept, shared, committed next
// to the service it describes, and handed back to lv the next time the same
// problem appears.
//
// The format is JSON so it can be written by hand or generated, and every field
// except the pattern has a sensible default so a minimal file stays short.
package ruleset

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sidcha/log-viewer/internal/atomicfile"
	"github.com/sidcha/log-viewer/internal/filter"
)

// Version is the format version written to new files.
const Version = 1

// File is the on-disk shape of a saved session.
type File struct {
	Version int `json:"version"`
	// Case is the matching mode the rules were built under. Patterns are
	// compiled against it on load, so a set saved under `sensitive` does not
	// quietly widen when reloaded somewhere configured for smart case.
	Case  string `json:"case,omitempty"`
	Rules []Rule `json:"rules"`
}

// Rule is one saved rule.
type Rule struct {
	Action  string `json:"action"`
	Pattern string `json:"pattern"`
	// Match is how the pattern is read: literal or regex. It is part of what
	// the rule means, so it travels with it; absent means literal.
	Match string `json:"match,omitempty"`
	// Disabled rather than Enabled, so the common case is an absent field and
	// a hand-written file needs only an action and a pattern.
	Disabled bool `json:"disabled,omitempty"`
	// Color is #rrggbb. Left out, lv assigns one on load.
	Color string `json:"color,omitempty"`
}

// Empty reports whether the file would restore nothing.
func (f *File) Empty() bool {
	return f == nil || len(f.Rules) == 0
}

// From captures a rule set for saving.
//
// A search is deliberately not part of this. It hides nothing and is thrown
// away the moment the next one is typed, so treating it as saveable state
// would mean asking about work nobody did.
func From(set *filter.Set, cs filter.Case) *File {
	f := &File{Version: Version, Case: cs.String()}
	for _, r := range set.All() {
		mode := ""
		if r.M.Mode() != filter.Literal {
			mode = r.M.Mode().String()
		}
		f.Rules = append(f.Rules, Rule{
			Action:   r.Action.String(),
			Pattern:  r.M.Pattern(),
			Match:    mode,
			Disabled: !r.Enabled,
			Color:    colorHex(r.Color),
		})
	}
	return f
}

// Set compiles the file back into a rule set.
//
// fallback is used when the file does not name a case mode, so a hand-written
// file inherits whatever the viewer is configured for.
func (f *File) Set(fallback filter.Case) (*filter.Set, filter.Case, error) {
	cs := fallback
	if f.Case != "" {
		parsed, ok := filter.ParseCase(f.Case)
		if !ok {
			return nil, cs, fmt.Errorf("unknown case %q", f.Case)
		}
		cs = parsed
	}

	set := filter.NewSet()
	for i, r := range f.Rules {
		action, err := filter.ParseAction(r.Action)
		if err != nil {
			return nil, cs, fmt.Errorf("rule %d: %w", i+1, err)
		}
		if r.Pattern == "" {
			return nil, cs, fmt.Errorf("rule %d: empty pattern", i+1)
		}
		mode, err := filter.ParseMode(r.Match)
		if err != nil {
			return nil, cs, fmt.Errorf("rule %d: %w", i+1, err)
		}
		m, err := filter.Compile(r.Pattern, mode, cs)
		if err != nil {
			return nil, cs, fmt.Errorf("rule %d: %w", i+1, err)
		}
		col, err := parseHex(r.Color)
		if err != nil {
			return nil, cs, fmt.Errorf("rule %d: %w", i+1, err)
		}
		set = set.Add(filter.Rule{
			Action:  action,
			M:       m,
			Enabled: !r.Disabled,
			Color:   col,
		})
	}
	return set, cs, nil
}

// Load reads a rules file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Version > Version {
		return nil, fmt.Errorf("%s: written by a newer lv (format %d, this build understands %d)",
			path, f.Version, Version)
	}
	return &f, nil
}

// Save writes a rules file, creating any missing directories.
//
// An interrupted save cannot leave a half-written set in place of a good one.
func (f *File) Save(path string) error {
	f.Version = Version

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	return atomicfile.Write(path, 0o644, func(w io.Writer) error {
		_, err := w.Write(b)
		return err
	})
}

// Digest is a stable summary used to tell whether anything has changed since
// the set was last saved or loaded, so quitting an untouched session does not
// ask about saving.
func (f *File) Digest() string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "case=%s\n", f.Case)
	for _, r := range f.Rules {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%v\t%s\n", r.Action, r.Pattern, r.Match, r.Disabled, r.Color)
	}
	return b.String()
}

func colorHex(c filter.Color) string {
	if c == (filter.Color{}) {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func parseHex(s string) (filter.Color, error) {
	if s == "" {
		return filter.Color{}, nil
	}
	var r, g, b uint8
	if len(s) != 7 || s[0] != '#' {
		return filter.Color{}, fmt.Errorf("colour %q: expected #rrggbb", s)
	}
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return filter.Color{}, fmt.Errorf("colour %q: %w", s, err)
	}
	return filter.Color{R: r, G: g, B: b}, nil
}
