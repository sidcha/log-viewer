package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
)

func statusRow(a *App, scr tcell.SimulationScreen) string {
	a.draw()
	return rowText(scr, testH-1)
}

// TestEveryStatusFieldIsDrawn: a field config accepts but the viewer does not
// know would pass validation and print nothing.
func TestEveryStatusFieldIsDrawn(t *testing.T) {
	for _, f := range config.StatusFields {
		if _, ok := statusFields[f]; !ok {
			t.Errorf("status field %q has no implementation", f)
		}
	}
	for f := range statusFields {
		if !config.IsStatusField(f) {
			t.Errorf("status field %q is drawn but config rejects it", f)
		}
	}
}

func TestStatusSidesGravitateToTheEdges(t *testing.T) {
	a, scr := newTestAppWith(t, isoLines(100), "status-line", "on")
	a.setCursor(50)

	row := statusRow(a, scr)
	if !strings.HasPrefix(row, " test.log") {
		t.Errorf("left side not at the left edge: %q", row)
	}
	// The default right side ends in one cell of padding, which rowText trims.
	if !strings.HasSuffix(row, "51/100  50%") {
		t.Errorf("position not at the right edge: %q", row)
	}
	if got := len([]rune(rowText(scr, testH-1))); got != testW-1 {
		t.Errorf("right side ends at column %d, want %d", got, testW-1)
	}
}

// Moving into another region of the file shows that region's profile and why
// it was chosen.
func TestStatusProfileFollowsTheCursorAcrossSegments(t *testing.T) {
	var b strings.Builder
	for range 70 {
		b.WriteString("[    1.000000] kernel line\n")
	}
	b.WriteString("U-Boot 2024.01 (Jan 01 2024)\n=> boot\n")
	a, scr, _ := newTestAppConf(t, b.String(), `
set status-right '{profile}'
profile uboot {
    marker 'U-Boot ' '^U-Boot [0-9]{4}\.[0-9]{2}'
    rule '^=> .*' syntax.level.warn
}
`)
	// The harness turns the status line off after reading the configuration.
	if err := a.cfg.Set("status-line", "on"); err != nil {
		t.Fatal(err)
	}
	if row := statusRow(a, scr); !strings.Contains(row, "kernel (detected") {
		t.Errorf("at the top: %q", row)
	}
	run(a, "goto-end")
	if row := statusRow(a, scr); !strings.Contains(row, "uboot (marker, line 71)") {
		t.Errorf("at the bootloader: %q", row)
	}
}

// A field with nothing to say takes the spaces before it along, or the ones
// after it when nothing precedes it, so an idle flag leaves no gap.
func TestStatusEmptyFieldTakesItsSpaces(t *testing.T) {
	for _, tc := range []struct{ left, want string }{
		{"A  {flags}  B", "A  B"},
		{"A  {flags}", "A"},
		{"{flags}  B", "B"},
		{"A  {flags}  {keys}  B", "A  B"},
	} {
		a, scr := newTestAppWith(t, "x\n", "status-line", "on", "status-left", tc.left, "status-right", "")
		if got := statusRow(a, scr); got != tc.want {
			t.Errorf("status-left %q = %q, want %q", tc.left, got, tc.want)
		}
	}
}

// When the sides collide the right keeps at most half the row, cut from its
// left so the percentage at the edge survives; the left is cut at the gap.
func TestStatusRightSideKeepsItsEdgeWhenCrowded(t *testing.T) {
	a, scr := newTestAppWith(t, isoLines(100), "status-line", "on",
		"status-left", strings.Repeat("L", 60),
		"status-right", strings.Repeat("R", 50)+" {percent}")
	row := statusRow(a, scr)

	if !strings.HasSuffix(row, "R 0%") {
		t.Errorf("right edge lost: %q", row)
	}
	if n := strings.Count(row, "R"); n+len(" 0%") != testW/2 {
		t.Errorf("right side is %d wide, want %d", n+len(" 0%"), testW/2)
	}
	if !strings.HasPrefix(row, strings.Repeat("L", testW/2-1)+" ") {
		t.Errorf("left side not cut at the gap: %q", row)
	}
}

// A message still takes the whole row.
func TestStatusMessageReplacesBothSides(t *testing.T) {
	a, scr := newTestAppWith(t, isoLines(100), "status-line", "on")
	a.infof("hello")
	if got := statusRow(a, scr); got != "hello" {
		t.Errorf("row = %q", got)
	}
}

func TestBufferFieldOnlyAppearsWithSeveralFiles(t *testing.T) {
	a, scr := newTestAppWith(t, numbered(50), "status-line", "on")
	a.draw()
	if strings.Contains(rowText(scr, testH-1), "[1/") {
		t.Errorf("one file is tagged: %q", rowText(scr, testH-1))
	}
	if strings.HasPrefix(rowText(scr, testH-1), "  ") {
		t.Errorf("the empty field left a gap: %q", rowText(scr, testH-1))
	}

	addTestBuffer(t, a, "b.log", numbered(50))
	a.clearMsg()
	a.draw()
	if !strings.Contains(rowText(scr, testH-1), "[2/2] b.log") {
		t.Errorf("status line is %q, want the tag before the file", rowText(scr, testH-1))
	}
}
