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

// TestTypingASecondOnlyLiftsTheFirst is the point of the whole arrangement:
// the lines a further only pattern would bring back are exactly the ones the
// existing rules are hiding, so they have to be on screen to be previewed
// against.
func TestTypingASecondOnlyLiftsTheFirst(t *testing.T) {
	a, _ := newTestApp(t, mixed)

	a.addRule(filter.Only, "alpha", filter.Color{})
	waitFiltered(t, a)
	if got := a.viewLen(); got != 2 {
		t.Fatalf("filtered view has %d lines, want 2", got)
	}

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	if !a.previewBase {
		t.Fatal("the only rules were not lifted while typing another")
	}
	if got := a.viewLen(); got != 6 {
		t.Fatalf("while typing, the view shows %d lines, want the whole log (6)", got)
	}

	// And the candidate can now be seen matching a line the filter was hiding.
	typeInto(a, "beta")
	if a.prev.m == nil || !a.prev.m.Match([]byte("beta two")) {
		t.Error("the candidate does not match a line that was hidden")
	}
}

// TestPreviewDimsByTheProspectiveSet: what is dimmed has to be what would
// actually go, which means judging each line by the rules as they would be,
// not by the pattern being typed on its own.
func TestPreviewDimsByTheProspectiveSet(t *testing.T) {
	a, scr := newTestApp(t, mixed)

	a.addRule(filter.Only, "alpha", filter.Color{})
	waitFiltered(t, a)

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "beta")
	a.draw()

	// mixed is: alpha one / beta two / gamma three / alpha four / beta five /
	// delta six. alpha and beta lines survive; gamma and delta do not.
	for row, want := range map[int]bool{0: false, 1: false, 2: true, 3: false, 4: false, 5: true} {
		if got := isDropped(a, scr, row); got != want {
			t.Errorf("row %d (%q) dimmed=%v, want %v", row, rowText(scr, row), got, want)
		}
	}

	if s := a.prev.summary(); !strings.Contains(s, "4 of 6 shown") {
		t.Errorf("summary = %q, want it to count the resulting view", s)
	}
}

// TestCancellingRestoresTheFilter: lifting the rules is for the duration of
// the prompt only.
func TestCancellingRestoresTheFilter(t *testing.T) {
	a, _ := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", filter.Color{})
	waitFiltered(t, a)

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "beta")
	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if a.previewBase {
		t.Fatal("the only rules were left lifted after cancelling")
	}
	waitFiltered(t, a)
	if got := a.viewLen(); got != 2 {
		t.Fatalf("after cancelling the view shows %d lines, want the filtered 2", got)
	}
	if a.rules.Len() != 1 {
		t.Errorf("cancelling added a rule: %d rules", a.rules.Len())
	}
}

// TestCommittingReenablesEveryOnly: all of them come back, or-ed with the new.
func TestCommittingReenablesEveryOnly(t *testing.T) {
	a, _ := newTestApp(t, mixed)
	a.addRule(filter.Only, "alpha", filter.Color{})
	waitFiltered(t, a)

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "beta")
	press(a, tcell.KeyEnter)

	if a.previewBase {
		t.Fatal("the only rules were left lifted after committing")
	}
	waitFiltered(t, a)
	if got := len(visible(t, a)); got != 4 {
		t.Fatalf("after committing the view shows %d lines, want 4", got)
	}
}

// TestPreviewOfAFirstOnlyStillNarrows keeps the other case honest.
func TestPreviewOfAFirstOnlyStillNarrows(t *testing.T) {
	a, scr := newTestApp(t, mixed)

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "alpha")
	a.draw()

	if a.previewBase {
		t.Fatal("there is no only rule to lift, so the view should be unchanged")
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

	if a.previewBase {
		t.Fatal("a never rule narrows, so the only rules must stay in force")
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
