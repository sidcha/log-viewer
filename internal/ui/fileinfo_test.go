package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

// isoLines builds n lines of equal length, every tenth an ERROR, so a byte
// offset and a line number give the same percentage.
func isoLines(n int) string {
	var b strings.Builder
	for i := range n {
		level := "INFO "
		if i%10 == 0 {
			level = "ERROR"
		}
		fmt.Fprintf(&b, "2024-01-02T03:04:05 %s n=%04d\n", level, i)
	}
	return b.String()
}

func TestFileInfoShowsPathPositionSizeAndProfile(t *testing.T) {
	a, scr := newTestApp(t, isoLines(100))
	scr.SetSize(200, testH)
	a.setCursor(50)
	run(a, "file-info")

	for _, want := range []string{
		a.src.Name(),
		"line 51 of 100 (50%)",
		humanBytes(a.src.Size()),
		"iso (detected",
	} {
		if !strings.Contains(a.msg, want) {
			t.Errorf("file info %q lacks %q", a.msg, want)
		}
	}
}

func TestFileInfoInAFilterNamesTheSourceLine(t *testing.T) {
	a, scr := newTestApp(t, isoLines(100))
	scr.SetSize(200, testH)
	a.addRule(filter.Only, "ERROR", color.RGB{})
	waitFiltered(t, a)
	a.setCursor(5)
	run(a, "file-info")

	for _, want := range []string{"line 6 of 10 shown (source line 51, 50%)", "FILTER×1"} {
		if !strings.Contains(a.msg, want) {
			t.Errorf("file info %q lacks %q", a.msg, want)
		}
	}
}

// A file bigger than memory is still being counted when you ask; the total so
// far is a floor, not the answer.
func TestFileInfoMarksAnUnfinishedCount(t *testing.T) {
	a, scr := newTestApp(t, isoLines(100))
	scr.SetSize(200, testH)
	a.idx.SetSize(2 * a.idx.Size())
	run(a, "file-info")

	for _, want := range []string{"line 1 of ≥", "indexing 50%"} {
		if !strings.Contains(a.msg, want) {
			t.Errorf("file info %q lacks %q", a.msg, want)
		}
	}
}

func TestFileInfoOnAPipeSaysStdin(t *testing.T) {
	a, _ := newPipedApp(t, isoLines(10))
	run(a, "file-info")
	if !strings.HasPrefix(a.msg, "(stdin)  line 1 of 10") {
		t.Errorf("file info = %q", a.msg)
	}
}

func TestFileInfoOnAnEmptyFile(t *testing.T) {
	a, _ := newTestApp(t, "")
	run(a, "file-info")
	if !strings.Contains(a.msg, "empty") {
		t.Errorf("file info = %q", a.msg)
	}
}

// On a narrow terminal the flags and the size give way first, then the
// directories of the path; where you are and how it is coloured stay.
func TestFileInfoFitsANarrowTerminal(t *testing.T) {
	const width = 60
	a, scr := newTestAppWith(t, isoLines(100), "wrap", "on")
	scr.SetSize(width, testH)
	a.setCursor(50)
	run(a, "file-info")

	if n := utf8.RuneCountInString(a.msg); n > width {
		t.Errorf("file info is %d wide on a %d-column terminal: %q", n, width, a.msg)
	}
	if !strings.HasPrefix(a.msg, "…") || !strings.Contains(a.msg, "test.log") {
		t.Errorf("path not shortened from the left: %q", a.msg)
	}
	for _, want := range []string{"line 51 of 100", "iso ("} {
		if !strings.Contains(a.msg, want) {
			t.Errorf("file info %q lacks %q", a.msg, want)
		}
	}
	if strings.Contains(a.msg, "WRAP") {
		t.Errorf("flags kept over the position: %q", a.msg)
	}
}

func TestCtrlGShowsFileInfoOnTheBottomRow(t *testing.T) {
	a, scr := newTestAppWith(t, isoLines(100), "status-line", "auto")
	ctrl(a, tcell.KeyCtrlG)
	a.draw()

	if got := rowText(scr, testH-1); !strings.Contains(got, "line 1 of 100") {
		t.Fatalf("bottom row = %q", got)
	}
}
