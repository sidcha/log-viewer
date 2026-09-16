package ui

import (
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
)

func set(a *App, args ...string) {
	a.exec(config.Command{Name: "set", Args: args}, 0)
}

// TestSetDoesNotRestartFollowing: `follow on` in a config says where a
// session starts. Scrolling away ends it, and changing an unrelated setting
// must not bring it back.
func TestSetDoesNotRestartFollowing(t *testing.T) {
	a, _ := newTestAppWith(t, numbered(100), "follow", "on")
	if !a.follow {
		t.Fatal("follow on in the config did not start following")
	}
	run(a, "up")
	if a.follow {
		t.Fatal("a motion did not stop following")
	}
	set(a, "wrap", "on")
	a.draw()
	if a.follow {
		t.Error(":set wrap started following again")
	}
}

// TestStatusPinSurvivesSet: zs pins the status line, and an unrelated :set
// used to unpin it silently.
func TestStatusPinSurvivesSet(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	run(a, "status-toggle")
	if !a.showStatus() {
		t.Fatal("zs did not pin the status line")
	}
	set(a, "wrap", "on")
	if !a.showStatus() {
		t.Error(":set wrap unpinned the status line")
	}
	run(a, "status-toggle")
	if a.showStatus() {
		t.Error("a second zs did not unpin it")
	}
}

// TestTabWidthReachesTheLayout: the cursor is placed by the setting and the
// text by the layout, and with a stale layout they disagree on any line with
// a tab.
func TestTabWidthReachesTheLayout(t *testing.T) {
	a, scr := newTestApp(t, "a\tb\n")
	set(a, "tab-width", "4")
	run(a, "line-end")

	if got := rowText(scr, 0); got != "a   b" {
		t.Fatalf("row = %q, want the tab four columns wide", got)
	}
	x, y, visible := scr.GetCursor()
	if !visible || x != 4 || y != 0 {
		t.Errorf("cursor at (%d, %d) visible=%v, want on the b at column 4", x, y, visible)
	}
}

func TestMouseCanBeTurnedBackOn(t *testing.T) {
	a, _ := newTestApp(t, numbered(10))
	set(a, "mouse", "off")
	a.opts()
	if a.mouseOn {
		t.Fatal(":set mouse off left it on")
	}
	set(a, "mouse", "on")
	a.opts()
	if !a.mouseOn {
		t.Error(":set mouse on did not enable it")
	}
}
