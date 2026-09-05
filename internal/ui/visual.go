package ui

import (
	"strings"

	"github.com/sidcha/log-viewer/internal/clipboard"
	"github.com/sidcha/log-viewer/internal/view"
)

// visualMode is off, character-wise or line-wise, as in vim.
type visualMode int

const (
	visualOff visualMode = iota
	visualChar
	visualLine
)

// selection is the anchored half of a visual selection. The other end is
// wherever the cursor currently is, so every motion extends it for free.
type selection struct {
	mode visualMode
	line int64
	col  int
}

// visualActive reports whether a selection is being made.
func (a *App) visualActive() bool { return a.sel.mode != visualOff }

// startVisual begins, extends or cancels a selection. Pressing v while already
// in character-wise visual leaves visual mode, which is what vim does.
func (a *App) startVisual(mode visualMode) {
	if a.sel.mode == mode {
		a.sel.mode = visualOff
		return
	}
	a.sel = selection{mode: mode, line: a.cursorLine(), col: a.curCol}
}

func (a *App) endVisual() { a.sel.mode = visualOff }

// span returns the ordered bounds of the selection.
func (a *App) span() (startLine int64, startCol int64, endLine int64, endCol int64) {
	cl, cc := a.cursorLine(), int64(a.curCol)
	al, ac := a.sel.line, int64(a.sel.col)
	if al < cl || (al == cl && ac <= cc) {
		return al, ac, cl, cc
	}
	return cl, cc, al, ac
}

// selectedCells reports the byte range of line i that is selected, and whether
// any of it is.
func (a *App) selectedRange(i int64, length int) (int, int, bool) {
	if !a.visualActive() {
		return 0, 0, false
	}
	sl, sc, el, ec := a.span()
	if i < sl || i > el {
		return 0, 0, false
	}
	if a.sel.mode == visualLine {
		return 0, length, true
	}

	start, end := 0, length
	if i == sl {
		start = int(sc)
	}
	if i == el {
		// The cursor's own character is part of the selection in vim.
		end = int(ec) + 1
	}
	if start < 0 {
		start = 0
	}
	if end > length {
		end = length
	}
	if start >= end {
		return 0, 0, false
	}
	return start, end, true
}

// yank copies the selection, or the current line when nothing is selected.
//
// The text is gathered by iterating the view rather than by re-reading the
// file, so a yank out of a filtered view copies what is on screen rather than
// what is underneath it.
func (a *App) yank(count int64) {
	var (
		b     strings.Builder
		lines int64
	)

	if !a.visualActive() {
		n := count
		if n < 1 {
			n = 1
		}
		from := a.cursorLine()
		err := a.activeView().Iterate(from, func(i int64, ln view.Line) bool {
			b.Write(ln.Text)
			b.WriteByte('\n')
			lines++
			return lines < n
		})
		if err != nil {
			a.errorf("yank: %v", err)
			return
		}
	} else {
		sl, _, el, _ := a.span()
		lineWise := a.sel.mode == visualLine
		err := a.activeView().Iterate(sl, func(i int64, ln view.Line) bool {
			if i > el {
				return false
			}
			if lineWise {
				b.Write(ln.Text)
			} else if s, e, ok := a.selectedRange(i, len(ln.Text)); ok {
				b.Write(ln.Text[s:e])
			}
			if lineWise || i != el {
				b.WriteByte('\n')
			}
			lines++
			return i < el
		})
		if err != nil {
			a.errorf("yank: %v", err)
			return
		}
		a.endVisual()
	}

	text := b.String()
	if err := clipboard.Copy(text, a.cfg.Str("clipboard-command")); err != nil {
		a.errorf("clipboard: %v", err)
		return
	}
	a.infof("yanked %s line(s), %s", commas(lines), humanBytes(int64(len(text))))
}
