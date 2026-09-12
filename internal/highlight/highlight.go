// Package highlight styles log lines.
//
// Two layers produce byte-range spans over a line: syntax profiles, which
// recognise the shape of common log formats, and rule marks, which show where
// the patterns the user is hunting for actually matched. Marks are emitted last
// and therefore win where they overlap.
//
// There is deliberately no programming-language support here. The formats that
// matter are kernel ring buffers, syslog, journal output and JSON records.
package highlight

import (
	"path/filepath"
	"regexp"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/filter"
)

// Span is a styled byte range within a line.
type Span struct {
	Start, End int
	Style      tcell.Style
}

// Rule is one syntax pattern within a profile.
type Rule struct {
	Re        *regexp.Regexp
	Group     int // 0 styles the whole match, N styles capture group N
	Style     tcell.Style
	StyleName string
}

// Profile is a named set of syntax rules for one family of log file.
type Profile struct {
	Name   string
	Globs  []string
	Detect *regexp.Regexp
	Rules  []Rule
}

// Mark is a pattern whose matches are painted wherever they appear, carrying
// the colour assigned to the rule that asked for it.
type Mark struct {
	M     filter.Matcher
	Style tcell.Style
}

// Engine computes spans for a line. It is not safe for concurrent use; the UI
// owns one and reuses its scratch buffers across every line of every frame.
type Engine struct {
	Profile *Profile
	Marks   []Mark

	// MaxScan bounds how many bytes of a line are examined. A single 10 MB JSON
	// record must not be allowed to stall a frame; the rest renders unstyled.
	MaxScan int
	// MaxMatches bounds repeats of one rule within a line.
	MaxMatches int

	spans []Span
	locs  [][2]int
}

// NewEngine returns an engine with sane bounds.
func NewEngine(maxScan, maxMatches int) *Engine {
	if maxScan <= 0 {
		maxScan = 4096
	}
	if maxMatches <= 0 {
		maxMatches = 64
	}
	return &Engine{MaxScan: maxScan, MaxMatches: maxMatches}
}

// SyntaxSpans returns what the profile makes of the line. The result is valid
// until the next call to either span method.
//
// It is separate from the marks because anything the line itself carries --
// the colours a log captured from a terminal was written with -- belongs
// between the two: lv's guess at the shape of the line should not overpaint
// what the producer actually said, and neither should overpaint the patterns
// the user is hunting for.
func (e *Engine) SyntaxSpans(line []byte) []Span {
	line = e.bound(line)
	e.spans = e.spans[:0]

	if e.Profile != nil {
		for i := range e.Profile.Rules {
			e.addRule(line, &e.Profile.Rules[i])
		}
	}
	return e.spans
}

// MarkSpans returns where the rules the user added matched.
func (e *Engine) MarkSpans(line []byte) []Span {
	line = e.bound(line)
	e.spans = e.spans[:0]

	for i := range e.Marks {
		mk := &e.Marks[i]
		e.locs = mk.M.Find(line, e.locs[:0], e.MaxMatches)
		for _, l := range e.locs {
			e.spans = append(e.spans, Span{Start: l[0], End: l[1], Style: mk.Style})
		}
	}
	return e.spans
}

func (e *Engine) bound(line []byte) []byte {
	if len(line) > e.MaxScan {
		return line[:e.MaxScan]
	}
	return line
}

func (e *Engine) addRule(line []byte, r *Rule) {
	if r.Group <= 0 {
		for _, loc := range r.Re.FindAllIndex(line, e.MaxMatches) {
			if loc[1] > loc[0] {
				e.spans = append(e.spans, Span{Start: loc[0], End: loc[1], Style: r.Style})
			}
		}
		return
	}
	g := 2 * r.Group
	for _, m := range r.Re.FindAllSubmatchIndex(line, e.MaxMatches) {
		if g+1 < len(m) && m[g] >= 0 && m[g+1] > m[g] {
			e.spans = append(e.spans, Span{Start: m[g], End: m[g+1], Style: r.Style})
		}
	}
}

// Select picks the profile for a file: an explicit name wins, then a filename
// glob, then a content sniff against the head of the file, then "generic".
func Select(profiles []*Profile, name, path string, head []byte) *Profile {
	if name != "" {
		for _, p := range profiles {
			if p.Name == name {
				return p
			}
		}
		return nil
	}

	base := filepath.Base(path)
	for _, p := range profiles {
		for _, g := range p.Globs {
			if ok, err := filepath.Match(g, base); err == nil && ok {
				return p
			}
		}
	}

	for _, p := range profiles {
		if p.Detect != nil && p.Detect.Match(head) {
			return p
		}
	}

	for _, p := range profiles {
		if p.Name == "generic" {
			return p
		}
	}
	return nil
}

// Names lists the available profile names, for the help screen and for
// validating `--profile`.
func Names(profiles []*Profile) []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Name)
	}
	return out
}
