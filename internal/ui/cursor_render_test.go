package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// cursorAt reports where the terminal cursor actually is.
func cursorAt(t *testing.T, scr tcell.SimulationScreen) (int, int) {
	t.Helper()
	x, y, visible := scr.GetCursor()
	if !visible {
		t.Fatal("no cursor is shown")
	}
	return x, y
}

// TestCursorIsVisible is the whole point of having a column at all: without a
// drawn cursor, h, l, w, b, e, $ and char-wise visual all move something the
// user cannot see, and * can only ever read column zero.
func TestCursorIsVisible(t *testing.T) {
	a, scr := newTestApp(t, "alpha beta gamma\nsecond line\n")
	a.draw()

	x, y := cursorAt(t, scr)
	if x != 0 || y != 0 {
		t.Fatalf("cursor starts at (%d,%d), want (0,0)", x, y)
	}
}

func TestCursorFollowsColumnMotions(t *testing.T) {
	a, scr := newTestApp(t, "alpha beta gamma\nsecond line\n")

	keys(a, "w") // onto "beta", byte 6
	a.draw()
	if x, _ := cursorAt(t, scr); x != 6 {
		t.Errorf("after w the cursor is at column %d, want 6", x)
	}

	keys(a, "$")
	a.draw()
	if x, _ := cursorAt(t, scr); x != 15 {
		t.Errorf("after $ the cursor is at column %d, want 15", x)
	}

	keys(a, "0")
	a.draw()
	if x, _ := cursorAt(t, scr); x != 0 {
		t.Errorf("after 0 the cursor is at column %d, want 0", x)
	}

	keys(a, "j")
	a.draw()
	if _, y := cursorAt(t, scr); y != 1 {
		t.Errorf("after j the cursor is on row %d, want 1", y)
	}
}

func TestCursorAccountsForTheGutter(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	run(a, "line-numbers-toggle")
	keys(a, "ll")
	a.draw()

	gw := a.gutterWidth()
	if gw == 0 {
		t.Fatal("no gutter")
	}
	if x, _ := cursorAt(t, scr); x != gw+2 {
		t.Errorf("cursor at column %d, want %d (gutter %d + 2)", x, gw+2, gw)
	}
}

func TestCursorAccountsForHorizontalScroll(t *testing.T) {
	// Comfortably wider than the 80-column test screen.
	long := strings.Repeat("abcdefghij", 20) + "END"
	a, scr := newTestApp(t, long+"\n")

	keys(a, "$")
	a.draw()
	if a.hoff == 0 {
		t.Fatal("$ on a line wider than the screen did not scroll")
	}
	x, _ := cursorAt(t, scr)
	if x < 0 || x >= testW {
		t.Fatalf("cursor at column %d is off screen", x)
	}
	// It must sit on the last character, wherever the window ended up.
	if want := a.curCol - a.hoff; x != want {
		t.Errorf("cursor at %d, want %d (col %d minus hoff %d)", x, want, a.curCol, a.hoff)
	}
}

func TestCursorHandlesTabs(t *testing.T) {
	a, scr := newTestApp(t, "\tindented\n")
	keys(a, "l") // past the tab, onto 'i'
	a.draw()

	x, _ := cursorAt(t, scr)
	if want := a.cfg.Int("tab-width"); x != want {
		t.Errorf("cursor after a tab is at column %d, want %d", x, want)
	}
}

// TestPromptOwnsTheCursor: while typing, the cursor belongs to the prompt.
func TestPromptOwnsTheCursor(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	keys(a, "j")
	a.draw()

	a.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	typeInto(a, "ERR")
	a.draw()

	_, y := cursorAt(t, scr)
	if y != testH-1 {
		t.Errorf("while prompting the cursor is on row %d, want the status row %d", y, testH-1)
	}

	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	a.draw()
	if _, y := cursorAt(t, scr); y != 1 {
		t.Errorf("after the prompt closed the cursor is on row %d, want 1", y)
	}
}

func TestOverlayHidesTheCursor(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	run(a, "help")
	if _, _, visible := scr.GetCursor(); visible {
		t.Error("the cursor is still shown behind an overlay")
	}
}

// TestStarPicksTheWordAtTheCursor is what the visible cursor buys: * can only
// be useful if you can see which word it is about to take.
func TestStarPicksTheWordAtTheCursor(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta gamma\nbeta again\n")

	keys(a, "w") // onto "beta"
	if got := a.wordUnderCursor(); got != "beta" {
		t.Fatalf("word under cursor = %q, want beta", got)
	}
	keys(a, "w") // onto "gamma"
	if got := a.wordUnderCursor(); got != "gamma" {
		t.Fatalf("word under cursor = %q, want gamma", got)
	}

	keys(a, "*")
	if a.search.m == nil || !a.search.m.Match([]byte("gamma here")) {
		t.Error("* did not search for the word the cursor was on")
	}
}
