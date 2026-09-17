package filter

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
)

// Mode is how a pattern is interpreted.
//
// It is explicit rather than guessed. Deciding by looking for metacharacters
// means `a.b` quietly stops being a literal the moment someone types a dot,
// and a rule saved today matches differently tomorrow depending on its
// spelling. The mode travels with the rule instead.
type Mode int

const (
	// Literal is a plain substring, and the default for anything that becomes
	// a saved rule: a filter should mean exactly what it says.
	Literal Mode = iota
	// Fuzzy matches the pattern's characters in order with anything between,
	// as a finder does. Good for reaching a line you half remember, too loose
	// to keep as a filter.
	Fuzzy
	// Regex is PCRE: lookaround, backreferences and lazy quantifiers all work.
	// A pattern that needs none of them runs on RE2, which is linear time.
	Regex
)

func (m Mode) String() string {
	switch m {
	case Fuzzy:
		return "fuzzy"
	case Regex:
		return "regex"
	default:
		return "literal"
	}
}

// ParseMode reads a mode name.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(s) {
	case "", "literal", "plain", "substring":
		return Literal, nil
	case "fuzzy":
		return Fuzzy, nil
	case "regex", "regexp", "pcre":
		return Regex, nil
	}
	return Literal, fmt.Errorf("unknown match mode %q, want literal, fuzzy or regex", s)
}

// matchTimeout bounds a single regex match.
//
// PCRE is a backtracking engine, so a pattern like (a+)+b can take
// exponential time on the wrong line. A log viewer reads lines it has never
// seen from files it did not write, so the engine needs a leash; RE2, which
// lv used before, could not express the syntax being asked for here.
var matchTimeout = 250 * time.Millisecond

// SetMatchTimeout replaces the bound on a single backtracking match. It
// applies to patterns compiled afterwards, and is meant to be set once, from
// the configuration, before any are.
func SetMatchTimeout(d time.Duration) {
	if d > 0 {
		matchTimeout = d
	}
}

// ------------------------------------------------------------------- fuzzy

type fuzzyMatcher struct {
	pat  []rune
	raw  string
	fold bool
}

func newFuzzy(pattern string, fold bool) *fuzzyMatcher {
	pat := []rune(pattern)
	if fold {
		for i, r := range pat {
			pat[i] = unicode.ToLower(r)
		}
	}
	return &fuzzyMatcher{pat: pat, raw: pattern, fold: fold}
}

func (f *fuzzyMatcher) Pattern() string { return f.raw }
func (f *fuzzyMatcher) Mode() Mode      { return Fuzzy }

func (f *fuzzyMatcher) Match(line []byte) bool {
	if len(f.pat) == 0 {
		return true
	}
	next := 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRune(line[i:])
		if size <= 0 {
			size = 1
		}
		if f.eq(r, f.pat[next]) {
			next++
			if next == len(f.pat) {
				return true
			}
		}
		i += size
	}
	return false
}

// Find returns the position of each character that matched, so a finder can
// show which parts of the line the pattern caught.
func (f *fuzzyMatcher) Find(line []byte, dst [][2]int, limit int) [][2]int {
	if len(f.pat) == 0 {
		return dst
	}
	start := len(dst)
	next := 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRune(line[i:])
		if size <= 0 {
			size = 1
		}
		if f.eq(r, f.pat[next]) {
			dst = append(dst, [2]int{i, i + size})
			next++
			if next == len(f.pat) {
				return dst
			}
		}
		i += size
	}
	// An incomplete run is not a match; drop what was gathered.
	return dst[:start]
}

func (f *fuzzyMatcher) eq(a, b rune) bool {
	if f.fold {
		return unicode.ToLower(a) == b
	}
	return a == b
}

// ------------------------------------------------------------------- regex

// newRegex compiles a pattern in Regex mode.
//
// Most patterns people write need nothing RE2 lacks, and RE2 is an order of
// magnitude faster over a large file: it matches bytes directly, where the
// backtracking engine decodes every line to runes first, and it cannot be
// driven into exponential time. So the backtracking engine is kept for the
// patterns that need it -- lookaround, backreferences -- and RE2 runs
// everything else.
//
// Which engine runs must not change what a pattern means. The backtracking
// engine is compiled in its RE2 compatibility mode, which reads \d, \w, \s,
// $ and POSIX classes as RE2 does while keeping every PCRE feature, and it
// always compiles first, so a pattern is accepted or rejected the same way
// whichever engine ends up running it.
func newRegex(pattern string, fold bool) (Matcher, error) {
	opts := regexp2.RegexOptions(regexp2.RE2)
	if fold {
		opts |= regexp2.IgnoreCase
	}
	back, err := regexp2.Compile(pattern, opts)
	if err != nil {
		return nil, err
	}

	goPattern := pattern
	if fold {
		goPattern = "(?i)" + pattern
	}
	back.MatchTimeout = matchTimeout
	slow := &pcreMatcher{re: back, raw: pattern}

	re, err := regexp.Compile(goPattern)
	if err != nil {
		return slow, nil
	}
	m := &re2Matcher{re: re, raw: pattern}
	if hasWordBoundary(re) {
		// The one place the engines still differ: the backtracking engine's
		// \b knows that é is a letter, and RE2's does not. They agree on
		// ASCII, so only lines that are not ASCII take the slow path.
		m.unicode = slow
	}
	return m, nil
}

