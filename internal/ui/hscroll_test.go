package ui

import (
	"strings"
	"testing"
)

func TestHorizontalScrollUsesTheStep(t *testing.T) {
	line := strings.Repeat("0123456789", 30)
	a, scr := newTestAppWith(t, line+"\n", "h-scroll-step", "5")

	keys(a, "zl")
	a.draw()
	if a.hoff != 5 {
		t.Fatalf("hoff = %d after zl, want 5", a.hoff)
	}
	if got := rowText(scr, 0); !strings.HasPrefix(got, "56789") {
		t.Errorf("row = %q, want it to start five columns in", got)
	}
	// The cursor was at column 0, now off the left edge: it comes along.
	if a.curCol != 5 {
		t.Errorf("cursor at byte %d, want dragged to 5", a.curCol)
	}

	keys(a, "3zl")
	if a.hoff != 20 {
		t.Errorf("hoff = %d after 3zl, want 20", a.hoff)
	}
	keys(a, "zh")
	if a.hoff != 15 {
		t.Errorf("hoff = %d after zh, want 15", a.hoff)
	}
	keys(a, "9zh")
	if a.hoff != 0 {
		t.Errorf("hoff = %d, want it stopped at 0", a.hoff)
	}
}

func TestByteAtColumnSkipsTabsAndWideRunes(t *testing.T) {
	text := []byte("a\tb語c")
	for col, want := range map[int]int{0: 0, 1: 1, 5: 2, 8: 2, 9: 3, 10: 6, 11: 6, 12: 7} {
		if got := byteAtColumn(text, col, 8); got != want {
			t.Errorf("byteAtColumn(%d) = %d, want %d", col, got, want)
		}
	}
}
