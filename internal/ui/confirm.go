package ui

import (
	"strings"
)

// choice is one answer to a confirmation. answer is the command that gives
// it, so the key is whatever the confirm table binds to that command.
type choice struct {
	answer string
	label  string
	run    func()
}

// The answers a question can offer.
const (
	answerYes    = "confirm-yes"
	answerNo     = "confirm-no"
	answerOther  = "confirm-other"
	answerCancel = "confirm-cancel"
)

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

// ask puts a question up. confirm-cancel always cancels, whether or not a
// cancel choice is listed.
func (a *App) ask(text string, cancel func(), choices ...choice) {
	a.clearMsg()
	a.confirm = &confirm{text: text, choices: choices, cancel: cancel}
}

func (a *App) closeConfirm() { a.confirm = nil }

// answer settles the question with the choice given by an answer command.
// Cancelling always works, whether or not a cancel choice is listed; any
// other answer the question does not offer is ignored, so a stray keystroke
// cannot settle a question about discarding work.
func (a *App) answer(answer string) {
	c := a.confirm
	for _, ch := range c.choices {
		if ch.answer == answer {
			a.closeConfirm()
			if ch.run != nil {
				ch.run()
			}
			return
		}
	}
	if answer == answerCancel {
		a.closeConfirm()
		if c.cancel != nil {
			c.cancel()
		}
	}
}

// answerKey is the key shown for a choice: the first one bound to it.
func (a *App) answerKey(answer string) string {
	if keys := a.cfg.KeysFor("confirm", answer); len(keys) > 0 {
		return keys[0]
	}
	return "?"
}

func (a *App) drawConfirm(row, w int) {
	c := a.confirm
	st := a.cfg.Style("prompt")

	var answers strings.Builder
	for _, ch := range c.choices {
		answers.WriteString("   (")
		answers.WriteString(a.answerKey(ch.answer))
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
