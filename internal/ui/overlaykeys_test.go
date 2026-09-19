package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

func shiftSpace(a *App) {
	a.key(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModShift))
}

// TestSpaceScrollsTheHelp: it used to fall through and page the log behind the
// panel, which is both useless and invisible.
func TestSpaceScrollsTheHelp(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	run(a, "help")

	top := a.ov.top
	logTop := a.top

	keys(a, " ")
	if a.ov.top <= top {
		t.Fatalf("Space left the help at row %d", a.ov.top)
	}
	if a.top != logTop {
		t.Errorf("Space scrolled the log behind the panel: %d -> %d", logTop, a.top)
	}
}

func TestOverlayPagingKeys(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	run(a, "help")

	keys(a, " ")
	keys(a, " ")
	after := a.ov.top
	if after == 0 {
		t.Fatal("two pages did not move the help")
	}

	keys(a, "b")
	if a.ov.top >= after {
		t.Errorf("b did not page back: %d, was %d", a.ov.top, after)
	}

	keys(a, "G")
	atEnd := a.ov.top
	keys(a, "g")
	if a.ov.top != 0 {
		t.Errorf("g did not go to the top: %d", a.ov.top)
	}
	if atEnd <= 0 {
		t.Error("G did not go to the bottom")
	}

	a.key(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone))
	if a.ov.top == 0 {
		t.Error("PgDn did not page the help")
	}
}

// TestOverlaySwallowsKeys: a panel on screen owns the keyboard. A stray / used
// to open a search prompt underneath it.
func TestOverlaySwallowsKeys(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	run(a, "help")

	logTop, cur := a.top, a.cursorLine()
	keys(a, "/")
	if a.prompt != nil {
		t.Error("a key fell through the panel and opened a prompt")
	}
	keys(a, "jjjj") // the panel's own scroll, not the log's
	if a.top != logTop || a.cursorLine() != cur {
		t.Errorf("keys moved the log behind the panel: top %d->%d cursor %d->%d",
			logTop, a.top, cur, a.cursorLine())
	}

	keys(a, "q")
	if a.ov != nil {
		t.Error("q did not close the panel")
	}
	if a.quit {
		t.Error("q quit lv instead of closing the panel")
	}
}

// TestRulesListKeepsSpaceForToggling: in the rules list Space is the toggle,
// which is worth more there than paging a list that fits on screen.
func TestRulesListKeepsSpaceForToggling(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	a.addRule(filter.Only, "ERROR", color.RGB{})
	waitFiltered(t, a)
	run(a, "filter-list")

	keys(a, " ")
	if a.rules.All()[0].Enabled {
		t.Error("Space did not toggle the rule in the rules list")
	}
}

// TestShiftSpacePagesBack records the intent, and what currently gets in the
// way of it.
func TestShiftSpacePagesBack(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	run(a, "help")
	keys(a, " ")
	keys(a, " ")
	after := a.ov.top

	shiftSpace(a)
	if a.ov.top >= after {
		t.Skipf("Shift-Space arrived as plain Space: tcell's NewEventKey drops "+
			"ModShift from rune events, so lv cannot tell the two apart "+
			"(help is at row %d, was %d). b, C-b and PgUp page back.", a.ov.top, after)
	}
}
