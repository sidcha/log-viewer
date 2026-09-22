package highlight

import (
	"bytes"
	"path/filepath"

	"github.com/sidcha/log-viewer/internal/ansi"
)

// globBonus is what a filename glob adds to a profile's score, in percent.
// It is below the default threshold, so a name alone never decides.
const globBonus = 25

// sampleLineBytes bounds how much of a line detection looks at. Formats are
// told apart by their prefix.
const sampleLineBytes = 512

// Score picks the profile whose detect patterns match the largest share of
// sample, plus globBonus when base matches one of its globs. A profile with
// globs but no detect patterns scores exactly threshold on a glob match, so
// a profile that is only a filename still works but loses to any content
// match. It returns nil when nothing reaches threshold, and the score capped
// at 100 otherwise. sample holds non-blank lines with escapes removed.
func Score(profiles []*Profile, sample [][]byte, base string, threshold int) (*Profile, int) {
	var best *Profile
	bestPct, bestHits := -1, -1
	for _, p := range profiles {
		glob := base != "" && p.globMatches(base)
		var pct, hits int
		switch {
		case len(p.Detects) == 0 && glob:
			pct = threshold
		case len(p.Detects) == 0:
			continue
		default:
			for _, line := range sample {
				if p.detects(line) {
					hits++
				}
			}
			if len(sample) > 0 {
				pct = hits * 100 / len(sample)
			}
			if glob {
				pct += globBonus
			}
		}
		if pct < threshold {
			continue
		}
		if pct > bestPct || (pct == bestPct && hits > bestHits) {
			best, bestPct, bestHits = p, pct, hits
		}
	}
	if best == nil {
		return nil, 0
	}
	return best, min(bestPct, 100)
}

func (p *Profile) detects(line []byte) bool {
	for _, re := range p.Detects {
		if re.Match(line) {
			return true
		}
	}
	return false
}

func (p *Profile) globMatches(base string) bool {
	for _, g := range p.Globs {
		if ok, err := filepath.Match(g, base); err == nil && ok {
			return true
		}
	}
	return false
}

// Find returns the profile called name, or nil.
func Find(profiles []*Profile, name string) *Profile {
	for _, p := range profiles {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// visiblePrefix appends to dst at most n bytes of line with terminal escape
// sequences removed and a trailing carriage return dropped: what a person
// reading the line sees at its start.
func visiblePrefix(dst, line []byte, n int) []byte {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	for i := 0; i < len(line) && len(dst) < n; {
		if line[i] == 0x1b {
			if j, ok := ansi.Skip(line, i); ok {
				i = j
				continue
			}
		}
		dst = append(dst, line[i])
		i++
	}
	return dst
}

func isBlank(b []byte) bool { return len(bytes.TrimSpace(b)) == 0 }
