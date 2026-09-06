package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

// styleAt reports the style of one cell.
func styleAt(scr tcell.SimulationScreen, x, y int) tcell.Style {
	_, _, st, _ := scr.GetContent(x, y)
	return st
}

// searchLit reports whether the search highlight is painted where a match is.
// "line 000 ERROR bad" puts ERROR at column 9.
func searchLit(a *App, scr tcell.SimulationScreen) bool {
	return styleAt(scr, 9, 0) == a.cfg.Style("match") ||
		styleAt(scr, 9, 0) == a.cfg.Style("match.current")
}

func TestSearchHighlightsMatches(t *testing.T) {
	a, scr := newTestApp(t, numbered(40))
	a.doSearch("ERROR", false)
	a.draw()

	if !searchLit(a, scr) {
		t.Fatal("a search did not highlight its matches")
	}
}

// TestCtrlLClearsTheSearch is vim's muscle memory: C-l tidies the screen,
// which includes dropping the search highlight.
func TestCtrlLClearsTheSearch(t *testing.T) {
	a, scr := newTestApp(t, numbered(40))
	a.doSearch("ERROR", false)
	a.draw()
	if !searchLit(a, scr) {
		t.Fatal("the search was not highlighted to begin with")
	}

	ctrl(a, tcell.KeyCtrlL)
	a.draw()

	if a.search.m != nil {
		t.Error("C-l left the search pattern in place")
	}
	if searchLit(a, scr) {
		t.Error("C-l did not clear the search highlight")
	}
}

// TestClearingTheSearchKeepsRuleHighlights is the actual complaint: tidying
// away a transient search must not throw away the highlights you built.
func TestClearingTheSearchKeepsRuleHighlights(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Highlight, "bad", filter.Color{})
	a.addRule(filter.Only, "line", filter.Color{})
	waitFiltered(t, a)
	a.doSearch("ERROR", false)

	ctrl(a, tcell.KeyCtrlL)

	if a.search.m != nil {
		t.Error("the search was not cleared")
	}
	if got := a.rules.Count(filter.Highlight); got != 1 {
		t.Errorf("highlight rules = %d, want 1 left alone", got)
	}
	if a.rules.Len() != 2 {
		t.Errorf("rules = %d, want both kept", a.rules.Len())
	}
	if len(a.eng.Marks) != 2 {
		t.Errorf("marks = %d, want the rules still painted", len(a.eng.Marks))
	}
}

func TestNohlsearchClearsOnlyTheSearch(t *testing.T) {
	for _, alias := range []string{"noh", "nohl", "nohlsearch"} {
		t.Run(alias, func(t *testing.T) {
			a, _ := newTestApp(t, numbered(40))
			a.addRule(filter.Highlight, "bad", filter.Color{})
			a.doSearch("ERROR", false)

			a.runCommandLine(alias)

			if a.search.m != nil {
				t.Errorf(":%s did not clear the search", alias)
			}
			if a.rules.Count(filter.Highlight) != 1 {
				t.Errorf(":%s removed a highlight rule", alias)
			}
		})
	}
}

// TestHighlightClearLeavesTheSearch is the other direction: removing the rules
// you added should not silently drop the search you are stepping through with
// n and N.
func TestHighlightClearLeavesTheSearch(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Highlight, "bad", filter.Color{})
	a.addRule(filter.Never, "nothing", filter.Color{})
	waitFiltered(t, a)
	a.doSearch("ERROR", false)

	run(a, "highlight-clear")

	if a.rules.Count(filter.Highlight) != 0 {
		t.Error("highlight-clear left a highlight rule")
	}
	if a.search.m == nil {
		t.Error("highlight-clear threw away the search as well")
	}
	if a.rules.Len() != 1 {
		t.Errorf("rules = %d, want the never rule kept", a.rules.Len())
	}
}

func TestSearchNextStillWorksAfterHighlightClear(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	a.addRule(filter.Highlight, "bad", filter.Color{})
	a.doSearch("ERROR", false)
	waitBusy(t, a)

	run(a, "highlight-clear")
	a.clearMsg()

	a.exec(cmdByName("search-next"), 0)
	waitBusy(t, a)
	if a.msg == "no search pattern" {
		t.Error("n stopped working after highlight-clear")
	}
}

func cmdByName(n string) config.Command { return config.Command{Name: n} }
