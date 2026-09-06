package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func wheel(a *App, up bool) {
	b := tcell.WheelDown
	if up {
		b = tcell.WheelUp
	}
	a.handle(tcell.NewEventMouse(0, 0, b, tcell.ModNone))
}

// TestWheelScrollSticks is the reported regression: scrolling with the wheel
// moved the window but left the cursor behind, so the next key snapped the
// view straight back and the viewport appeared stuck.
func TestWheelScrollSticks(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "G")

	for i := 0; i < 5; i++ {
		wheel(a, true)
	}
	scrolled := a.top
	if scrolled == 0 {
		t.Fatal("the wheel did not move the window at all")
	}

	keys(a, "j")
	if a.top > scrolled+int64(a.contentRows()) {
		t.Fatalf("the view jumped back to %d after one keypress (was %d)", a.top, scrolled)
	}
	if a.cursorLine() < a.top || a.cursorLine() >= a.top+int64(a.contentRows()) {
		t.Fatalf("the cursor at %d is outside the window at %d", a.cursorLine(), a.top)
	}
}

func TestWheelKeepsCursorOnScreen(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "100G")

	for i := 0; i < 10; i++ {
		wheel(a, true)
	}
	if a.cursorLine() < a.top || a.cursorLine() >= a.top+int64(a.contentRows()) {
		t.Errorf("after wheeling up, cursor %d is outside the window at %d", a.cursorLine(), a.top)
	}

	for i := 0; i < 20; i++ {
		wheel(a, false)
	}
	if a.cursorLine() < a.top || a.cursorLine() >= a.top+int64(a.contentRows()) {
		t.Errorf("after wheeling down, cursor %d is outside the window at %d", a.cursorLine(), a.top)
	}
}

func TestWheelLeavesFollow(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	run(a, "follow-toggle")

	wheel(a, true)
	if a.follow {
		t.Error("wheeling up did not leave follow mode")
	}
	top := a.top
	a.draw()
	if a.top != top {
		t.Errorf("the next frame pinned the view back to %d (was %d)", a.top, top)
	}
}

// TestSearchMovesTheCursor: a search that only moved the window left the
// cursor elsewhere, so the first motion after it jumped away from the match.
func TestSearchMovesTheCursor(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	a.doSearch("line 150", false)
	waitBusy(t, a)

	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(ln.Text) != numberedLine(150) {
		t.Fatalf("the cursor is on %q, want the match", ln.Text)
	}

	before := a.top
	keys(a, "j")
	if a.top > before+int64(a.contentRows()) {
		t.Errorf("the view jumped after the search: %d -> %d", before, a.top)
	}
}

// waitBusy drains the background operation a search starts.
func waitBusy(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case f := <-a.post:
			f()
			if a.busy == nil {
				return
			}
		case <-time.After(2 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the search did not finish")
		}
	}
}
