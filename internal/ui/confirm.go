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

	var b strings.Builder
	b.WriteString(" ")
	b.WriteString(c.text)
	for _, ch := range c.choices {
		b.WriteString("   (")
		b.WriteRune(ch.key)
		b.WriteString(") ")
		b.WriteString(ch.label)
	}
	a.fillRow(row, b.String(), st)
	a.scr.HideCursor()
}
