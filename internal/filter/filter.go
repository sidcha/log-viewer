// Package filter decides which lines survive.
//
// Patterns are regular expressions, but a pattern containing no metacharacters
// compiles to a substring matcher instead. That matters: a full pass over a
// 100 GB log runs the predicate 10^9 times, and bytes.Index is an order of
// magnitude faster than even a literal-prefixed RE2 program.
package filter

import (
	"bytes"
	"strings"
)

// Kind is what a rule does to the lines it matches.
type Kind int

const (
	// Keep restricts the view to matching lines.
	Keep Kind = iota
	// Drop removes matching lines from the view.
	Drop
)

func (k Kind) String() string {
	if k == Keep {
		return "keep"
	}
	return "drop"
}

// Case selects case sensitivity.
type Case int

const (
	// Smart is case-insensitive unless the pattern contains an upper-case
	// letter, the behaviour people expect from vim and ripgrep.
	Smart Case = iota
	// Sensitive always respects case.
	Sensitive
	// Insensitive never respects case.
	Insensitive
)

// ParseCase turns a config value into a Case.
func ParseCase(s string) (Case, bool) {
	switch strings.ToLower(s) {
	case "smart":
		return Smart, true
	case "sensitive", "on", "true", "yes":
		return Sensitive, true
	case "insensitive", "off", "false", "no":
		return Insensitive, true
	}
	return Smart, false
}

// String is the name ParseCase accepts, so a case mode survives a round trip
// through a configuration or rules file.
func (c Case) String() string {
	switch c {
	case Sensitive:
		return "sensitive"
	case Insensitive:
		return "insensitive"
	default:
		return "smart"
	}
}

// Matcher tests a single line.
type Matcher interface {
	Match(line []byte) bool
	Pattern() string
	// Mode is how the pattern is being interpreted, so a rule can be saved
	// and reloaded meaning the same thing.
	Mode() Mode
	// Find returns byte ranges matched within line, appended to dst. It is used
	// for highlighting, never on the filtering hot path.
	Find(line []byte, dst [][2]int, limit int) [][2]int
}

// Compile builds a matcher for a pattern in a given mode.
func Compile(pattern string, mode Mode, c Case) (Matcher, error) {
	fold := foldFor(pattern, c)
	switch mode {
	case Fuzzy:
		return newFuzzy(pattern, fold), nil
	case Regex:
		return newRegex(pattern, fold)
	default:
		return newLiteral(pattern, fold), nil
	}
}

func foldFor(pattern string, c Case) bool {
	switch c {
	case Sensitive:
		return false
	case Insensitive:
		return true
	default:
		return !strings.ContainsAny(pattern, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	}
}

// literal matches a plain substring, optionally ASCII case-folded.
type literal struct {
	pat    []byte
	lo, up byte // first byte in both cases, for the folded scan
	fold   bool
	raw    string
}

func newLiteral(pattern string, fold bool) *literal {
	l := &literal{pat: []byte(pattern), fold: fold, raw: pattern}
	if len(l.pat) > 0 {
		l.lo, l.up = lowerASCII(l.pat[0]), upperASCII(l.pat[0])
	}
	return l
}

func (l *literal) Pattern() string { return l.raw }
func (l *literal) Mode() Mode      { return Literal }

func (l *literal) Match(line []byte) bool {
	if len(l.pat) == 0 {
		return true
	}
	if !l.fold {
		return bytes.Contains(line, l.pat)
	}
	return l.indexFold(line, 0) >= 0
}

func (l *literal) Find(line []byte, dst [][2]int, limit int) [][2]int {
	if len(l.pat) == 0 {
		return dst
	}
	for i := 0; limit <= 0 || len(dst) < limit; {
		var j int
		if l.fold {
			j = l.indexFold(line, i)
		} else {
			if k := bytes.Index(line[i:], l.pat); k < 0 {
				j = -1
			} else {
				j = i + k
			}
		}
		if j < 0 {
			break
		}
		dst = append(dst, [2]int{j, j + len(l.pat)})
		i = j + len(l.pat)
	}
	return dst
}

// indexFold finds the pattern from position start, ignoring ASCII case. It
// locates candidates with bytes.IndexByte on both cases of the first byte so
// the scan stays on the runtime's vectorised path.
func (l *literal) indexFold(line []byte, start int) int {
	n := len(l.pat)
	for i := start; i+n <= len(line); {
		rest := line[i : len(line)-n+1]
		k := bytes.IndexByte(rest, l.lo)
		if l.up != l.lo {
			if k2 := bytes.IndexByte(rest, l.up); k2 >= 0 && (k < 0 || k2 < k) {
				k = k2
			}
		}
		if k < 0 {
			return -1
		}
		i += k
		if equalFoldASCII(line[i:i+n], l.pat) {
			return i
		}
		i++
	}
	return -1
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

func upperASCII(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

func equalFoldASCII(a, b []byte) bool {
	for i := range a {
		if a[i] != b[i] && lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}
