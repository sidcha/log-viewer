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
	Name  string
	Globs []string
	// Detects are matched against each sampled line; the share of lines any
	// of them matches is the profile's score.
	Detects []*regexp.Regexp
	// Items is the profile as written; Rules is what it means, every use
	// replaced by the rules of its token set. Expand fills Rules in.
	Items []Item
	Rules []Rule
}

// Item is one line of a profile or token set as written: a rule, or the
// name of a token set to insert in its place.
type Item struct {
	Rule  Rule
	Use   string
	Where string // file:line of a use, for errors
}

// TokenSet is a named list of rules that profiles pull in with `use`, so
// the tokens every log shares are written once.
type TokenSet struct {
	Name  string
	Items []Item
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

	// cache holds the syntax spans of lines drawn recently. A frame repaints
	// every visible line, and running every profile rule over each of them
	// again when nothing moved is most of what a frame costs.
	cache map[LineKey][]Span
}

// LineKey names a line's bytes: where it starts, how long it was when read,
// and the index generation, which changes when the file is replaced. A
// partial last line that grows gets a new key.
type LineKey struct {
	Gen uint64
	Off int64
	Len int
}

// maxCached bounds the span cache; it is dropped wholesale when it fills,
// which costs one frame's recomputation now and then.
const maxCached = 1024

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

// SyntaxSpansAt is SyntaxSpans for a line with a stable identity, reusing
// the answer from the last time the same line was drawn. The result must not
// be modified.
func (e *Engine) SyntaxSpansAt(key LineKey, line []byte) []Span {
	if spans, ok := e.cache[key]; ok {
		return spans
	}
	spans := append([]Span(nil), e.SyntaxSpans(line)...)
	if e.cache == nil || len(e.cache) >= maxCached {
		e.cache = make(map[LineKey][]Span, maxCached)
	}
	e.cache[key] = spans
	return spans
}

// Forget drops every cached span, for when the profile or the bounds change.
func (e *Engine) Forget() { e.cache = nil }

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

// Select picks the profile for a file: an explicit name wins; otherwise the
// head of the file is scored, and "generic" is the answer when nothing fits.
func Select(profiles []*Profile, name, path string, head []byte) *Profile {
	if name != "" {
		return Find(profiles, name)
	}
	sample := headSample(head, DefaultDetectLines)
	if p, _ := Score(profiles, sample, filepath.Base(path), DefaultDetectThreshold); p != nil {
		return p
	}
	return Find(profiles, "generic")
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
