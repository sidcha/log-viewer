package highlight

import (
	"regexp"
	"testing"
)

func detecting(name string, globs []string, res ...string) *Profile {
	p := &Profile{Name: name, Globs: globs}
	for _, re := range res {
		p.Detects = append(p.Detects, regexp.MustCompile(re))
	}
	return p
}

func lines(ss ...string) [][]byte {
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out
}

func TestScoreLooksPastTheFirstLine(t *testing.T) {
	k := detecting("kernel", nil, `^\[[ ]*[0-9]+\.[0-9]+\]`)
	sample := lines("=== capture started ===", "[    0.000000] Booting", "[    0.100000] x", "[    0.200000] y")
	p, pct := Score([]*Profile{k}, sample, "", 30)
	if p != k || pct != 75 {
		t.Errorf("got %v at %d%%, want kernel at 75%%", p, pct)
	}
}

func TestScoreDoesNotLetAGlobDecideAlone(t *testing.T) {
	iso := detecting("iso", []string{"*.log"}, `^[0-9]{4}-`)
	p, _ := Score([]*Profile{iso}, lines("[ 1.0] kernel line"), "app.log", 30)
	if p != nil {
		t.Errorf("a glob alone chose %s", p.Name)
	}
}

func TestScoreGlobBonusCountsTowardsTheScore(t *testing.T) {
	a := detecting("a", nil, `^A`)             // 25%
	b := detecting("b", []string{"*.b"}, `^B`) // 50% + 25
	p, pct := Score([]*Profile{a, b}, lines("A1", "B1", "B2", "x"), "f.b", 30)
	if p != b || pct != 75 {
		t.Errorf("got %v at %d%%, want b at 75%%", p, pct)
	}
}

func TestScorePrefersMoreHitsOnATie(t *testing.T) {
	a := detecting("a", []string{"*.x"}, `^A`) // 25% + 25 glob = 50
	b := detecting("b", nil, `^B`)             // 50%
	p, _ := Score([]*Profile{a, b}, lines("A", "B", "B", "z"), "f.x", 30)
	if p != b {
		t.Errorf("got %v, want b, which matched more lines", p)
	}
}

// A user's profile that has only globs keeps working.
func TestScoreChoosesAGlobOnlyProfileByItsGlob(t *testing.T) {
	mine := &Profile{Name: "mine", Globs: []string{"*.mine"}}
	p, _ := Score([]*Profile{mine}, lines("anything"), "x.mine", 30)
	if p != mine {
		t.Errorf("got %v, want the glob-only profile", p)
	}
}

func TestScoreReturnsNilBelowTheThreshold(t *testing.T) {
	a := detecting("a", nil, `^A`)
	if p, _ := Score([]*Profile{a}, lines("A", "x", "x", "x", "x"), "", 30); p != nil {
		t.Errorf("20%% chose %s at a 30%% threshold", p.Name)
	}
}

func TestVisiblePrefixDropsEscapes(t *testing.T) {
	got := visiblePrefix(nil, []byte("\x1b[32m[    1.0]\x1b[0m hello"), 512)
	if string(got) != "[    1.0] hello" {
		t.Errorf("got %q", got)
	}
	if got := visiblePrefix(nil, []byte("abcdef"), 3); string(got) != "abc" {
		t.Errorf("bounded prefix = %q, want abc", got)
	}
}

// Review focus 3: a CR line ending must not defeat a $-anchored pattern.
func TestSelectTrimsCarriageReturns(t *testing.T) {
	p := detecting("p", nil, `^ok$`)
	gen := &Profile{Name: "generic"}
	if got := Select([]*Profile{p, gen}, "", "f", []byte("ok\r\nok\r\n")); got != p {
		t.Errorf("got %v, want p", got)
	}
}
