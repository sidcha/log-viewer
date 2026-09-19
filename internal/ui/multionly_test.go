package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/view"
)

// only rules are or-ed: each one widens what is shown rather than replacing
// the last. It is the part of the model least likely to be guessed correctly
// from the outside, and the part a rewrite of the filter path would most
// easily get wrong, so it is pinned here at the level a user works at.

func visible(t *testing.T, a *App) []string {
	t.Helper()
	var out []string
	if err := a.activeView().Iterate(0, func(_ int64, ln view.Line) bool {
		out = append(out, string(ln.Text))
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

const mixed = "alpha one\nbeta two\ngamma three\nalpha four\nbeta five\ndelta six\n"

func TestMultipleOnlyRulesAreOred(t *testing.T) {
	a, _ := newTestApp(t, mixed)

	a.addRule(filter.Only, "alpha", color.RGB{})
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 2 {
		t.Fatalf("one only rule shows %d lines, want 2", got)
	}

	a.addRule(filter.Only, "beta", color.RGB{})
	waitFiltered(t, a)

	got := visible(t, a)
	if len(got) != 4 {
		t.Fatalf("two only rules show %d lines, want 4:\n%q", len(got), got)
	}
	for _, l := range got {
		if !strings.Contains(l, "alpha") && !strings.Contains(l, "beta") {
			t.Errorf("unexpected line %q", l)
		}
	}

	// A third widens again.
	a.addRule(filter.Only, "delta", color.RGB{})
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 5 {
		t.Fatalf("three only rules show %d lines, want 5", got)
	}
}

// TestSecondOnlyThroughTheWindow walks the whole flow the way a user does:
// the second pattern is typed into a window opened over an already filtered
// view, and it must find the lines that filter is hiding.
func TestSecondOnlyThroughTheWindow(t *testing.T) {
	a, _ := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", color.RGB{})
	waitFiltered(t, a)

	f := openFinderWith(t, a, '&', "beta")
	if len(f.rows) != 2 {
		t.Fatalf("the window found %d beta lines, want 2: they are hidden, which is the point", len(f.rows))
	}

	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitFiltered(t, a)
	if got := visible(t, a); len(got) != 4 {
		t.Fatalf("after committing, %d lines are shown, want 4:\n%q", len(got), got)
	}
}

// TestNeverNarrowsAlongsideOnlys: never is an and, so it keeps cutting whatever
// the only rules let through.
func TestNeverNarrowsAlongsideOnlys(t *testing.T) {
	a, _ := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", color.RGB{})
	a.addRule(filter.Only, "beta", color.RGB{})
	waitFiltered(t, a)

	a.addRule(filter.Never, "four", color.RGB{})
	waitFiltered(t, a)

	got := visible(t, a)
	if len(got) != 3 {
		t.Fatalf("shows %d lines, want 3:\n%q", len(got), got)
	}
	for _, l := range got {
		if strings.Contains(l, "four") {
			t.Errorf("a never rule let %q through", l)
		}
	}
}

func TestDisablingOneOnlyNarrowsToTheRest(t *testing.T) {
	a, _ := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", color.RGB{})
	a.addRule(filter.Only, "beta", color.RGB{})
	waitFiltered(t, a)

	a.applyRules(a.rules.Toggle(1), false) // drop beta
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 2 {
		t.Fatalf("with beta disabled the view shows %d lines, want 2", got)
	}

	a.applyRules(a.rules.Toggle(1), false) // back on
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 4 {
		t.Fatalf("with beta back the view shows %d lines, want 4", got)
	}
}

// TestRuleListExplainsHowRulesCombine: the or is not something anyone should
// have to work out by experiment.
func TestRuleListExplainsHowRulesCombine(t *testing.T) {
	a, scr := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", color.RGB{})
	a.addRule(filter.Only, "beta", color.RGB{})
	a.addRule(filter.Never, "four", color.RGB{})
	waitFiltered(t, a)

	run(a, "filter-list")
	var all strings.Builder
	for row := 0; row < testH; row++ {
		all.WriteString(rowText(scr, row))
		all.WriteByte('\n')
	}
	text := all.String()
	if !strings.Contains(text, "any of the 2 only rules") {
		t.Errorf("the rule list does not explain the or:\n%s", text)
	}
	if !strings.Contains(text, "never") {
		t.Errorf("the rule list does not mention the never rule:\n%s", text)
	}
}
