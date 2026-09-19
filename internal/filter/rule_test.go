package filter

import (
	"testing"

	"github.com/sidcha/log-viewer/internal/color"
)

func rule(t *testing.T, a Action, pat string) Rule {
	t.Helper()
	m, err := Compile(pat, Literal, Sensitive)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pat, err)
	}
	return Rule{Action: a, M: m, Enabled: true}
}

func TestParseAction(t *testing.T) {
	for in, want := range map[string]Action{
		"only": Only, "keep": Only,
		"never": Never, "drop": Never, "out": Never,
		"highlight": Highlight, "mark": Highlight,
		"ONLY": Only,
	} {
		got, err := ParseAction(in)
		if err != nil || got != want {
			t.Errorf("ParseAction(%q) = (%v, %v), want %v", in, got, err, want)
		}
	}
	if _, err := ParseAction("sideways"); err == nil {
		t.Error("ParseAction should reject an unknown action")
	}
}

func TestPassSemantics(t *testing.T) {
	lines := []string{"alpha one", "beta two", "gamma three", "alpha noisy"}

	cases := []struct {
		name string
		set  func(*testing.T) *Set
		want []bool
	}{
		{
			"empty shows everything",
			func(t *testing.T) *Set { return NewSet() },
			[]bool{true, true, true, true},
		},
		{
			"only rules are or-ed",
			func(t *testing.T) *Set {
				return NewSet().Add(rule(t, Only, "alpha")).Add(rule(t, Only, "beta"))
			},
			[]bool{true, true, false, true},
		},
		{
			"never beats only",
			func(t *testing.T) *Set {
				return NewSet().Add(rule(t, Only, "alpha")).Add(rule(t, Never, "noisy"))
			},
			[]bool{true, false, false, false},
		},
		{
			"never alone keeps the rest",
			func(t *testing.T) *Set { return NewSet().Add(rule(t, Never, "noisy")) },
			[]bool{true, true, true, false},
		},
		{
			"highlight does not filter",
			func(t *testing.T) *Set { return NewSet().Add(rule(t, Highlight, "alpha")) },
			[]bool{true, true, true, true},
		},
		{
			"highlight alongside a filter does not widen it",
			func(t *testing.T) *Set {
				return NewSet().Add(rule(t, Only, "alpha")).Add(rule(t, Highlight, "beta"))
			},
			[]bool{true, false, false, true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.set(t)
			for i, l := range lines {
				if got := s.Pass([]byte(l)); got != tc.want[i] {
					t.Errorf("Pass(%q) = %v, want %v", l, got, tc.want[i])
				}
			}
		})
	}
}

func TestFilteringIgnoresHighlights(t *testing.T) {
	s := NewSet().Add(rule(t, Highlight, "x"))
	if s.Filtering() {
		t.Error("a set of only highlights should not be filtering")
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}

	s = s.Add(rule(t, Never, "y"))
	if !s.Filtering() {
		t.Error("adding a never rule should start filtering")
	}
	if s.SetAll(false).Filtering() {
		t.Error("disabling everything should stop filtering")
	}
}

func TestSetIsImmutable(t *testing.T) {
	base := NewSet().Add(rule(t, Only, "x"))
	derived := base.Add(rule(t, Never, "y"))

	if base.Len() != 1 || derived.Len() != 2 {
		t.Fatalf("lengths = %d, %d; want 1, 2", base.Len(), derived.Len())
	}
	if !base.Pass([]byte("x y")) {
		t.Error("a scan holding the original set must keep seeing its behaviour")
	}
	if derived.Pass([]byte("x y")) {
		t.Error("derived set should drop on y")
	}
}

func TestClearAction(t *testing.T) {
	s := NewSet().
		Add(rule(t, Only, "a")).
		Add(rule(t, Highlight, "b")).
		Add(rule(t, Highlight, "c")).
		Add(rule(t, Never, "d"))

	if got := s.Count(Highlight); got != 2 {
		t.Fatalf("Count(Highlight) = %d, want 2", got)
	}

	s = s.ClearAction(Highlight)
	if s.Len() != 2 || s.Count(Highlight) != 0 {
		t.Fatalf("after ClearAction: Len = %d, highlights = %d", s.Len(), s.Count(Highlight))
	}
	if !s.Filtering() {
		t.Error("clearing highlights should leave the filters alone")
	}
}

