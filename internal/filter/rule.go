package filter

import (
	"fmt"
	"strings"
)

// Action is what a rule does with the lines it matches.
type Action int

const (
	// Only restricts the view to matching lines.
	Only Action = iota
	// Never removes matching lines from the view.
	Never
	// Highlight marks matches where they appear without changing which lines
	// are shown.
	Highlight
)

func (a Action) String() string {
	switch a {
	case Only:
		return "only"
	case Never:
		return "never"
	default:
		return "highlight"
	}
}

// ParseAction reads an action name.
func ParseAction(s string) (Action, error) {
	switch strings.ToLower(s) {
	case "only", "keep":
		return Only, nil
	case "never", "drop", "out":
		return Never, nil
	case "highlight", "mark":
		return Highlight, nil
	}
	return Only, fmt.Errorf("unknown action %q, want only, never or highlight", s)
}

// Rule is one pattern and what to do with the lines it matches.
type Rule struct {
	Action  Action
	M       Matcher
	Enabled bool
	// Color marks this rule's matches on screen. Every rule carries one, so a
	// line kept by one rule and a line marked by another are told apart at a
	// glance.
	Color Color
}

// Set is the ordered list of rules the viewer is applying.
//
// Rules are flat rather than nested. A nested filter would need one pass over
// the file per level; one pass evaluates every rule, which is the difference
// between seconds and minutes at the sizes this is built for.
//
// A line is shown when it matches at least one enabled Only rule (or there are
// none) and matches no enabled Never rule, which is `grep -e … | grep -v …`.
// Highlight rules never change which lines are shown.
//
// A Set is immutable once handed to a scan; every mutating method returns a new
// one, so background scans never lock.
type Set struct {
	rules []Rule
	onlys int
}

// NewSet returns an empty set.
func NewSet() *Set { return &Set{} }

// All exposes the rules for display. The slice must not be modified.
func (s *Set) All() []Rule { return s.rules }

// Len is the number of rules, of every action.
func (s *Set) Len() int { return len(s.rules) }

// Count returns how many rules have the given action.
func (s *Set) Count(a Action) int {
	n := 0
	for _, r := range s.rules {
		if r.Action == a {
			n++
		}
	}
	return n
}

// Filtering reports whether any enabled rule changes which lines are shown.
// Highlight-only sets do not, so the viewer can keep using the cheaper
// unfiltered view.
func (s *Set) Filtering() bool {
	for _, r := range s.rules {
		if r.Enabled && r.Action != Highlight {
			return true
		}
	}
	return false
}

// Colors lists the colours already in use, so a new rule can avoid them.
func (s *Set) Colors() []Color {
	out := make([]Color, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, r.Color)
	}
	return out
}

func (s *Set) clone() *Set {
	n := &Set{rules: make([]Rule, len(s.rules))}
	copy(n.rules, s.rules)
	n.recount()
	return n
}

func (s *Set) recount() {
	s.onlys = 0
	for _, r := range s.rules {
		if r.Enabled && r.Action == Only {
			s.onlys++
		}
	}
}

// Add returns a set with r appended. If r has no colour one is chosen that is
// distinct from the colours already present.
func (s *Set) Add(r Rule) *Set {
	n := s.clone()
	if r.Color == (Color{}) {
		r.Color = NextColor(s.Colors())
	}
	n.rules = append(n.rules, r)
	n.recount()
	return n
}

// Pop returns a set without its last rule.
func (s *Set) Pop() *Set {
	if len(s.rules) == 0 {
		return s
	}
	n := s.clone()
	n.rules = n.rules[:len(n.rules)-1]
	n.recount()
	return n
}

// Remove returns a set without rule i.
func (s *Set) Remove(i int) *Set {
	if i < 0 || i >= len(s.rules) {
		return s
	}
	n := s.clone()
	n.rules = append(n.rules[:i], n.rules[i+1:]...)
	n.recount()
	return n
}

// Toggle returns a set with rule i enabled or disabled.
func (s *Set) Toggle(i int) *Set {
	if i < 0 || i >= len(s.rules) {
		return s
	}
	n := s.clone()
	n.rules[i].Enabled = !n.rules[i].Enabled
	n.recount()
	return n
}

// SetAll returns a set with every rule enabled or disabled, which is how a
// momentary look at the unfiltered log works without losing the rules.
func (s *Set) SetAll(enabled bool) *Set {
	n := s.clone()
	for i := range n.rules {
		n.rules[i].Enabled = enabled
	}
	n.recount()
	return n
}

// ClearAction returns a set without any rule of the given action.
func (s *Set) ClearAction(a Action) *Set {
	n := &Set{}
	for _, r := range s.rules {
		if r.Action != a {
			n.rules = append(n.rules, r)
		}
	}
	n.recount()
	return n
}

// Clear returns an empty set.
func (s *Set) Clear() *Set { return NewSet() }

// Pass reports whether line belongs in the view. Highlight rules are ignored
// here; they mark text, they do not filter.
func (s *Set) Pass(line []byte) bool {
	if len(s.rules) == 0 {
		return true
	}
	ok := s.onlys == 0
	for i := range s.rules {
		r := &s.rules[i]
		if !r.Enabled {
			continue
		}
		switch r.Action {
		case Never:
			if r.M.Match(line) {
				return false
			}
		case Only:
			if !ok && r.M.Match(line) {
				ok = true
			}
		}
	}
	return ok
}

// Fingerprint identifies the filtering behaviour of the set. A scan is
// restarted when it changes and left alone when it does not, so adding a
// highlight does not throw away a filter scan in progress.
func (s *Set) Fingerprint() string {
	var b strings.Builder
	for _, r := range s.rules {
		if !r.Enabled || r.Action == Highlight {
			continue
		}
		b.WriteString(r.Action.String())
		b.WriteByte(':')
		b.WriteString(r.M.Pattern())
		b.WriteByte('\n')
	}
	return b.String()
}
