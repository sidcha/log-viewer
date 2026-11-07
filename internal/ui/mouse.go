package ui

import (
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
)

// The mouse works as it does in a text editor, but nothing about it is wired
// in: a press, a drag or a release becomes a key, like the wheel does, and the
// key table that has the keyboard decides what it means. The commands those
// keys run read where the pointer is from here.

// edgeScrollInterval is how often a drag held against an edge of the screen
// scrolls the log one more step.
const edgeScrollInterval = 50 * time.Millisecond

// pointer is the mouse as the last event left it.
type pointer struct {
	x, y int
	held tcell.ButtonMask
	// key is the last button key dispatched, which an edge scroll repeats.
	key config.Key
	// clicks counts presses in quick succession on one cell, for double and
	// triple clicks.
	clicks         int
	clickAt        time.Time
	clickX, clickY int
	edgeQueued     bool
}

const mouseButtons = tcell.Button1 | tcell.Button2 | tcell.Button3

// mouse turns a mouse event into a key, so what it does is a binding in
// whichever table has the keyboard.
func (a *App) mouse(ev *tcell.EventMouse) {
	if !a.opts().Mouse {
		return
	}
	switch {
	case ev.Buttons()&tcell.WheelUp != 0:
		a.dispatch(a.keyTable(), config.Key{Key: config.KeyWheelUp})
		return
	case ev.Buttons()&tcell.WheelDown != 0:
		a.dispatch(a.keyTable(), config.Key{Key: config.KeyWheelDown})
		return
	}

	x, y := ev.Position()
	held := ev.Buttons() & mouseButtons
	was := a.ptr.held
	moved := x != a.ptr.x || y != a.ptr.y
	a.ptr.x, a.ptr.y, a.ptr.held = x, y, held

	var k tcell.Key
	switch {
	case held&tcell.Button1 != 0 && was&tcell.Button1 == 0:
		k = a.ptr.leftPress(time.Now(), a.opts().DoubleClickTime)
	case held&tcell.Button1 != 0:
		if !moved {
			return // the terminal reported motion within one cell
		}
		k = config.KeyLeftDrag
	case was&tcell.Button1 != 0:
		k = config.KeyLeftRelease
	case held&tcell.Button3 != 0 && was&tcell.Button3 == 0:
		k = config.KeyMiddleMouse
	case held&tcell.Button2 != 0 && was&tcell.Button2 == 0:
		k = config.KeyRightMouse
	default:
		return
	}

	// A click is not the next key of a half-typed sequence: g then a click
	// should place the cursor, not fail to match g<LeftMouse>.
	a.pending, a.count = a.pending[:0], 0
	a.ptr.key = config.Key{Key: k, Mod: ev.Modifiers()}
	a.dispatch(a.keyTable(), a.ptr.key)
}

// leftPress names a press of the left button by how many came before it in
// quick succession on the same cell.
func (p *pointer) leftPress(now time.Time, interval time.Duration) tcell.Key {
	if p.clicks < 3 && p.x == p.clickX && p.y == p.clickY && now.Sub(p.clickAt) <= interval {
		p.clicks++
	} else {
		p.clicks = 1
	}
	p.clickAt, p.clickX, p.clickY = now, p.x, p.y
	switch p.clicks {
	case 2:
		return config.KeyLeftMouse2
	case 3:
		return config.KeyLeftMouse3
	}
	return config.KeyLeftMouse
}

// pointerInLog reports whether the pointer is over the log rather than the
// status line or a mode drawn at the bottom of the screen.
func (a *App) pointerInLog() bool {
	return a.ptr.y >= 0 && a.ptr.y < a.contentRows()
}

// pointerPosition is the view line and byte offset under the pointer, clamped
// to the log's rows.
func (a *App) pointerPosition() (int64, int) {
	return a.positionAt(a.ptr.x, a.ptr.y)
}

// positionAt maps a screen cell onto the view line and byte offset drawn
// there, laying lines out exactly as drawLine does so tabs, wide runes and
// wrapped rows land where they were painted. Below the last line is the last
// line.
func (a *App) positionAt(x, y int) (int64, int) {
	n := a.viewLen()
	if n == 0 {
		return 0, 0
	}
	rows := a.contentRows()
	y = max(0, min(y, rows-1))
	width := max(a.contentWidth(), 1)
	cell := max(x-a.gutterWidth(), 0)

	line := a.top
	if !a.opts().Wrap {
		line += int64(y)
		cell += a.hoff
	} else {
		for row := 0; ; line++ {
			if line >= n {
				cell = maxHOff
				break
			}
			a.lay.build(a.lineText(line), width*(rows-row))
			used := max((a.lay.cols()+width-1)/width, 1)
			if y < row+used {
				cell += (y - row) * width
				break
			}
			row += used
		}
	}
	if line >= n {
		line = n - 1
	}
	return line, a.byteAtCell(a.lineText(line), cell)
}

