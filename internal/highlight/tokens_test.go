package highlight

import (
	"regexp"
	"strings"
	"testing"
)

func ruleOf(re string) Item { return Item{Rule: Rule{Re: regexp.MustCompile(re)}} }

func ruleSources(rs []Rule) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Re.String()
	}
	return out
}

func TestExpandInsertsATokenSetWhereItIsUsed(t *testing.T) {
	sets := map[string]*TokenSet{"nums": {Name: "nums", Items: []Item{ruleOf(`[0-9]+`)}}}
	p := &Profile{Name: "p", Items: []Item{ruleOf(`a`), {Use: "nums"}, ruleOf(`b`)}}
	if err := Expand(p, sets); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ruleSources(p.Rules), " "); got != "a [0-9]+ b" {
		t.Errorf("rules = %s, want a [0-9]+ b", got)
	}
}

func TestExpandFollowsTokenSetsThatUseOthers(t *testing.T) {
	sets := map[string]*TokenSet{
		"inner": {Name: "inner", Items: []Item{ruleOf(`x`)}},
		"outer": {Name: "outer", Items: []Item{{Use: "inner"}, ruleOf(`y`)}},
	}
	p := &Profile{Name: "p", Items: []Item{{Use: "outer"}}}
	if err := Expand(p, sets); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ruleSources(p.Rules), " "); got != "x y" {
		t.Errorf("rules = %s, want x y", got)
	}
}

func TestExpandIsRepeatable(t *testing.T) {
	sets := map[string]*TokenSet{"n": {Name: "n", Items: []Item{ruleOf(`1`)}}}
	p := &Profile{Name: "p", Items: []Item{{Use: "n"}}}
	for i := 0; i < 2; i++ {
		if err := Expand(p, sets); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Rules) != 1 {
		t.Errorf("expanding twice left %d rules, want 1", len(p.Rules))
	}
}

func TestExpandRejectsAnUndefinedSet(t *testing.T) {
	p := &Profile{Name: "p", Items: []Item{{Use: "missing", Where: "f:3"}}}
	err := Expand(p, nil)
	if err == nil || !strings.Contains(err.Error(), `f:3`) || !strings.Contains(err.Error(), `"missing"`) {
		t.Errorf("err = %v, want the position and the undefined name", err)
	}
}

func TestExpandRejectsASetThatUsesItself(t *testing.T) {
	sets := map[string]*TokenSet{
		"a": {Name: "a", Items: []Item{{Use: "b"}}},
		"b": {Name: "b", Items: []Item{{Use: "a"}}},
	}
	p := &Profile{Name: "p", Items: []Item{{Use: "a"}}}
	if err := Expand(p, sets); err == nil || !strings.Contains(err.Error(), "uses itself") {
		t.Errorf("err = %v, want a cycle error", err)
	}
}
