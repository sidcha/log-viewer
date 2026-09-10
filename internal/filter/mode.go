package filter

import (
	"fmt"
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
const matchTimeout = 250 * time.Millisecond

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

// -------------------------------------------------------------------- PCRE

type pcreMatcher struct {
	re  *regexp2.Regexp
	raw string
}

// runeBufs keeps decoding buffers out of the allocator. A matcher is used by
// the renderer, a background scan and an export at once, so the buffer cannot
// live on the matcher itself.
var runeBufs = sync.Pool{New: func() any { s := make([]rune, 0, 256); return &s }}

func newPCRE(pattern string, fold bool) (*pcreMatcher, error) {
	opts := regexp2.None
	if fold {
		opts |= regexp2.IgnoreCase
	}
	re, err := regexp2.Compile(pattern, opts)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = matchTimeout
	return &pcreMatcher{re: re, raw: pattern}, nil
}

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

func (p *pcreMatcher) Find(line []byte, dst [][2]int, limit int) [][2]int {
	runes, offsets := decodeWithOffsets(line)

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

// decodeWithOffsets also records where each rune started, so a match reported
// in rune positions can be turned back into byte positions for drawing.
func decodeWithOffsets(line []byte) ([]rune, []int) {
	runes := make([]rune, 0, len(line))
	offsets := make([]int, 0, len(line)+1)
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRune(line[i:])
		if size <= 0 {
			size = 1
		}
		runes = append(runes, r)
		offsets = append(offsets, i)
		i += size
	}
	return runes, append(offsets, len(line))
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
