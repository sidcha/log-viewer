package ui

import (
	"unicode"
	"unicode/utf8"
)

// The viewer has a cursor line, and motions move it rather than the viewport.
//
// This is the part of vim's model that carries the most muscle memory: j and k
// move the cursor and the window follows only when the cursor would leave it,
// while C-e and C-y move the window and drag the cursor. Scrolling the viewport
// on j would feel wrong immediately, and would leave nothing for visual mode or
// yank to be anchored on.

// cursorLine returns the view line the cursor is on, clamped to what exists.
func (a *App) cursorLine() int64 {
	n := a.viewLen()
	if n == 0 {
		return 0
	}
	if a.cur >= n {
		return n - 1
	}
	if a.cur < 0 {
		return 0
	}
	return a.cur
}

// scrolloff is how many lines of context are kept above and below the cursor.
func (a *App) scrolloff() int {
	off := a.cfg.Int("scroll-off")
	rows := a.contentRows()
	if max := (rows - 1) / 2; off > max {
		off = max
	}
	if off < 0 {
		off = 0
	}
	return off
}

// setCursor moves the cursor and scrolls the viewport the minimum needed to
// keep it on screen.
func (a *App) setCursor(line int64) {
	n := a.viewLen()
	if n == 0 {
		a.cur, a.top = 0, 0
		return
	}
	if line < 0 {
		line = 0
	}
	if line >= n {
		line = n - 1
	}
	a.cur = line
	a.followCursor()
}

func (a *App) followCursor() {
	rows := int64(a.contentRows())
	off := int64(a.scrolloff())

	if a.cur < a.top+off {
		a.top = a.cur - off
	}
	if a.cur > a.top+rows-1-off {
		a.top = a.cur - rows + 1 + off
	}
	a.clampTop()
}

// clampTop keeps the window inside the file. The limit is the last full
// screenful rather than the last line, so scroll-off cannot push the view past
// the end and leave blank rows under the final line.
func (a *App) clampTop() {
	max := a.lastTop()
	if a.top > max {
		a.top = max
	}
	if a.top < 0 {
		a.top = 0
	}
}

// moveCursor is a relative line motion.
//
// The column is restored from the last one explicitly asked for, as vim does:
// running down past a few short lines and back onto a long one should put the
// cursor where it was, not where the short lines clamped it.
func (a *App) moveCursor(delta int64) {
	a.follow = false
	a.setCursor(a.cursorLine() + delta)
	a.restoreColumn()
}

// restoreColumn puts the cursor back at the remembered column, as far as the
// current line allows.
func (a *App) restoreColumn() {
	a.curCol = a.wantCol
	a.clampColumn()
	a.followColumn()
}

// setColumn moves to a column and remembers it as the one wanted.
func (a *App) setColumn(byteOff int) {
	a.curCol = byteOff
	a.clampColumn()
	a.wantCol = a.curCol
	a.followColumn()
}

// followColumn scrolls sideways the minimum needed to keep the cursor visible.
func (a *App) followColumn() {
	if a.wrap {
		a.hoff = 0
		return
	}
	width := a.contentWidth()
	if width < 1 {
		return
	}
	col := displayColumn(a.cursorText(), a.curCol, a.cfg.Int("tab-width"))
	if col < a.hoff {
		a.hoff = col
	}
	if col >= a.hoff+width {
		a.hoff = col - width + 1
	}
	if a.hoff < 0 {
		a.hoff = 0
	}
}

// scrollView moves the window, taking the cursor along only when it would
// otherwise fall outside. This is C-e and C-y.
func (a *App) scrollView(delta int64) {
	a.follow = false
	a.top += delta
	a.clampTop()

	rows := int64(a.contentRows())
	off := int64(a.scrolloff())
	switch {
	case a.cur < a.top+off:
		a.cur = a.top + off
	case a.cur > a.top+rows-1-off:
		a.cur = a.top + rows - 1 - off
	}

	n := a.viewLen()
	if a.cur >= n {
		a.cur = n - 1
	}
	if a.cur < 0 {
		a.cur = 0
	}
}

// pageCursor moves by whole or half screens, as C-f, C-b, C-d and C-u do:
// cursor and window travel together.
func (a *App) pageCursor(delta int64) {
	a.follow = false
	a.top += delta
	a.clampTop()
	a.setCursor(a.cursorLine() + delta)
}

