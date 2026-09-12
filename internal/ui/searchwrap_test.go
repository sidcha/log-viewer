package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// waitSearch runs the event loop's part until the background search settles.
func waitSearch(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.busy != nil {
		select {
		case fn := <-a.post:
			fn()
		case <-time.After(time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the search never finished")
		}
	}
}

// TestSearchWindowFindsMatchesAboveTheCursor is the reported bug: the window
// said there was nothing while the committed search plainly found matches.
func TestSearchWindowFindsMatchesAboveTheCursor(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	a.gotoSourceLine(150)
	a.draw()

	f := openFinderWith(t, a, '/', "line 003")
	if len(f.rows) == 0 {
		t.Fatal("the window found nothing for a pattern that is in the file")
	}
	if got := rowNums(f); got[0] != 4 {
		t.Errorf("first match = line %d, want 4", got[0])
	}
	if !f.wrapped {
		t.Error("the window did not record that it came round again")
	}

	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitSearch(t, a)
	if got := a.anchorSource() + 1; got != 4 {
		t.Errorf("landed on line %d, want the match at 4", got)
	}
}

// TestSearchPrefersWhatIsAhead: wrapping is a fallback, not the starting point.
func TestSearchPrefersWhatIsAhead(t *testing.T) {
	a, _ := newTestApp(t, numbered(400))
	a.gotoSourceLine(200)
	a.draw()

	f := openFinderWith(t, a, '/', "ERROR")
	if nums := rowNums(f); len(nums) == 0 || nums[0] < 200 {
		t.Errorf("first match = %v, want one at or after the cursor", nums)
	}
	if f.wrapped {
		t.Error("wrapped while there were matches ahead")
	}
}

// TestBackwardSearchStepsBackward. ? opened a window that looked forward and
// then committed a forward search, so the backward key never searched back.
func TestBackwardSearchStepsBackward(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	a.gotoSourceLine(100)
	a.draw()

	a.key(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone))
	if a.find == nil || !a.find.back {
		t.Fatal("? did not open a backward search")
	}
	typeInto(a, "ERROR")
	settleFinder(t, a)
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitSearch(t, a)

	if !a.search.back {
		t.Fatal("the committed search steps forward")
	}
	at := a.anchorSource()
	run(a, "search-next")
	waitSearch(t, a)
	if got := a.anchorSource(); got >= at {
		t.Errorf("n moved from %d to %d; a backward search should walk up", at, got)
	}
}

// TestSearchWrapsAtTheEnd so n can reach what the window listed.
//
// The cursor is parked at the last line, not at the match it would wrap to:
// landing on line 1 has to be something the wrap did, not somewhere we were
// already sitting.
func TestSearchWrapsAtTheEnd(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	a.doSearch("ERROR", false)
	waitSearch(t, a)

	last := a.base.Len() - 1
	a.setCursor(last)
	a.search.cur = last // walked down past the last match
	a.draw()
	if got := a.anchorSource() + 1; got != last+1 {
		t.Fatalf("setup put the cursor on line %d, want %d", got, last+1)
	}

	run(a, "search-next")
	waitSearch(t, a)

	if got := a.anchorSource() + 1; got != 1 {
		t.Errorf("n from the end landed on line %d, want the first match at 1", got)
	}
	if !strings.Contains(a.msg, "continuing at the start") {
		t.Errorf("wrapping reported %q; it has to be visible that the position jumped", a.msg)
	}
}

func TestSearchWrapsAtTheStart(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	a.doSearch("ERROR", false)
	waitSearch(t, a)

	a.setCursor(0)
	a.search.cur = 0
	a.draw()

	run(a, "search-prev")
	waitSearch(t, a)

	// The last ERROR line of numbered(60).
	if got := a.anchorSource() + 1; got != 58 {
		t.Errorf("N from the top landed on line %d, want the last match at 58", got)
	}
	if !strings.Contains(a.msg, "continuing at the end") {
		t.Errorf("wrapping reported %q", a.msg)
	}
}

// TestAMissingPatternStillSaysSo rather than circling for ever.
func TestAMissingPatternStillSaysSo(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	a.doSearch("no such text anywhere", false)
	waitSearch(t, a)

	run(a, "search-next")
	waitSearch(t, a)
	if a.msg == "" {
		t.Error("a pattern that is not in the file reported nothing")
	}
}

// TestCommittingAnEmptyWindowStillSearches: the window may be committed before
// its scan has reached a match, and a highlight with no movement is the same
// complaint in a smaller form.
func TestCommittingAnEmptyWindowStillSearches(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	a.draw()

	a.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	typeInto(a, "line 150")
	// No settle: commit while the scan is still in flight.
	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	waitSearch(t, a)

	if a.search.m == nil {
		t.Fatal("no search installed")
	}
	if got := a.anchorSource() + 1; got != 151 {
		t.Errorf("landed on line %d, want 151", got)
	}
}
