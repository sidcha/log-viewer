package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

// typeInto sends a string to an open prompt, one key at a time, so the preview
// is recomputed exactly as it would be for a real typist.
func typeInto(a *App, s string) {
	for _, r := range s {
		a.key(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

func press(a *App, k tcell.Key) {
	a.key(tcell.NewEventKey(k, 0, tcell.ModNone))
}

func rowStyles(scr tcell.SimulationScreen, row int) []tcell.Style {
	out := make([]tcell.Style, 0, testW)
	for x := 0; x < testW; x++ {
		_, _, st, _ := scr.GetContent(x, row)
		out = append(out, st)
	}
	return out
}

// fg compares by foreground only. The cursor line tints the background, so a
// whole-style comparison would depend on where the cursor happens to be.
func fg(st tcell.Style) tcell.Color {
	c, _, _ := st.Decompose()
	return c
}

// isDropped reports whether a row is drawn as a line the candidate rule would
// remove.
func isDropped(a *App, scr tcell.SimulationScreen, row int) bool {
	return fg(rowStyles(scr, row)[0]) == fg(a.cfg.Style("preview.dropped"))
}

func TestPreviewDimsLinesAnOnlyRuleWouldRemove(t *testing.T) {
	a, scr := newTestApp(t, numbered(40))

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone)) // show only
	typeInto(a, "ERROR")
	a.draw()

	// Row 0 is an ERROR line and survives; row 1 is INFO and would go.
	if isDropped(a, scr, 0) {
		t.Error("a line the rule would keep is drawn as dropped")
	}
	if !isDropped(a, scr, 1) {
		t.Error("a line the rule would remove is not dimmed")
	}

	// Nothing has actually been applied yet.
	if a.rules.Len() != 0 || a.filt != nil {
		t.Error("the preview applied the rule before it was committed")
	}
}

func TestPreviewDimsLinesANeverRuleWouldRemove(t *testing.T) {
	a, scr := newTestApp(t, numbered(40))

	a.key(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone)) // never show
	typeInto(a, "ERROR")
	a.draw()

	if !isDropped(a, scr, 0) {
		t.Error("a matching line is not dimmed under a never rule")
	}
	if isDropped(a, scr, 1) {
		t.Error("a non-matching line should be unaffected by a never rule")
	}
}

func TestPreviewMarksMatchesWithoutDimmingAnything(t *testing.T) {
	a, scr := newTestApp(t, numbered(40))

	a.key(tcell.NewEventKey(tcell.KeyRune, 'm', tcell.ModNone)) // highlight
	typeInto(a, "ERROR")
	a.draw()

	for row := 0; row < testH; row++ {
		if isDropped(a, scr, row) {
			t.Fatalf("row %d dimmed; a highlight removes nothing", row)
		}
	}

	// "ERROR" starts at column 9 of "line 000 ERROR bad". Compare on a row
	// away from the cursor so the cursor-line tint does not interfere.
	styles := rowStyles(scr, 3)
	if styles[9] != a.prev.style {
		t.Error("the matched text is not painted in the previewed colour")
	}
	if styles[0] == a.prev.style {
		t.Error("text outside the match should not be painted")
	}
}

func TestPreviewCountsMatches(t *testing.T) {
	a, _ := newTestApp(t, numbered(90))

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "ERROR")

	// Every third line is an ERROR.
	if a.prev.sampled != 90 {
		t.Fatalf("sampled = %d, want the whole 90-line file", a.prev.sampled)
	}
	if a.prev.matched != 30 {
		t.Fatalf("matched = %d, want 30", a.prev.matched)
	}
	if s := a.prev.summary(); !strings.Contains(s, "30 of 90") {
		t.Errorf("summary = %q", s)
	}
}

func TestPreviewSampleIsBounded(t *testing.T) {
	a, _ := newTestApp(t, numbered(5000))
	if err := a.cfg.Set("preview-sample", "100"); err != nil {
		t.Fatal(err)
	}

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "line")

	// A keystroke must not cost a pass over the whole file.
	if a.prev.sampled != 100 {
		t.Fatalf("sampled = %d, want the configured 100", a.prev.sampled)
	}
}

