package ui

import (
	"path/filepath"
	"time"
)

// Quitting asks about anything that would be lost and nothing that would not.
//
// Two things can be at risk. Rules are work: a set of filters over an
// unfamiliar log takes real effort to arrive at. Piped input is worse, because
// it was never anywhere else -- lv spools it to a temporary file that is
// unlinked as soon as it is opened, so when the process ends the capture ends
// with it, and whatever produced it has long since moved on.
//
// The rules question is on by default and the input question off: lv is a
// pager first, and asking on the way out of every pipe costs every session to
// protect the rare capture worth keeping. ask-save turns it on.

// requestQuit is the quit command.
func (a *App) requestQuit() {
	a.askSaveEachBuffer(0, func() {
		a.askSaveStream(func() { a.quit = true })
	})
}

// askSaveEachBuffer asks about the rules of buffer i and every one after it,
// putting each on screen first so it is plain whose rules are in question.
func (a *App) askSaveEachBuffer(i int, next func()) {
	for ; i < len(a.bufs); i++ {
		if a.bufs[i].hasUnsavedRules() {
			break
		}
	}
	if i == len(a.bufs) {
		next()
		return
	}
	a.showBuffer(i)
	a.askSaveRules(func() { a.askSaveEachBuffer(i+1, next) })
}

// whose names the file a question is about, when there is more than one to
// tell apart.
func (a *App) whose() string {
	if len(a.bufs) < 2 {
		return ""
	}
	return filepath.Base(a.src.Name()) + ": "
}

// askSaveRules runs next once the rules are dealt with, and not at all if the
// answer is to stay.
func (a *App) askSaveRules(next func()) {
	if !a.rulesDirty() || !a.opts().AskSaveRules {
		next()
		return
	}

	if a.rulesPath != "" {
		a.ask(a.whose()+"Rules changed. Update "+filepath.Base(a.rulesPath)+"?",
			nil,
			choice{answerYes, "update", func() {
				if a.saveRules(a.rulesPath) {
					next()
				}
			}},
			choice{answerNo, "discard", next},
			choice{answerOther, "save as…", func() { a.promptSaveRules(next) }},
			choice{answerCancel, "cancel", nil},
		)
		return
	}

	a.ask(a.whose()+"Save rules before quitting?",
		nil,
		choice{answerYes, "save as…", func() { a.promptSaveRules(next) }},
		choice{answerNo, "discard", next},
		choice{answerCancel, "cancel", nil},
	)
}

// streamAtRisk reports whether quitting now would throw away input that exists
// nowhere but in this process.
func (a *App) streamAtRisk() bool {
	return a.src.IsStdin() && !a.streamSaved && a.base.Len() > 0
}

// askAboutStream decides whether losing the input is worth a question.
//
// On auto, the answer is whether lv was ever following it. Following a pipe is
// how you watch something happen -- a boot, a build, a device talking -- and
// that is a capture: it exists nowhere else and cannot be produced again by
// asking nicely. Paging the output of a command is not, and a pager that
// stopped to ask on the way out of `git log` would be intolerable.
func (a *App) askAboutStream() bool {
	switch a.opts().AskSave {
	case "on":
		return true
	case "off":
		return false
	default:
		return a.everFollowed
	}
}

func (a *App) askSaveStream(next func()) {
	for i, b := range a.bufs {
		if b.src.IsStdin() {
			a.showBuffer(i)
		}
	}
	if !a.streamAtRisk() || !a.askAboutStream() {
		next()
		return
	}

	a.ask(a.whose()+"Piped input is not on disk. Keep it?",
		nil,
		choice{answerYes, "save all…", func() { a.promptSaveStream(next) }},
		choice{answerNo, "discard", next},
		choice{answerCancel, "cancel", nil},
	)
}

// promptSaveStream asks where to put the capture, then writes all of it.
//
// All of it, rather than the filtered view `:w` writes: the question was about
// losing the input, and answering it with a subset of the input would be a
// trap. The filters can be applied again to the file afterwards.
func (a *App) promptSaveStream(after func()) {
	a.openPrompt(pSaveStream, "save all input to: ", &a.histPath)
	a.prompt.then = after
	a.prompt.set(captureName(time.Now()))
}

// captureName is a suggestion to type over, so answering the question does not
// also mean inventing a file name.
func captureName(now time.Time) string {
	return now.Format("lv-20060102-150405.log")
}
