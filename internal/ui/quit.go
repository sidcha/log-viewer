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
// Both questions can be turned off, since a workflow that pipes a log through
// lv to glance at it wants neither.

// requestQuit is the quit command.
func (a *App) requestQuit() {
	a.askSaveRules(func() {
		a.askSaveStream(func() { a.quit = true })
	})
}

// askSaveRules runs next once the rules are dealt with, and not at all if the
// answer is to stay.
func (a *App) askSaveRules(next func()) {
	if !a.rulesDirty() || !a.cfg.Bool("ask-save-rules") {
		next()
		return
	}

	if a.rulesPath != "" {
		a.ask("Rules changed. Update "+filepath.Base(a.rulesPath)+"?",
			nil,
			choice{'y', "update", func() {
				if a.saveRules(a.rulesPath) {
					next()
				}
			}},
			choice{'n', "discard", next},
			choice{'a', "save as…", func() { a.promptSaveRules(next) }},
			choice{'c', "cancel", nil},
		)
		return
	}

	a.ask("Save rules before quitting?",
		nil,
		choice{'y', "save as…", func() { a.promptSaveRules(next) }},
		choice{'n', "discard", next},
		choice{'c', "cancel", nil},
	)
}

// streamAtRisk reports whether quitting now would throw away input that exists
// nowhere but in this process.
func (a *App) streamAtRisk() bool {
	return a.src.IsStdin() && !a.streamSaved && a.base.Len() > 0
}

func (a *App) askSaveStream(next func()) {
	if !a.streamAtRisk() || !a.cfg.Bool("ask-save") {
		next()
		return
	}

	a.ask("Piped input is not on disk. Keep it?",
		nil,
		choice{'y', "save all…", func() { a.promptSaveStream(next) }},
		choice{'n', "discard", next},
		choice{'c', "cancel", nil},
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
	a.prompt.buf = []rune(captureName(time.Now()))
	a.prompt.cur = len(a.prompt.buf)
}

// captureName is a suggestion to type over, so answering the question does not
// also mean inventing a file name.
func captureName(now time.Time) string {
	return now.Format("lv-20060102-150405.log")
}
