package ui

import "unicode/utf8"

// Word motions walk the view a character at a time rather than a line at a
// time, so w, b and e carry on across line boundaries the way they do in vi.
// Stopping dead at the end of a line makes them useless for reading: in a log
// the interesting run of text is usually several lines, and a motion that has
// to be interleaved with j is no faster than pressing j.
//
// "Next line" means the next line in the *view*. With a filter on, the motions
// follow what is on screen rather than what is underneath it.
//
// h and l stay within a line, matching vi's default whichwrap.

// place is a position in the view: a line and a byte offset within it.
//
// col may equal len(text), which is the position just past the last character.
// That stands in for the line break, and counts as whitespace, which is what
// makes a word motion cross lines without any special case for it.
type place struct {
	line int64
	col  int
}

// walkLine reads a view line into a buffer of its own, so a motion can look at
// several lines without disturbing the one the renderer is using.
func (a *App) walkLine(line int64) []byte {
	if line < 0 || line >= a.viewLen() {
		return nil
	}
	ln, err := a.activeView().At(line, a.walkbuf[:0])
	if err != nil {
		return nil
	}
	a.walkbuf = ln.Text
	return ln.Text
}

// classAt is the character class at p, treating the end of a line as space.
func classAt(text []byte, col int) charClass {
	if col >= len(text) {
		return classSpace
	}
	r, size := utf8.DecodeRune(text[col:])
	if size == 0 {
		return classSpace
	}
	return classOf(r)
}

func runeLenAt(text []byte, col int) int {
	if col >= len(text) {
		return 1
	}
	_, size := utf8.DecodeRune(text[col:])
	if size <= 0 {
		return 1
	}
	return size
}

func prevRuneStart(text []byte, col int) int {
	if col <= 0 {
		return 0
	}
	if col > len(text) {
		col = len(text)
	}
	_, size := utf8.DecodeLastRune(text[:col])
	if size <= 0 {
		size = 1
	}
	return col - size
}

// lastColumn is the offset of the final character, where $ lands.
func lastColumn(text []byte) int {
	if len(text) == 0 {
		return 0
	}
	return prevRuneStart(text, len(text))
}

// forward moves one position on, stepping to the next line at the end of one.
// It reports false at the end of the view.
func (a *App) forward(p place, text []byte) (place, []byte, bool) {
	if p.col < len(text) {
		p.col += runeLenAt(text, p.col)
		if p.col <= len(text) {
			return p, text, true
		}
	}
	if p.line+1 >= a.viewLen() {
		return p, text, false
	}
	p = place{line: p.line + 1, col: 0}
	return p, a.walkLine(p.line), true
}

// backward moves one position back, stepping to the end of the previous line.
func (a *App) backward(p place, text []byte) (place, []byte, bool) {
	if p.col > 0 {
		p.col = prevRuneStart(text, p.col)
		return p, text, true
	}
	if p.line == 0 {
		return p, text, false
	}
	prev := a.walkLine(p.line - 1)
	return place{line: p.line - 1, col: len(prev)}, prev, true
}

// wordForward is w: to the start of the next word, crossing lines. An empty
// line is a stop, as it is in vi.
func (a *App) wordForward() {
	p := place{line: a.cursorLine(), col: a.curCol}
	text := a.walkLine(p.line)

	// Step off the current word.
	if cls := classAt(text, p.col); cls != classSpace {
		for classAt(text, p.col) == cls && p.col < len(text) {
			p.col += runeLenAt(text, p.col)
		}
	}

	for {
		if p.col >= len(text) {
			next, nextText, ok := a.forward(p, text)
			if !ok {
				a.land(p.line, lastColumn(text))
				return
			}
			p, text = next, nextText
			if len(text) == 0 {
				a.land(p.line, 0)
				return
			}
			continue
		}
		if classAt(text, p.col) != classSpace {
			a.land(p.line, p.col)
			return
		}
		p.col += runeLenAt(text, p.col)
	}
}

// wordEnd is e: to the last character of the next word, crossing lines. Empty
// lines are skipped, because there is no word on them to end.
func (a *App) wordEnd() {
	p := place{line: a.cursorLine(), col: a.curCol}
	text := a.walkLine(p.line)

	next, nextText, ok := a.forward(p, text)
	if !ok {
		a.land(p.line, lastColumn(text))
		return
	}
	p, text = next, nextText

	for classAt(text, p.col) == classSpace {
		next, nextText, ok := a.forward(p, text)
		if !ok {
			a.land(p.line, lastColumn(text))
			return
		}
		p, text = next, nextText
	}

	cls := classAt(text, p.col)
	for {
		n := p.col + runeLenAt(text, p.col)
		if n >= len(text) || classAt(text, n) != cls {
			break
		}
		p.col = n
	}
	a.land(p.line, p.col)
}

// wordBack is b: to the start of the previous word, crossing lines. An empty
// line is a stop.
func (a *App) wordBack() {
	p := place{line: a.cursorLine(), col: a.curCol}
	text := a.walkLine(p.line)

	prev, prevText, ok := a.backward(p, text)
	if !ok {
		a.land(p.line, 0)
		return
	}
	p, text = prev, prevText

	for {
		if len(text) == 0 {
			a.land(p.line, 0)
			return
		}
		if classAt(text, p.col) != classSpace {
			break
		}
		prev, prevText, ok := a.backward(p, text)
		if !ok {
			a.land(p.line, 0)
			return
		}
		p, text = prev, prevText
	}

	cls := classAt(text, p.col)
	for p.col > 0 {
		before := prevRuneStart(text, p.col)
		if classAt(text, before) != cls {
			break
		}
		p.col = before
	}
	a.land(p.line, p.col)
}

// land puts the cursor at a position, moving the window if the motion left the
// line it started on.
func (a *App) land(line int64, col int) {
	if line != a.cursorLine() {
		a.follow = false
		a.setCursor(line)
	}
	a.setColumn(col)
}
