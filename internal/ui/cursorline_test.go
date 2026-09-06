package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/filter"
)

func bgAt(scr tcell.SimulationScreen, x, y int) tcell.Color {
	_, _, st, _ := scr.GetContent(x, y)
	_, bg, _ := st.Decompose()
	return bg
}

// TestCursorLineSpansTheWholeRow: tinting only the characters leaves the band
// ending wherever the line happens to stop, which reads as a smudge behind the
// text rather than as a cursor line.
func TestCursorLineSpansTheWholeRow(t *testing.T) {
	a, scr := newTestApp(t, "short\n"+numbered(5))
	a.draw()

	want, ok := a.cursorTint(0)
	if !ok {
		t.Fatal("no cursor tint configured")
	}
	for x := 0; x < testW; x++ {
		if got := bgAt(scr, x, 0); got != want {
			t.Fatalf("cursor line breaks at column %d: %v, want %v", x, got, want)
		}
	}
	// And no other row is tinted.
	for x := 0; x < testW; x++ {
		if bgAt(scr, x, 1) == want {
			t.Fatalf("row 1 is tinted at column %d", x)
		}
	}
}

func TestCursorLineFollowsTheCursor(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	want, _ := a.cursorTint(0)

	keys(a, "3j")
	a.draw()

	if got := bgAt(scr, 40, 3); got != want {
		t.Errorf("row 3 background = %v, want the tint", got)
	}
	if got := bgAt(scr, 40, 0); got == want {
		t.Error("row 0 is still tinted after the cursor moved")
	}
}

// TestCursorLineKeepsRuleColours is why the tint is not painted over every
// cell: a rule's colour is what says why the line is on screen, and black mark
// text on a dark tint would be unreadable.
func TestCursorLineKeepsRuleColours(t *testing.T) {
	a, scr := newTestApp(t, "plain marked here\n")
	a.addRule(filter.Highlight, "marked", filter.Color{})
	a.draw()

	markBg := a.styleFor(a.rules.All()[0].Color)
	_, want, _ := markBg.Decompose()

	// "marked" starts at column 6.
	if got := bgAt(scr, 6, 0); got != want {
		t.Errorf("the mark lost its colour on the cursor line: %v, want %v", got, want)
	}
	// The rest of the row still carries the tint.
	tint, _ := a.cursorTint(0)
	if got := bgAt(scr, 0, 0); got != tint {
		t.Errorf("column 0 background = %v, want the tint", got)
	}
}

func TestCursorLineOffWhenDisabled(t *testing.T) {
	a, scr := newTestApp(t, numbered(10))
	if err := a.cfg.Set("cursor-line", "off"); err != nil {
		t.Fatal(err)
	}
	a.draw()

	if _, ok := a.cursorTint(0); ok {
		t.Error("cursorTint still reports a tint when the setting is off")
	}
	for x := 0; x < testW; x++ {
		if bgAt(scr, x, 0) != tcell.ColorDefault {
			t.Fatalf("row 0 is tinted at column %d with cursor-line off", x)
		}
	}
}

// TestSelectionReplacesTheCursorLine keeps the two from fighting: while
// selecting, the selection is the thing that matters.
func TestSelectionReplacesTheCursorLine(t *testing.T) {
	a, scr := newTestApp(t, numbered(10))
	keys(a, "V")
	a.draw()

	tint, ok := a.cursorTint(0)
	if ok {
		t.Fatalf("the cursor tint is still active during a selection (%v)", tint)
	}
	_, want, _ := a.cfg.Style("selection").Decompose()
	if got := bgAt(scr, 0, 0); got != want {
		t.Errorf("selected row background = %v, want the selection colour", got)
	}
}

func TestCursorLineWithLineNumbers(t *testing.T) {
	a, scr := newTestApp(t, numbered(10))
	run(a, "line-numbers-toggle")

	want, _ := a.cursorTint(0)
	// The gutter is part of the row, so it carries the band too.
	if got := bgAt(scr, 0, 0); got != want {
		t.Errorf("gutter background = %v, want the tint", got)
	}
}
