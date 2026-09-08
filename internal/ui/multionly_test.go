package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/view"
)

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

// TestMultipleOnlyRulesAreOred: a second only pattern widens the view, it does
// not replace the first.
func TestMultipleOnlyRulesAreOred(t *testing.T) {
	a, _ := newTestApp(t, mixed)

	a.addRule(filter.Only, "alpha", filter.Color{})
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 2 {
		t.Fatalf("one only rule shows %d lines, want 2", got)
	}

	a.addRule(filter.Only, "beta", filter.Color{})
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
	a.addRule(filter.Only, "delta", filter.Color{})
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 5 {
		t.Fatalf("three only rules show %d lines, want 5", got)
	}
}

// TestPreviewOfASecondOnlyDoesNotLookEmpty is the reported problem: the
// preview evaluated the new pattern alone against the already-filtered view,
// so it reported nothing matching and dimmed the screen, which reads as "this
// pattern replaces the first and matches nothing".
func TestPreviewOfASecondOnlyDoesNotLookEmpty(t *testing.T) {
	a, scr := newTestApp(t, mixed)

	a.addRule(filter.Only, "alpha", filter.Color{})
	waitFiltered(t, a)

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "beta")
	a.draw()

	if !a.prev.widening {
		t.Fatal("the preview did not notice that this only rule widens")
	}
	if a.prev.matched == 0 {
		t.Error("the preview counted no lines to add, but beta lines exist")
	}
	if s := a.prev.summary(); !strings.Contains(s, "+") {
		t.Errorf("summary = %q, want it to show an addition", s)
	}

	// Nothing on screen may be dimmed: or-ing never removes a visible line.
	for row := 0; row < 2; row++ {
		if isDropped(a, scr, row) {
			t.Fatalf("row %d is dimmed while previewing a widening rule", row)
		}
	}
}

// TestPreviewOfAFirstOnlyStillNarrows keeps the other case honest.
func TestPreviewOfAFirstOnlyStillNarrows(t *testing.T) {
	a, scr := newTestApp(t, mixed)

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "alpha")
	a.draw()

	if a.prev.widening {
		t.Fatal("the first only rule should narrow, not widen")
	}
	if !isDropped(a, scr, 1) { // "beta two"
		t.Error("a line the first only rule would remove is not dimmed")
	}
	if s := a.prev.summary(); !strings.Contains(s, "shown") {
		t.Errorf("summary = %q", s)
	}
}

// TestPreviewOfANeverStillNarrowsAlongsideOnlys: never is an and, so it keeps
// removing lines however many only rules there are.
func TestPreviewOfANeverStillNarrowsAlongsideOnlys(t *testing.T) {
	a, scr := newTestApp(t, mixed)

	a.addRule(filter.Only, "alpha", filter.Color{})
	a.addRule(filter.Only, "beta", filter.Color{})
	waitFiltered(t, a)

	a.key(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone))
	typeInto(a, "two")
	a.draw()

	if a.prev.widening {
		t.Fatal("a never rule must not be treated as widening")
	}
	// "beta two" is row 1 of the filtered view.
	if !isDropped(a, scr, 1) {
		t.Error("the line a never rule would remove is not dimmed")
	}
}

// TestSecondOnlyThroughThePrompt walks the whole flow the way a user does.
func TestSecondOnlyThroughThePrompt(t *testing.T) {
	a, _ := newTestApp(t, mixed)

	for _, pat := range []string{"alpha", "beta"} {
		a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
		typeInto(a, pat)
		press(a, tcell.KeyEnter)
		waitFiltered(t, a)
	}

	if a.rules.Count(filter.Only) != 2 {
		t.Fatalf("only rules = %d, want 2", a.rules.Count(filter.Only))
	}
	if got := len(visible(t, a)); got != 4 {
		t.Fatalf("view shows %d lines, want 4", got)
	}
}

// TestDisablingOneOnlyNarrowsToTheRest checks the or holds as rules are turned
// on and off from the list.
func TestDisablingOneOnlyNarrowsToTheRest(t *testing.T) {
	a, _ := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", filter.Color{})
	a.addRule(filter.Only, "beta", filter.Color{})
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
// have to discover by trying it.
func TestRuleListExplainsHowRulesCombine(t *testing.T) {
	a, scr := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", filter.Color{})
	a.addRule(filter.Only, "beta", filter.Color{})
	a.addRule(filter.Never, "four", filter.Color{})
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
