package highlight

import (
	"bytes"
	"regexp"
)

// Marker is a line that starts a new segment of a log: a bootloader's
// banner, a kernel's first words. Lit is looked for first, with a plain
// substring search, because the scan runs on every line of the file and
// almost none are markers; Re, when set, must also match.
//
// A marker with a Profile names what follows. One without only says that
// something new starts, and the segment's sample decides what.
type Marker struct {
	Lit     []byte
	Re      *regexp.Regexp
	Profile *Profile
}

// markerWindow is how much of a line, escapes removed, is searched for a
// marker. Markers are banners and hand-offs, which lead their line.
const markerWindow = 128

// findMarker returns the first marker, in config order, that text holds.
func findMarker(ms []*Marker, text []byte) *Marker {
	for _, m := range ms {
		if bytes.Contains(text, m.Lit) && (m.Re == nil || m.Re.Match(text)) {
			return m
		}
	}
	return nil
}

// markerScan is findMarker for every line of a file. A substring search per
// marker costs a dozen passes over each line; instead each literal gets an
// anchor, one of its bytes that is rare in logs, shared with other literals
// where it can be, and one vectorised byte search per anchor finds where a
// literal could sit. Only a line holding a literal goes on to findMarker.
type markerScan struct {
	ms      []*Marker
	anchors []anchor
	// emptyLit is a literal with no bytes to anchor, which every line holds.
	emptyLit bool
}

type anchor struct {
	b    byte
	lits []anchoredLit
}

// anchoredLit is a literal whose byte at off is its anchor.
type anchoredLit struct {
	lit []byte
	off int
}

func newMarkerScan(ms []*Marker) markerScan {
	s := markerScan{ms: ms}
	var lits [][]byte
	for _, m := range ms {
		if len(m.Lit) == 0 {
			s.emptyLit = true
		}
		lits = append(lits, m.Lit)
	}
	if !s.emptyLit {
		s.anchors = chooseAnchors(lits)
	}
	return s
}

func (s *markerScan) find(text []byte) *Marker {
	if !s.emptyLit && !s.holdsLiteral(text) {
		return nil
	}
	return findMarker(s.ms, text)
}

func (s *markerScan) holdsLiteral(text []byte) bool {
	for _, a := range s.anchors {
		for i := 0; i < len(text); i++ {
			k := bytes.IndexByte(text[i:], a.b)
			if k < 0 {
				break
			}
			i += k
			for _, l := range a.lits {
				start, end := i-l.off, i-l.off+len(l.lit)
				if start >= 0 && end <= len(text) &&
					text[start] == l.lit[0] && text[end-1] == l.lit[len(l.lit)-1] &&
					bytes.Equal(text[start:end], l.lit) {
					return true
				}
			}
		}
	}
	return false
}

// commonLogBytes lists the bytes most frequent in log text, most frequent
// first. Any byte not listed, capitals included, is taken to be rarer than
// all of them.
const commonLogBytes = " 0e1t2a3r4i5o6n7s8l9c.d:u-mh[]p/g=f_,byw()vk'\"x\tzqj"

func logFrequency(b byte) int {
	if i := bytes.IndexByte([]byte(commonLogBytes), b); i >= 0 {
		return len(commonLogBytes) - i
	}
	return 0
}

// chooseAnchors covers every literal with as few, and as rare, anchors as
// it can: it takes the rarest byte of the literals not yet covered, the one
// most of them hold when several are as rare, until all are covered.
// Every literal must be non-empty.
func chooseAnchors(left [][]byte) []anchor {
	var anchors []anchor
	for len(left) > 0 {
		var holders [256]int
		for _, lit := range left {
			var seen [256]bool
			for _, c := range lit {
				if !seen[c] {
					seen[c] = true
					holders[c]++
				}
			}
		}
		best := -1
		for c := range holders {
			if holders[c] == 0 {
				continue
			}
			if best < 0 || logFrequency(byte(c)) < logFrequency(byte(best)) ||
				logFrequency(byte(c)) == logFrequency(byte(best)) && holders[c] > holders[best] {
				best = c
			}
		}
		a := anchor{b: byte(best)}
		rest := left[:0]
		for _, lit := range left {
			if off := bytes.IndexByte(lit, a.b); off >= 0 {
				a.lits = append(a.lits, anchoredLit{lit: lit, off: off})
			} else {
				rest = append(rest, lit)
			}
		}
		left = rest
		anchors = append(anchors, a)
	}
	return anchors
}
