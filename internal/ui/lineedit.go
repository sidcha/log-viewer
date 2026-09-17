package ui

// lineEdit is the one-line text editor under both the bottom-row prompt and
// the finder's input: a buffer, a cursor in it, and a walk through what was
// typed before.
type lineEdit struct {
	buf []rune
	cur int

	hist  *[]string
	hidx  int    // index into history; len(*hist) means "the live buffer"
	stash []rune // the live buffer while browsing history
}

func newLineEdit(hist *[]string) lineEdit {
	e := lineEdit{hist: hist}
	if hist != nil {
		e.hidx = len(*hist)
	}
	return e
}

// set replaces the text and puts the cursor at its end.
func (e *lineEdit) set(s string) {
	e.buf = []rune(s)
	e.cur = len(e.buf)
}

func (e *lineEdit) text() string { return string(e.buf) }

// The editing operations report whether the text changed, which is what
// decides whether a finder has to search again.

func (e *lineEdit) insert(r rune) bool {
	e.buf = append(e.buf, 0)
	copy(e.buf[e.cur+1:], e.buf[e.cur:])
	e.buf[e.cur] = r
	e.cur++
	return true
}

func (e *lineEdit) left() bool {
	if e.cur > 0 {
		e.cur--
	}
	return false
}

func (e *lineEdit) right() bool {
	if e.cur < len(e.buf) {
		e.cur++
	}
	return false
}

func (e *lineEdit) home() bool  { e.cur = 0; return false }
func (e *lineEdit) toEnd() bool { e.cur = len(e.buf); return false }

func (e *lineEdit) backspace() bool {
	if e.cur == 0 {
		return false
	}
	e.buf = append(e.buf[:e.cur-1], e.buf[e.cur:]...)
	e.cur--
	return true
}

func (e *lineEdit) del() bool {
	if e.cur >= len(e.buf) {
		return false
	}
	e.buf = append(e.buf[:e.cur], e.buf[e.cur+1:]...)
	return true
}

func (e *lineEdit) clear() bool {
	e.buf, e.cur = e.buf[:0], 0
	return true
}

func (e *lineEdit) killEnd() bool {
	e.buf = e.buf[:e.cur]
	return true
}

func (e *lineEdit) deleteWord() bool {
	i := e.cur
	for i > 0 && e.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && e.buf[i-1] != ' ' {
		i--
	}
	e.buf = append(e.buf[:i], e.buf[e.cur:]...)
	e.cur = i
	return true
}

// history walks the entries typed earlier, keeping the live buffer aside so
// coming back down returns what was half written.
func (e *lineEdit) history(delta int) bool {
	if e.hist == nil || len(*e.hist) == 0 {
		return false
	}
	h := *e.hist
	if e.hidx == len(h) && delta < 0 {
		e.stash = append(e.stash[:0], e.buf...)
	}
	n := min(max(e.hidx+delta, 0), len(h))
	e.hidx = n
	if n == len(h) {
		e.buf = append(e.buf[:0], e.stash...)
	} else {
		e.buf = append(e.buf[:0], []rune(h[n])...)
	}
	e.cur = len(e.buf)
	return true
}
