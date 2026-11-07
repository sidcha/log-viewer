package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func mouseDown(a *App, x, y int) {
	a.handle(tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
}

func mouseUp(a *App, x, y int) {
	a.handle(tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone))
}

func click(a *App, x, y int) {
	mouseDown(a, x, y)
	mouseUp(a, x, y)
}

func drag(a *App, x, y int) {
	a.handle(tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
}

func TestClickPlacesTheCursor(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()

	click(a, 5, 4)
	if a.cursorLine() != 4 || a.curCol != 5 {
		t.Fatalf("cursor at %d:%d, want 4:5", a.cursorLine(), a.curCol)
	}
	if a.visualActive() {
		t.Error("a click started a selection")
	}
	if a.top != 0 {
		t.Errorf("a click scrolled the window to %d", a.top)
	}

	// Past the end of the text the cursor stops on the last character.
	click(a, 70, 2)
	if want := len(numberedLine(2)) - 1; a.curCol != want {
		t.Errorf("column = %d past the end of the line, want %d", a.curCol, want)
	}
}

func TestClickAccountsForTabsGutterAndScroll(t *testing.T) {
	a, _ := newTestApp(t, "\tafter-tab\n"+strings.Repeat("x", 200)+"\n")
	a.draw()
	click(a, 9, 0)
	if a.curCol != 2 {
		t.Errorf("column = %d clicking the second character after a tab, want 2", a.curCol)
	}

	run(a, "line-numbers-toggle")
	gw := a.gutterWidth()
	click(a, gw+3, 1)
	if a.curCol != 3 {
		t.Errorf("column = %d with a gutter, want 3", a.curCol)
	}
	click(a, 0, 1)
	if a.curCol != 0 {
		t.Errorf("column = %d clicking the gutter, want 0", a.curCol)
	}

	a.hoff = 50
	click(a, gw+3, 1)
	if a.curCol != 53 {
		t.Errorf("column = %d scrolled 50 columns right, want 53", a.curCol)
	}
}

func TestClickOnAWrappedLine(t *testing.T) {
	a, _ := newTestAppWith(t, strings.Repeat("a", 100)+"\nshort\n", "wrap", "on")
	a.draw()

	click(a, 4, 1)
	if a.cursorLine() != 0 || a.curCol != 84 {
		t.Errorf("cursor at %d:%d on the wrapped second row, want 0:84", a.cursorLine(), a.curCol)
	}
	click(a, 2, 2)
	if a.cursorLine() != 1 || a.curCol != 2 {
		t.Errorf("cursor at %d:%d on the line after the wrap, want 1:2", a.cursorLine(), a.curCol)
	}
}

func TestClickBelowTheTextGoesToTheLastLine(t *testing.T) {
	a, _ := newTestApp(t, numbered(3))
	a.draw()
	click(a, 2, 8)
	if a.cursorLine() != 2 {
		t.Errorf("cursor on line %d, want the last line", a.cursorLine())
	}
}

func TestDragSelects(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()

	mouseDown(a, 2, 1)
	drag(a, 6, 3)
	if !a.visualActive() {
		t.Fatal("a drag did not start a selection")
	}
	sl, sc, el, ec := a.span()
	if sl != 1 || sc != 2 || el != 3 || ec != 6 {
		t.Errorf("selection %d:%d-%d:%d, want 1:2-3:6", sl, sc, el, ec)
	}
	mouseUp(a, 6, 3)
	if !a.visualActive() {
		t.Error("releasing the button dropped the selection")
	}

	click(a, 0, 0)
	if a.visualActive() {
		t.Error("a click did not drop the selection")
	}
}

func TestPressAndReleaseWithoutMovingIsAClick(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()
	mouseDown(a, 3, 3)
	drag(a, 3, 3)
	mouseUp(a, 3, 3)
	if a.visualActive() {
		t.Error("a motion within one cell started a selection")
	}
}

func TestShiftClickExtendsFromTheCursor(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()
	click(a, 1, 1)
	a.handle(tcell.NewEventMouse(4, 2, tcell.Button1, tcell.ModShift))
	sl, sc, el, ec := a.span()
	if !a.visualActive() || sl != 1 || sc != 1 || el != 2 || ec != 4 {
		t.Errorf("selection %d:%d-%d:%d, want 1:1-2:4", sl, sc, el, ec)
	}
}

func TestDoubleClickSelectsAWordAndTripleALine(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta_2 gamma\n")
	a.draw()

	click(a, 8, 0)
	click(a, 8, 0)
	sl, sc, el, ec := a.span()
	if a.sel.mode != visualChar || sl != 0 || sc != 6 || el != 0 || ec != 11 {
		t.Errorf("double click selected %d:%d-%d:%d, want beta_2 at 0:6-0:11", sl, sc, el, ec)
	}

	click(a, 8, 0)
	if a.sel.mode != visualLine {
		t.Errorf("triple click mode = %v, want line-wise", a.sel.mode)
	}

	// A fourth press starts over as a single click.
	click(a, 8, 0)
	if a.visualActive() {
		t.Error("a fourth click left a selection")
	}
}

func TestClicksTooFarApartAreSingle(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta\n")
	a.draw()
	click(a, 1, 0)
	a.ptr.clickAt = time.Now().Add(-time.Second)
	click(a, 1, 0)
	if a.visualActive() {
		t.Error("two clicks a second apart made a double click")
	}
	click(a, 7, 0)
	if a.visualActive() {
		t.Error("clicks on different cells made a double click")
	}
}

func TestDragAgainstTheBottomEdgeScrolls(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()
	mouseDown(a, 0, 2)
	drag(a, 0, testH-1)
	if a.top != 1 {
		t.Fatalf("top = %d dragging onto the bottom row, want 1", a.top)
	}

	// Resting there, the queued repeat keeps scrolling.
	for i := 0; i < 3; i++ {
		f := <-a.post
		f()
	}
	if a.top != 4 {
		t.Errorf("top = %d after three repeats, want 4", a.top)
	}
	if a.cursorLine() != a.top+int64(testH-1) {
		t.Errorf("cursor on %d, want the bottom row's line %d", a.cursorLine(), a.top+int64(testH-1))
	}
	sl, _, _, _ := a.span()
	if sl != 2 {
		t.Errorf("selection starts at %d, want the line pressed on", sl)
	}

	mouseUp(a, 0, testH-1)
	f := <-a.post
	f()
	if a.top != 4 {
		t.Errorf("top = %d, the window kept scrolling after the release", a.top)
	}
}

func TestDragAgainstTheTopEdgeScrolls(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()
	a.top = 20
	mouseDown(a, 0, 5)
	drag(a, 0, 0)
	if a.top != 19 || a.cursorLine() != 19 {
		t.Errorf("top %d cursor %d dragging onto the top row, want 19 and 19", a.top, a.cursorLine())
	}
}

func TestDragAgainstTheRightEdgeScrollsSideways(t *testing.T) {
	a, _ := newTestApp(t, strings.Repeat("x", 200)+"\n")
	a.draw()
	mouseDown(a, 10, 0)
	drag(a, testW-1, 0)
	if a.hoff != 1 {
		t.Errorf("hoff = %d dragging onto the last column, want 1", a.hoff)
	}
	if a.curCol != testW {
		t.Errorf("column = %d, want the newly shown %d", a.curCol, testW)
	}
}

func TestDragWithoutRoomDoesNotQueueRepeats(t *testing.T) {
	a, _ := newTestApp(t, numbered(3))
	a.draw()
	mouseDown(a, 0, 0)
	drag(a, 2, 0)
	if a.ptr.edgeQueued {
		t.Error("a drag on a window that cannot scroll queued a repeat")
	}
}

func TestClickInTheMatchWindowLeavesTheLogAlone(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	keys(a, "/")
	cur := a.cursorLine()
	click(a, 3, 2)
	if a.cursorLine() != cur || a.find == nil {
		t.Error("a click behind the match window moved the cursor or closed it")
	}
}

func TestMouseOffIgnoresClicks(t *testing.T) {
	a, _ := newTestAppWith(t, numbered(100), "mouse", "off")
	a.draw()
	click(a, 3, 3)
	if a.cursorLine() != 0 {
		t.Error("a click moved the cursor with the mouse off")
	}
}

func TestReleaseIsBindable(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	out := clipboardSink(t, a)
	if err := a.cfg.Parse("t", "bind -T visual <LeftRelease> yank\n"); err != nil {
		t.Fatal(err)
	}
	a.draw()
	mouseDown(a, 5, 1)
	drag(a, 7, 1)
	mouseUp(a, 7, 1)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "001" {
		t.Errorf("copied %q on release, want %q", got, "001")
	}
}