func TestPreviewReportsABadPattern(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "ERR(")

	if a.prev.err == nil {
		t.Fatal("an unbalanced group should be reported while typing")
	}
	if s := a.prev.summary(); !strings.Contains(s, "✗") {
		t.Errorf("summary = %q, want an error marker", s)
	}
	// A broken pattern must not dim the whole screen.
	a.draw()
	if a.prev.m != nil {
		t.Error("a pattern that does not compile should not be previewed")
	}
}

func TestPreviewFollowsEdits(t *testing.T) {
	a, _ := newTestApp(t, numbered(30))

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "ERROR")
	if a.prev.matched != 10 {
		t.Fatalf("matched = %d, want 10", a.prev.matched)
	}

	// Backspacing to a wider pattern must widen the preview.
	for i := 0; i < 5; i++ {
		press(a, tcell.KeyBackspace2)
	}
	typeInto(a, "line")
	if a.prev.matched != 30 {
		t.Fatalf("after editing, matched = %d, want all 30", a.prev.matched)
	}

	// Clearing it entirely leaves nothing previewed.
	press(a, tcell.KeyCtrlU)
	if a.prev.m != nil || a.prev.matched != 0 {
		t.Error("an empty pattern should preview nothing")
	}
}

func TestPreviewEndsWithThePrompt(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	a.key(tcell.NewEventKey(tcell.KeyRune, '&', tcell.ModNone))
	typeInto(a, "ERROR")
	if a.prev == nil {
		t.Fatal("no preview while typing")
	}

	press(a, tcell.KeyEscape)
	if a.prev != nil {
		t.Error("the preview outlived the prompt")
	}
	if a.rules.Len() != 0 {
		t.Error("cancelling applied the rule anyway")
	}
}

// TestCommittedRuleKeepsThePreviewedColour is what makes the preview
// trustworthy: the colour you were looking at is the colour you get.
func TestCommittedRuleKeepsThePreviewedColour(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	a.key(tcell.NewEventKey(tcell.KeyRune, 'm', tcell.ModNone))
	typeInto(a, "ERROR")
	want := a.prev.color

	press(a, tcell.KeyEnter)
	if a.rules.Len() != 1 {
		t.Fatalf("rule was not added")
	}
	if got := a.rules.All()[0].Color; got != want {
		t.Errorf("rule colour = %+v, want the previewed %+v", got, want)
	}
}

// TestSearchPreviewsAndDefaultsToHighlight is the / behaviour: matches are
// marked as you type, nothing is removed, and Enter searches rather than
// filtering.
func TestSearchPreviewsAndDefaultsToHighlight(t *testing.T) {
	a, scr := newTestApp(t, numbered(40))

	a.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	if a.prev == nil {
		t.Fatal("/ did not start a preview")
	}
	if a.prompt.act != actSearch {
		t.Fatalf("/ opened on %v, want search", a.prompt.act)
	}

	typeInto(a, "ERROR")
	a.draw()

	for row := 0; row < testH; row++ {
		if isDropped(a, scr, row) {
			t.Fatalf("row %d dimmed; searching removes nothing", row)
		}
	}
	// Matches are marked in the previewed colour.
	if rowStyles(scr, 3)[9] != a.prev.style {
		t.Error("search preview did not mark the match")
	}
	if a.prev.matched == 0 {
		t.Error("search preview counted no matches")
	}
}

// TestSearchPromptCyclesAction covers Tab promoting a search into a rule
// without retyping the pattern.
func TestSearchPromptCyclesAction(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	a.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	typeInto(a, "ERROR")

	press(a, tcell.KeyTab)
	if a.prompt.act != actOnly {
		t.Fatalf("Tab from search went to %v, want only", a.prompt.act)
	}
	if a.prev.act != actOnly {
		t.Error("the preview did not follow the action change")
	}
	if string(a.prompt.buf) != "ERROR" {
		t.Errorf("Tab lost the typed pattern: %q", a.prompt.buf)
	}

	press(a, tcell.KeyTab)
	if a.prompt.act != actNever {
		t.Fatalf("second Tab went to %v, want never", a.prompt.act)
	}
	press(a, tcell.KeyTab)
	if a.prompt.act != actHighlight {
		t.Fatalf("third Tab went to %v, want highlight", a.prompt.act)
	}
	press(a, tcell.KeyTab)
	if a.prompt.act != actSearch {
		t.Fatalf("Tab did not wrap back to search, got %v", a.prompt.act)
	}

	// Back-tab goes the other way.
	press(a, tcell.KeyBacktab)
	if a.prompt.act != actHighlight {
		t.Fatalf("Backtab went to %v, want highlight", a.prompt.act)
	}

	// Committing on a rule action adds the rule rather than searching.
	press(a, tcell.KeyEnter)
	if a.rules.Len() != 1 || a.rules.All()[0].Action != filter.Highlight {
		t.Fatalf("rules = %d after Tab+Enter", a.rules.Len())
	}
}