// cursorToScreen places the cursor at a row of the current window, for H, M
// and L. off keeps the requested context at the top and bottom, as vim does.
func (a *App) cursorToScreen(where screenSpot, count int64) {
	rows := int64(a.contentRows())
	n := a.viewLen()
	last := a.top + rows - 1
	if last > n-1 {
		last = n - 1
	}

	off := int64(a.scrolloff())
	var line int64
	switch where {
	case spotTop:
		line = a.top + max64(off, count-1)
	case spotMiddle:
		line = (a.top + last) / 2
	default:
		line = last - max64(off, count-1)
	}
	a.follow = false
	a.setCursor(line)
	a.followCursor()
	a.restoreColumn()
}

type screenSpot int

const (
	spotTop screenSpot = iota
	spotMiddle
	spotBottom
)

// alignView redraws so the cursor sits at a chosen place in the window: zz, zt
// and zb.
func (a *App) alignView(where screenSpot) {
	rows := int64(a.contentRows())
	switch where {
	case spotTop:
		a.top = a.cur
	case spotMiddle:
		a.top = a.cur - rows/2
	default:
		a.top = a.cur - rows + 1
	}
	a.follow = false
	a.clampTop()
}

// ------------------------------------------------------------ column motions

// cursorText returns the text of the line the cursor is on.
func (a *App) cursorText() []byte {
	ln, err := a.activeView().At(a.cursorLine(), a.linebuf[:0])
	if err != nil {
		return nil
	}
	a.linebuf = ln.Text
	return ln.Text
}

// clampColumn keeps the column on a real character.
//
// The position just past the last character is not somewhere the cursor may
// rest: vi only allows it while inserting, and lv never inserts. Without this,
// l at the end of a line would park the cursor on empty space and $ and l would
// disagree about where the end is.
func (a *App) clampColumn() {
	last := lastColumn(a.cursorText())
	if a.curCol > last {
		a.curCol = last
	}
	if a.curCol < 0 {
		a.curCol = 0
	}
}

// moveColumn steps the cursor left or right by whole runes, so a multi-byte
// character is never split.
func (a *App) moveColumn(delta int) {
	text := a.cursorText()
	for ; delta > 0 && a.curCol < len(text); delta-- {
		_, size := utf8.DecodeRune(text[a.curCol:])
		if size <= 0 {
			size = 1
		}
		a.curCol += size
	}
	for ; delta < 0 && a.curCol > 0; delta++ {
		_, size := utf8.DecodeLastRune(text[:a.curCol])
		if size <= 0 {
			size = 1
		}
		a.curCol -= size
	}
	a.setColumn(a.curCol)
}

// columnEnd puts the cursor on the last character of the line, which is where
// $ goes.
func (a *App) columnEnd() {
	text := a.cursorText()
	col := len(text)
	if len(text) > 0 {
		_, size := utf8.DecodeLastRune(text)
		col = len(text) - size
	}
	a.setColumn(col)
}

// columnFirstNonBlank is ^.
func (a *App) columnFirstNonBlank() {
	text := a.cursorText()
	i := 0
	for i < len(text) {
		r, size := utf8.DecodeRune(text[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += size
	}
	a.setColumn(i)
}

// Word motions use vim's notion of a word: a run of letters, digits and
// underscores, or a run of other punctuation, with whitespace between.
type charClass int

const (
	classSpace charClass = iota
	classWord
	classPunct
)

func classOf(r rune) charClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return classWord
	default:
		return classPunct
	}
}

func runeAt(text []byte, i int) (rune, int) {
	if i < 0 || i >= len(text) {
		return 0, 0
	}
	return utf8.DecodeRune(text[i:])
}

// wordUnderCursor returns the word the cursor sits on, which is what * marks.
func (a *App) wordUnderCursor() string {
	text := a.cursorText()
	if len(text) == 0 {
		return ""
	}
	i := a.curCol
	if i >= len(text) {
		i = len(text) - 1
	}

	// Step forward to a word character if the cursor is not on one, which is
	// what vim does.
	for i < len(text) {
		r, size := runeAt(text, i)
		if size == 0 {
			break
		}
		if classOf(r) == classWord {
			break
		}
		i += size
	}
	if i >= len(text) {
		return ""
	}

	start := i
	for start > 0 {
		r, size := utf8.DecodeLastRune(text[:start])
		if size <= 0 || classOf(r) != classWord {
			break
		}
		start -= size
	}
	end := i
	for end < len(text) {
		r, size := runeAt(text, end)
		if size == 0 || classOf(r) != classWord {
			break
		}
		end += size
	}
	return string(text[start:end])
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