// TestFingerprintIgnoresHighlights is the property that lets a highlight be
// added without throwing away a filter scan that is already running.
func TestFingerprintIgnoresHighlights(t *testing.T) {
	s := NewSet().Add(rule(t, Only, "a"))
	before := s.Fingerprint()

	if got := s.Add(rule(t, Highlight, "b")).Fingerprint(); got != before {
		t.Error("adding a highlight changed the filtering fingerprint")
	}
	if got := s.Add(rule(t, Never, "b")).Fingerprint(); got == before {
		t.Error("adding a never rule should change the fingerprint")
	}
	if got := s.Toggle(0).Fingerprint(); got == before {
		t.Error("disabling a filter should change the fingerprint")
	}
}

func TestRemoveAndToggle(t *testing.T) {
	s := NewSet().Add(rule(t, Only, "a")).Add(rule(t, Never, "b"))

	if got := s.Remove(0).Len(); got != 1 {
		t.Errorf("Remove: Len = %d, want 1", got)
	}
	if s.Len() != 2 {
		t.Error("Remove mutated the original set")
	}
	if s.Remove(9).Len() != 2 {
		t.Error("Remove with an out-of-range index should be a no-op")
	}
	if s.Toggle(1).All()[1].Enabled {
		t.Error("Toggle did not disable the rule")
	}
	if !s.Pop().All()[0].Enabled || s.Pop().Len() != 1 {
		t.Error("Pop should drop the last rule only")
	}
	if NewSet().Pop().Len() != 0 {
		t.Error("Pop on an empty set should be a no-op")
	}
}

func TestEveryRuleGetsAColor(t *testing.T) {
	s := NewSet()
	for i := 0; i < 8; i++ {
		s = s.Add(rule(t, Highlight, string(rune('a'+i))))
	}
	for i, r := range s.All() {
		if r.Color == (color.RGB{}) {
			t.Errorf("rule %d was not assigned a colour", i)
		}
	}
}

func TestAssignedColorsStayDistinct(t *testing.T) {
	// Six is about as many highlights as fit on one screen usefully; they all
	// have to be tellable apart.
	const n = 6
	s := NewSet()
	for i := 0; i < n; i++ {
		s = s.Add(rule(t, Highlight, string(rune('a'+i))))
	}

	hues := make([]float64, 0, n)
	for _, r := range s.All() {
		hues = append(hues, r.Color.Hue())
	}
	for i := range hues {
		gap := minHueGap(hues[i], append(append([]float64{}, hues[:i]...), hues[i+1:]...))
		if gap < minSeparation*0.8 {
			t.Errorf("colour %d is only %.3f around the hue circle from its nearest neighbour", i, gap)
		}
	}
}

func TestExplicitColorIsKept(t *testing.T) {
	want := color.RGB{R: 1, G: 2, B: 3}
	s := NewSet().Add(Rule{Action: Highlight, M: rule(t, Highlight, "x").M, Enabled: true, Color: want})
	if got := s.All()[0].Color; got != want {
		t.Errorf("color.RGB = %v, want %v", got, want)
	}
}

func TestColorRoundTripsThroughHue(t *testing.T) {
	for i := 0; i < 20; i++ {
		h := float64(i) / 20
		c := color.FromHSL(h, colorSat, colorLight)
		if got := c.Hue(); minHueGap(got, []float64{h}) > 0.01 {
			t.Errorf("hue %v round-tripped to %v", h, got)
		}
		// Generated colours are light, so dark text reads on them.
		if c.Luminance() < 0.4 {
			t.Errorf("colour for hue %v is too dark: luminance %.2f", h, c.Luminance())
		}
	}
}

func TestNarrows(t *testing.T) {
	a, b, c := mustCompile(t, "a", Sensitive), mustCompile(t, "b", Sensitive), mustCompile(t, "c", Sensitive)
	rule := func(act Action, m Matcher) Rule { return Rule{Action: act, M: m, Enabled: true} }

	onlyA := NewSet().Add(rule(Only, a))
	onlyAB := onlyA.Add(rule(Only, b))
	cases := []struct {
		name     string
		old, new *Set
		want     bool
	}{
		{"first only rule", NewSet(), onlyA, true},
		{"add a never rule", onlyA, onlyA.Add(rule(Never, c)), true},
		{"remove an only rule", onlyAB, onlyA, true},
		{"disable an only rule", onlyAB, onlyAB.Toggle(1), true},
		{"add an only rule", onlyA, onlyAB, false},
		{"remove the last only rule", onlyA, NewSet(), false},
		{"remove a never rule", onlyA.Add(rule(Never, c)), onlyA, false},
		{"add a highlight", onlyA, onlyA.Add(rule(Highlight, b)), true},
		{"same pattern recompiled", onlyA, NewSet().Add(rule(Only, mustCompile(t, "a", Sensitive))), false},
	}
	for _, c := range cases {
		if got := c.new.Narrows(c.old); got != c.want {
			t.Errorf("%s: Narrows = %v, want %v", c.name, got, c.want)
		}
	}
}