func TestPreviewRunsOnTopOfExistingFilters(t *testing.T) {
	a, _ := newTestApp(t, numbered(90))

	a.exec(config.Command{Name: "filter-only", Args: []string{"ERROR"}}, 0)
	waitFiltered(t, a)
	if a.viewLen() != 30 {
		t.Fatalf("filtered view has %d lines, want 30", a.viewLen())
	}

	// The candidate is evaluated against what is currently shown, not the
	// whole file.
	a.key(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone))
	typeInto(a, "line 00")
	if a.prev.sampled != 30 {
		t.Fatalf("sampled = %d, want the 30 lines the filter leaves", a.prev.sampled)
	}
}

func TestHighlightDoesNotRestartAFilterScan(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	a.exec(config.Command{Name: "filter-only", Args: []string{"ERROR"}}, 0)
	waitFiltered(t, a)
	before := a.filt

	a.addRule(filter.Highlight, "bad", filter.Color{})
	if a.filt != before {
		t.Error("adding a highlight threw away the filtered view")
	}
	if a.rules.Len() != 2 {
		t.Errorf("rules = %d, want 2", a.rules.Len())
	}
	// And the highlight must actually be drawn.
	if len(a.eng.Marks) != 2 {
		t.Errorf("marks = %d, want the only rule and the highlight", len(a.eng.Marks))
	}
}

func TestNeverRulesAreNotMarked(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	a.addRule(filter.Never, "INFO", filter.Color{})
	// A hidden line cannot be marked, so it contributes no mark.
	if len(a.eng.Marks) != 0 {
		t.Errorf("marks = %d, want 0 for a never rule", len(a.eng.Marks))
	}

	a.addRule(filter.Only, "line", filter.Color{})
	if len(a.eng.Marks) != 1 {
		t.Errorf("marks = %d, want 1 for the only rule", len(a.eng.Marks))
	}
}

func TestRulesGetDistinctColours(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	for _, p := range []string{"aa", "bb", "cc", "dd"} {
		a.addRule(filter.Highlight, p, filter.Color{})
	}
	seen := map[filter.Color]bool{}
	for _, r := range a.rules.All() {
		if r.Color == (filter.Color{}) {
			t.Fatal("a rule was left without a colour")
		}
		if seen[r.Color] {
			t.Fatalf("colour %+v was assigned twice", r.Color)
		}
		seen[r.Color] = true
	}
}

func TestHighlightClearLeavesFiltersAlone(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))

	a.exec(config.Command{Name: "filter-only", Args: []string{"ERROR"}}, 0)
	waitFiltered(t, a)
	a.addRule(filter.Highlight, "bad", filter.Color{})

	run(a, "highlight-clear")
	if a.rules.Len() != 1 || a.rules.Count(filter.Highlight) != 0 {
		t.Fatalf("rules = %d, highlights = %d", a.rules.Len(), a.rules.Count(filter.Highlight))
	}
	if a.filt == nil {
		t.Error("clearing highlights dropped the filter")
	}
}

func TestRulesOverlayListsEveryAction(t *testing.T) {
	a, scr := newTestApp(t, numbered(60))

	a.addRule(filter.Only, "line", filter.Color{})
	a.addRule(filter.Never, "INFO", filter.Color{})
	a.addRule(filter.Highlight, "bad", filter.Color{})
	run(a, "filter-list")

	var all strings.Builder
	for row := 0; row < testH; row++ {
		all.WriteString(rowText(scr, row))
		all.WriteByte('\n')
	}
	for _, want := range []string{"only", "never", "highlight"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("rules overlay does not mention %q:\n%s", want, all.String())
		}
	}
}
