package ui

import (
	"fmt"
	"testing"

	"github.com/sidcha/log-viewer/internal/filter"
)

// at reports where the cursor is, as line and byte column.
func at(a *App) (int64, int) { return a.cursorLine(), a.curCol }

func wantAt(t *testing.T, a *App, line int64, col int, what string) {
	t.Helper()
	gotLine, gotCol := at(a)
	if gotLine != line || gotCol != col {
		t.Fatalf("%s: cursor at (%d,%d), want (%d,%d)", what, gotLine, gotCol, line, col)
	}
}

// TestWordEndCrossesLines is the reported case: pressing e repeatedly used to
// stop dead at the end of a line.
func TestWordEndCrossesLines(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta\ngamma delta\n")

	keys(a, "e")
	wantAt(t, a, 0, 4, "e onto the end of alpha")
	keys(a, "e")
	wantAt(t, a, 0, 9, "e onto the end of beta")
	keys(a, "e")
	wantAt(t, a, 1, 4, "e across the line break onto the end of gamma")
	keys(a, "e")
	wantAt(t, a, 1, 10, "e onto the end of delta")

	// At the very end it stays put rather than wrapping or erroring.
	keys(a, "e")
	wantAt(t, a, 1, 10, "e at the end of the view")
}

func TestWordForwardCrossesLines(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta\ngamma delta\n")

	keys(a, "w")
	wantAt(t, a, 0, 6, "w onto beta")
	keys(a, "w")
	wantAt(t, a, 1, 0, "w across the line break onto gamma")
	keys(a, "w")
	wantAt(t, a, 1, 6, "w onto delta")
	keys(a, "w")
	wantAt(t, a, 1, 10, "w at the end of the view")
}

func TestWordBackCrossesLines(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta\ngamma delta\n")

	keys(a, "j$")
	wantAt(t, a, 1, 10, "$ on the second line")
	keys(a, "b")
	wantAt(t, a, 1, 6, "b onto delta")
	keys(a, "b")
	wantAt(t, a, 1, 0, "b onto gamma")
	keys(a, "b")
	wantAt(t, a, 0, 6, "b across the line break onto beta")
	keys(a, "b")
	wantAt(t, a, 0, 0, "b onto alpha")
	keys(a, "b")
	wantAt(t, a, 0, 0, "b at the start of the view")
}

// TestWordMotionsAcrossManyLines walks the whole way there and back, which is
// what reading a log with these actually looks like.
func TestWordMotionsAcrossManyLines(t *testing.T) {
	a, _ := newTestApp(t, "aa bb\ncc dd\nee ff\n")

	for i := 0; i < 5; i++ {
		keys(a, "w")
	}
	wantAt(t, a, 2, 3, "five w from the start")

	for i := 0; i < 5; i++ {
		keys(a, "b")
	}
	wantAt(t, a, 0, 0, "five b back again")
}

func TestWordMotionsWithCounts(t *testing.T) {
	a, _ := newTestApp(t, "aa bb\ncc dd\nee ff\n")

	keys(a, "5w")
	wantAt(t, a, 2, 3, "5w")
	keys(a, "3b")
	wantAt(t, a, 1, 0, "3b")
	keys(a, "2e")
	wantAt(t, a, 1, 4, "2e")
}

// Empty lines are a stop for w and b, and skipped by e, as in vi.
func TestEmptyLinesAreStopsForWAndB(t *testing.T) {
	a, _ := newTestApp(t, "alpha\n\nbeta\n")

	keys(a, "w")
	wantAt(t, a, 1, 0, "w onto the empty line")
	keys(a, "w")
	wantAt(t, a, 2, 0, "w off the empty line")

	keys(a, "b")
	wantAt(t, a, 1, 0, "b back onto the empty line")
	keys(a, "b")
	wantAt(t, a, 0, 0, "b off the empty line")
}

func TestEmptyLinesAreSkippedByE(t *testing.T) {
	a, _ := newTestApp(t, "alpha\n\nbeta\n")

	keys(a, "e")
	wantAt(t, a, 0, 4, "e onto the end of alpha")
	keys(a, "e")
	wantAt(t, a, 2, 3, "e past the empty line onto the end of beta")
}

// Punctuation is its own class, so a path or a timestamp breaks into pieces
// the way it does in vi.
func TestWordClassesAcrossLines(t *testing.T) {
	a, _ := newTestApp(t, "foo.bar\nbaz\n")

	keys(a, "w")
	wantAt(t, a, 0, 3, "w onto the dot")
	keys(a, "w")
	wantAt(t, a, 0, 4, "w onto bar")
	keys(a, "w")
	wantAt(t, a, 1, 0, "w across onto baz")
}

// TestWordMotionsFollowTheView is what makes this right for a log viewer: with
// a filter on, the next line is the next visible line, not the next in the file.
func TestWordMotionsFollowTheView(t *testing.T) {
	var src string
	for i := 0; i < 30; i++ {
		if i%3 == 0 {
			src += fmt.Sprintf("keep%02d here\n", i)
		} else {
			src += fmt.Sprintf("drop%02d gone\n", i)
		}
	}
	a, _ := newTestApp(t, src)

	a.addRule(filter.Only, "keep", filter.Color{})
	waitFiltered(t, a)

	keys(a, "w") // onto "here" on the first kept line
	wantAt(t, a, 0, 7, "w within the first kept line")
	keys(a, "w") // across to the next kept line
	wantAt(t, a, 1, 0, "w onto the next kept line")

	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(ln.Text[:4]) != "keep" {
		t.Errorf("w landed on %q, which the filter hides", ln.Text)
	}
}

// TestWordMotionScrollsTheWindow: crossing a line has to bring the view along.
func TestWordMotionScrollsTheWindow(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	for i := 0; i < 200; i++ {
		keys(a, "w")
	}
	if a.cursorLine() < a.top || a.cursorLine() >= a.top+int64(a.contentRows()) {
		t.Fatalf("cursor at %d is outside the window at %d", a.cursorLine(), a.top)
	}
	if a.top == 0 {
		t.Error("the window never scrolled while the cursor walked down")
	}
}

func TestHAndLStayOnTheirLine(t *testing.T) {
	// vi's default whichwrap leaves h and l alone; only the word motions wrap.
	a, _ := newTestApp(t, "ab\ncd\n")

	keys(a, "$")
	keys(a, "l")
	wantAt(t, a, 0, 1, "l at the end of a line")

	keys(a, "j0")
	keys(a, "h")
	wantAt(t, a, 1, 0, "h at the start of a line")
}