func hasWordBoundary(re *regexp.Regexp) bool {
	tree, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return true
	}
	var walk func(*syntax.Regexp) bool
	walk = func(n *syntax.Regexp) bool {
		if n.Op == syntax.OpWordBoundary || n.Op == syntax.OpNoWordBoundary {
			return true
		}
		for _, sub := range n.Sub {
			if walk(sub) {
				return true
			}
		}
		return false
	}
	return walk(tree)
}

type re2Matcher struct {
	re  *regexp.Regexp
	raw string
	// unicode handles lines that are not ASCII, for a pattern whose meaning
	// there depends on the engine.
	unicode *pcreMatcher
}

func (r *re2Matcher) Pattern() string { return r.raw }
func (r *re2Matcher) Mode() Mode      { return Regex }

func (r *re2Matcher) Match(line []byte) bool {
	if r.unicode != nil && !isASCII(line) {
		return r.unicode.Match(line)
	}
	return r.re.Match(line)
}

func (r *re2Matcher) Find(line []byte, dst [][2]int, limit int) [][2]int {
	if r.unicode != nil && !isASCII(line) {
		return r.unicode.Find(line, dst, limit)
	}
	n := -1
	if limit > 0 {
		n = limit - len(dst)
		if n <= 0 {
			return dst
		}
	}
	for _, loc := range r.re.FindAllIndex(line, n) {
		if loc[1] > loc[0] {
			dst = append(dst, [2]int{loc[0], loc[1]})
		}
	}
	return dst
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

type pcreMatcher struct {
	re  *regexp2.Regexp
	raw string
}

// runeBufs keeps decoding buffers out of the allocator. A matcher is used by
// the renderer, a background scan and an export at once, so the buffer cannot
// live on the matcher itself.
var runeBufs = sync.Pool{New: func() any { s := make([]rune, 0, 256); return &s }}

func (p *pcreMatcher) Pattern() string { return p.raw }
func (p *pcreMatcher) Mode() Mode      { return Regex }

func (p *pcreMatcher) Match(line []byte) bool {
	buf := runeBufs.Get().(*[]rune)
	*buf = decodeRunes((*buf)[:0], line)
	ok, err := p.re.MatchRunes(*buf)
	runeBufs.Put(buf)
	// A timeout is reported as an error; treating it as "no match" keeps a
	// pathological pattern from stopping the scan dead.
	return err == nil && ok
}

// decoded is a line decoded to runes, with where each rune started.
type decoded struct {
	runes   []rune
	offsets []int
}

var decodedBufs = sync.Pool{New: func() any { return new(decoded) }}

func (p *pcreMatcher) Find(line []byte, dst [][2]int, limit int) [][2]int {
	d := decodedBufs.Get().(*decoded)
	defer decodedBufs.Put(d)
	d.decode(line)
	runes, offsets := d.runes, d.offsets

	m, err := p.re.FindRunesMatch(runes)
	for err == nil && m != nil {
		if limit > 0 && len(dst) >= limit {
			break
		}
		if m.Length > 0 {
			dst = append(dst, [2]int{byteAt(offsets, m.Index), byteAt(offsets, m.Index+m.Length)})
		} else if m.Index >= len(runes) {
			break // an empty match at the end would loop forever
		}
		m, err = p.re.FindNextMatch(m)
	}
	return dst
}

func decodeRunes(dst []rune, line []byte) []rune {
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRune(line[i:])
		if size <= 0 {
			size = 1
		}
		dst = append(dst, r)
		i += size
	}
	return dst
}

// decode also records where each rune started, so a match reported in rune
// positions can be turned back into byte positions for drawing.
func (d *decoded) decode(line []byte) {
	d.runes, d.offsets = d.runes[:0], d.offsets[:0]
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRune(line[i:])
		if size <= 0 {
			size = 1
		}
		d.runes = append(d.runes, r)
		d.offsets = append(d.offsets, i)
		i += size
	}
	d.offsets = append(d.offsets, len(line))
}

func byteAt(offsets []int, runeIndex int) int {
	if runeIndex < 0 {
		return 0
	}
	if runeIndex >= len(offsets) {
		return offsets[len(offsets)-1]
	}
	return offsets[runeIndex]
}