// byteAtCell is the offset of the character drawn in a cell of a laid-out
// line, or the line's length when the cell is past its end.
func (a *App) byteAtCell(text []byte, cell int) int {
	a.lay.build(text, cell+1)
	if cell < len(a.lay.cbyte) {
		return int(a.lay.cbyte[cell])
	}
	return len(text)
}

// placeCursorAt moves the cursor to a position the pointer chose. The window
// stays where it is: a click is on screen already, and scroll-off pulling the
// text out from under the pointer would make the next click miss.
func (a *App) placeCursorAt(line int64, col int) {
	a.follow = false
	a.cur = line
	a.curCol = col
	a.clampColumn()
	a.wantCol = a.curCol
}

func (a *App) mouseCursor() {
	if !a.pointerInLog() {
		return
	}
	a.endVisual()
	a.placeCursorAt(a.pointerPosition())
}

// mouseSelect extends the selection to the pointer, starting one at the
// cursor when there is none. Held against an edge of the log it scrolls that
// way first, and keeps doing so until the button is let go or the pointer
// leaves the edge.
func (a *App) mouseSelect() {
	if a.viewLen() == 0 {
		return
	}
	if !a.visualActive() {
		a.sel = selection{mode: visualChar, line: a.cursorLine(), col: a.curCol}
	}
	dy, dx := a.edgePush()
	a.top += int64(dy)
	a.clampTop()
	a.hoff = max(0, min(a.hoff+dx, maxHOff))
	a.placeCursorAt(a.pointerPosition())
	if dy != 0 || dx != 0 {
		a.queueEdgeScroll()
	}
}

// edgePush is the direction a drag at the pointer's position scrolls the
// window: from the top or bottom row, or past the bottom onto the status
// line, it scrolls by a line; from the gutter or either side column, by a
// column. It is zero where the window cannot move any further that way.
func (a *App) edgePush() (dy, dx int) {
	rows := a.contentRows()
	switch {
	case a.ptr.y <= 0 && a.top > 0:
		dy = -1
	case a.ptr.y >= rows-1 && a.top < a.lastTop():
		dy = 1
	}
	if a.opts().Wrap {
		return dy, 0
	}
	gw, width := a.gutterWidth(), a.contentWidth()
	switch {
	case a.ptr.x <= max(gw-1, 0) && a.hoff > 0:
		dx = -1
	case a.ptr.x >= gw+width-1 && width > 0:
		line, _ := a.pointerPosition()
		if DisplayWidth(a.lineText(line), a.opts().TabWidth) > a.hoff+width {
			dx = 1
		}
	}
	return dy, dx
}

// queueEdgeScroll repeats the drag after a pause, because a pointer resting
// against an edge sends no events of its own.
func (a *App) queueEdgeScroll() {
	if a.ptr.edgeQueued {
		return
	}
	a.ptr.edgeQueued = true
	time.AfterFunc(edgeScrollInterval, func() {
		a.postFn(func() {
			a.ptr.edgeQueued = false
			if a.ptr.held&tcell.Button1 != 0 && a.opts().Mouse {
				a.dispatch(a.keyTable(), a.ptr.key)
			}
		})
	})
}

// mouseSelectWord selects the run of word characters, punctuation or blanks
// under the pointer, as a double click does in an editor.
func (a *App) mouseSelectWord() {
	if !a.pointerInLog() || a.viewLen() == 0 {
		return
	}
	line, col := a.pointerPosition()
	text := a.lineText(line)
	if len(text) == 0 {
		a.mouseCursor()
		return
	}
	col = min(col, lastColumn(text))
	start, end := classRunAround(text, col)
	a.sel = selection{mode: visualChar, line: line, col: start}
	a.placeCursorAt(line, end)
}

// classRunAround is the run of characters of one class around byte i, as the
// offsets of its first and last characters.
func classRunAround(text []byte, i int) (int, int) {
	r, _ := runeAt(text, i)
	class := classOf(r)
	start := i
	for start > 0 {
		r, size := utf8.DecodeLastRune(text[:start])
		if size <= 0 || classOf(r) != class {
			break
		}
		start -= size
	}
	last := i
	for {
		_, size := runeAt(text, last)
		r, next := runeAt(text, last+size)
		if next == 0 || classOf(r) != class {
			break
		}
		last += size
	}
	return start, last
}

func (a *App) mouseSelectLine() {
	if !a.pointerInLog() || a.viewLen() == 0 {
		return
	}
	line, col := a.pointerPosition()
	a.sel = selection{mode: visualLine, line: line}
	a.placeCursorAt(line, col)
}
