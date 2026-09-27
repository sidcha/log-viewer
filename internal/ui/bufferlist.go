package ui

import (
	"fmt"
	"strings"
)

// showBufferList opens the list of open files with the one on screen
// selected.
func (a *App) showBufferList() {
	o := &overlay{
		kind:  ovBuffers,
		title: "buffers",
		hint:  a.panelHint("buffers", "go", "buffers-select", "close", "buffers-cancel"),
		sel:   a.bufferIndex(),
	}
	width, _ := a.scr.Size()
	for i, b := range a.bufs {
		var flags []string
		if b.follow {
			flags = append(flags, "follow")
		}
		if b.hasUnsavedRules() {
			flags = append(flags, "unsaved rules")
		}
		if b.src.IsStdin() && !b.streamSaved {
			flags = append(flags, "unsaved input")
		}
		tail := fmt.Sprintf("  %8s  %s", humanBytes(b.idx.Size()), ruleCount(b.rules.Len()))
		if len(flags) > 0 {
			tail += "  [" + strings.Join(flags, "] [") + "]"
		}
		head := fmt.Sprintf("  %2d  ", i+1)
		// Leave room for the panel's border and padding.
		room := max(width-8-len(head)-len(tail), 10)
		o.rows = append(o.rows, ovRow{
			text: head + shortenPathLeft(b.src.Name(), room) + tail,
			item: i,
		})
	}
	a.ov = o
}

func ruleCount(n int) string {
	switch n {
	case 0:
		return "no rules"
	case 1:
		return "1 rule"
	}
	return fmt.Sprintf("%d rules", n)
}

// bufferDigit is a digit typed in the buffer list. The number built so far
// is selected as it grows, and chosen at once when no further digit could
// name another buffer.
func (a *App) bufferDigit(d int) {
	o := a.ov
	n := o.typed*10 + d
	if n < 1 || n > len(a.bufs) {
		o.typed = 0
		a.errorf("no buffer %d", n)
		return
	}
	o.typed = n
	o.sel = n - 1
	if n*10 > len(a.bufs) {
		a.pickBuffer(n - 1)
	}
}

func (a *App) pickBuffer(i int) {
	a.ov = nil
	a.switchBuffer(i)
}
