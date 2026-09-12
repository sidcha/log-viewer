package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

// choice is one answer to a confirmation.
type choice struct {
	key   rune
	label string
	run   func()
}

// confirm is a single-key question on the status row.
//
// It is deliberately not a free-text prompt: the answers are a short fixed set,
// and asking someone to type "yes" to a question about losing work they can see
// on screen would be worse, not safer.
type confirm struct {
	text    string
	choices []choice
	cancel  func()
}

// ask puts a question up. Escape and C-g always cancel, whether or not a cancel
// choice is listed.
func (a *App) ask(text string, cancel func(), choices ...choice) {
	a.clearMsg()
	a.confirm = &confirm{text: text, choices: choices, cancel: cancel}
}

func (a *App) closeConfirm() { a.confirm = nil }

// confirmKey handles a key while a question is up, reporting whether it was
// consumed. Anything unrecognised is swallowed rather than acted on, so a
// stray keystroke cannot answer a question about discarding work.
func (a *App) confirmKey(ev *tcell.EventKey) bool {
	c := a.confirm

	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlG:
		a.closeConfirm()
		if c.cancel != nil {
			c.cancel()
		}
		return true
	case tcell.KeyRune:
		for _, ch := range c.choices {
			if matchesChoice(ev.Rune(), ch.key) {
				a.closeConfirm()
				if ch.run != nil {
					ch.run()
				}
				return true
			}
		}
	}
	return true
}

// matchesChoice accepts either case, so a caps-lock accident still answers.
func matchesChoice(got, want rune) bool {
	return got == want || lowerRune(got) == lowerRune(want)
}

func lowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

func (a *App) drawConfirm(row, w int) {
	c := a.confirm
	st := a.cfg.Style("prompt")

	var answers strings.Builder
	for _, ch := range c.choices {
		answers.WriteString("   (")
		answers.WriteRune(ch.key)
		answers.WriteString(") ")
		answers.WriteString(ch.label)
	}

	// The answers are the part that has to be readable: a question with its
	// choices cut off the right edge is unanswerable. A long file name in the
	// question gives way to them instead.
	text := c.text
	if room := w - 1 - len([]rune(answers.String())); room < len([]rune(text)) {
		text = ellipsis(text, room)
	}

	a.fillRow(row, " "+text+answers.String(), st)
	a.scr.HideCursor()
}

// ellipsis shortens s to at most n columns, marking that it was cut.
func ellipsis(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
