package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

func openedWith(t *testing.T, n int) *App {
	t.Helper()
	a, _ := newTestApp(t, numbered(50))
	for i := 2; i <= n; i++ {
		addTestBuffer(t, a, fmt.Sprintf("f%02d.log", i), numbered(50))
	}
	a.showBuffer(0)
	return a
}

func TestPanelListsBuffersWithTheCurrentOneSelected(t *testing.T) {
	a := openedWith(t, 3)
	a.addRule(filter.Only, "ERROR", color.RGB{})
	a.showBuffer(1)

	keys(a, "B")
	if a.ov == nil || a.ov.kind != ovBuffers {
		t.Fatal("B did not open the buffer panel")
	}
	if a.ov.selected() != 1 {
		t.Errorf("selected %d, want the current buffer 1", a.ov.selected())
	}
	var text strings.Builder
	for _, r := range a.ov.rows {
		text.WriteString(r.text + "\n")
	}
	for _, want := range []string{"test.log", "f02.log", "f03.log", "1 rule", "unsaved rules"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("panel lacks %q:\n%s", want, text.String())
		}
	}
}

func TestPanelMovesAndSelects(t *testing.T) {
	a := openedWith(t, 3)
	keys(a, "Bjj")
	press(a, tcell.KeyEnter)
	if a.ov != nil || a.bufferIndex() != 2 {
		t.Errorf("Enter: panel open %v, buffer %d, want closed on 2", a.ov != nil, a.bufferIndex())
	}
	if !strings.Contains(a.msg, "[3/3]") {
		t.Errorf("choosing from the panel did not show the file: %q", a.msg)
	}
}

func TestPanelDigitSwitchesAtOnceWhenUnambiguous(t *testing.T) {
	a := openedWith(t, 3)
	keys(a, "B2")
	if a.ov != nil || a.bufferIndex() != 1 {
		t.Errorf("2 with three buffers: panel open %v, buffer %d", a.ov != nil, a.bufferIndex())
	}
}

func TestPanelDigitWaitsWhenALongerNumberCouldFollow(t *testing.T) {
	a := openedWith(t, 12)

	keys(a, "B1")
	if a.ov == nil || a.ov.selected() != 0 {
		t.Fatal("1 with twelve buffers should wait with buffer 1 selected")
	}
	keys(a, "2")
	if a.ov != nil || a.bufferIndex() != 11 {
		t.Errorf("12: buffer %d, want 11", a.bufferIndex())
	}

	keys(a, "B1")
	press(a, tcell.KeyEnter)
	if a.bufferIndex() != 0 {
		t.Errorf("1 then Enter went to %d, want 0", a.bufferIndex())
	}
}

func TestPanelRejectsANumberWithNoBuffer(t *testing.T) {
	a := openedWith(t, 3)
	keys(a, "B0")
	if a.ov == nil || !strings.Contains(a.msg, "no buffer") {
		t.Errorf("0: panel open %v, message %q", a.ov != nil, a.msg)
	}
}

// TestPanelQDropsATypedNumberFirst: q backs out one step at a time, and
// never quits lv from inside the panel.
func TestPanelQDropsATypedNumberFirst(t *testing.T) {
	a := openedWith(t, 12)
	a.showBuffer(4)

	keys(a, "B1q")
	if a.ov == nil || a.ov.typed != 0 {
		t.Fatalf("q with a typed number: panel open %v, typed %d", a.ov != nil, a.ov.typed)
	}
	keys(a, "q")
	if a.ov != nil || a.quit || a.bufferIndex() != 4 {
		t.Errorf("second q: panel %v, quit %v, buffer %d, want closed on 4", a.ov != nil, a.quit, a.bufferIndex())
	}
}
