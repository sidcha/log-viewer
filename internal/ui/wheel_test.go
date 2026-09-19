package ui

import "testing"

func TestWheelIsABinding(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	a.draw()

	wheel(a, false)
	if a.top != 3 {
		t.Fatalf("top = %d after a wheel turn, want 3", a.top)
	}

	if err := a.cfg.Parse("t", "set scroll-step 2\nbind <WheelDown> scroll-down 5\n"); err != nil {
		t.Fatal(err)
	}
	wheel(a, false)
	if a.top != 13 {
		t.Errorf("top = %d, want 3 + 5 steps of 2", a.top)
	}
	wheel(a, true)
	if a.top != 7 {
		t.Errorf("top = %d after scrolling up 3 steps of 2, want 7", a.top)
	}
}

func TestWheelInTheFinderMovesTheSelection(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	keys(a, "/")
	typeInto(a, "line")
	settleFinder(t, a)
	top := a.top
	wheel(a, false)
	if a.find.sel != 1 {
		t.Errorf("selection = %d, want the wheel to move it", a.find.sel)
	}
	if a.top != top {
		t.Error("the log scrolled behind the match window")
	}
}

func TestWheelInAPanelScrollsIt(t *testing.T) {
	a, _ := newTestApp(t, numbered(10))
	run(a, "help")
	wheel(a, false)
	if a.ov.top != 3 {
		t.Errorf("panel top = %d, want 3", a.ov.top)
	}
}
