package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/index"
)

// appendLines writes more lines to the file the app is viewing and brings the
// index up to date, standing in for a producer that is still running.
func appendLines(t *testing.T, a *App, from, count int) {
	t.Helper()

	f, err := os.OpenFile(a.src.Name(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := from; i < from+count; i++ {
		b.WriteString(numberedLine(i))
		b.WriteByte('\n')
	}
	if _, err := f.WriteString(b.String()); err != nil {
		t.Fatal(err)
	}
	f.Close()

	ix := index.NewIndexer(a.src, a.idx, index.Options{Block: 256})
	ix.Sync(t.Context())
}

// TestFollowCarriesTheCursor is the property that makes follow usable with a
// cursor: the window tracking the tail while the cursor stayed behind meant
// yanking copied a line that had long scrolled away.
func TestFollowCarriesTheCursor(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	run(a, "follow-toggle")
	if !a.follow {
		t.Fatal("follow did not turn on")
	}
	if got, want := a.cursorLine(), a.viewLen()-1; got != want {
		t.Fatalf("cursor is on %d, want the last line %d", got, want)
	}

	appendLines(t, a, 40, 10)
	a.draw()

	if got, want := a.cursorLine(), a.viewLen()-1; got != want {
		t.Fatalf("after new output the cursor is on %d, want %d", got, want)
	}
	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(ln.Text) != numberedLine(49) {
		t.Errorf("cursor is on %q, want the newest line", ln.Text)
	}
}

// TestYankWhileFollowingTakesTheNewestLine is the same bug seen from the place
// it actually hurt.
func TestYankWhileFollowingTakesTheNewestLine(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	out := clipboardSink(t, a)

	run(a, "follow-toggle")
	appendLines(t, a, 40, 5)
	a.draw()

	keys(a, "yy")
	if got, want := readSink(t, out), numberedLine(44)+"\n"; got != want {
		t.Errorf("yanked %q, want the newest line %q", got, want)
	}
}

// TestMotionLeavesFollowFromTheTail checks that stepping back starts from
// where the screen is, not from wherever the cursor had been abandoned.
func TestMotionLeavesFollowFromTheTail(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))

	run(a, "follow-toggle")
	last := a.viewLen() - 1

	keys(a, "k")
	if a.follow {
		t.Error("a motion did not leave follow mode")
	}
	if got := a.cursorLine(); got != last-1 {
		t.Fatalf("cursor is on %d, want %d (one back from the tail)", got, last-1)
	}
}

func TestFollowStopsWhenScrollingAway(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	run(a, "follow-toggle")

	run(a, "page-up")
	if a.follow {
		t.Error("paging up did not leave follow mode")
	}

	// New output must not drag the view back while the user is reading.
	top := a.top
	appendLines(t, a, 100, 20)
	a.draw()
	if a.top != top {
		t.Errorf("the window moved to %d while follow was off (was %d)", a.top, top)
	}
}

func TestGotoEndPutsTheCursorOnTheLastLine(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))
	keys(a, "G")

	if got, want := a.cursorLine(), a.viewLen()-1; got != want {
		t.Fatalf("G left the cursor on %d, want %d", got, want)
	}
	// And the last line is actually on screen.
	if a.cursorLine() < a.top || a.cursorLine() >= a.top+int64(a.contentRows()) {
		t.Errorf("the cursor at %d is outside the window starting at %d", a.cursorLine(), a.top)
	}
}

// TestFollowWithAFilterTracksSurvivingLines checks the combination that makes
// a piped session useful: watch a live stream, but only the lines that matter.
func TestFollowWithAFilterTracksSurvivingLines(t *testing.T) {
	a, _ := newTestApp(t, numbered(30))

	a.exec(cmdOnly("ERROR"), 0)
	waitFiltered(t, a)
	run(a, "follow-toggle")

	before := a.viewLen()
	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ln.Text), "ERROR") {
		t.Fatalf("cursor is on %q, which the filter should have hidden", ln.Text)
	}

	appendLines(t, a, 30, 12)
	waitGrow(t, a, before)
	a.draw()

	if got, want := a.cursorLine(), a.viewLen()-1; got != want {
		t.Errorf("cursor is on %d, want the newest surviving line %d", got, want)
	}
	ln, err = a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ln.Text), "ERROR") {
		t.Errorf("cursor landed on %q, which the filter hides", ln.Text)
	}
}

// waitGrow waits for the view to take in lines appended after the scan had
// already caught up, which is the live-stream case.
func waitGrow(t *testing.T, a *App, was int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.viewLen() <= was {
		if time.Now().After(deadline) {
			t.Fatalf("the view did not grow past %d; it is still %d", was, a.viewLen())
		}
		time.Sleep(time.Millisecond)
	}
}

func cmdOnly(pattern string) config.Command {
	return config.Command{Name: "filter-only", Args: []string{pattern}}
}
