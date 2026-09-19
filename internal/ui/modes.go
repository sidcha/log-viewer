package ui

// Whatever is in front of the user owns the keyboard: a question, the match
// window, the prompt, a panel, a selection, or the log. Each has a key table
// of its own (config.Tables), so every key in every mode is bound by name and
// can be rebound, and a mode's commands are handlers like any other.

// mode is something that has taken the keyboard from the log for a while: a
// question, the match window, the prompt or a panel. Everything the rest of
// the viewer needs to know about one is here, so adding a mode is writing
// these methods rather than threading a new case through the key handling,
// the layout, the drawing and the cursor.
type mode interface {
	// keyTable names the bindings in force while the mode is up.
	keyTable() string
	// screenRows is how many rows the mode takes from the bottom of a screen h
	// rows tall; the log gets the rest. A panel floats over the log and
	// takes none.
	screenRows(a *App, h int) int
	// draw paints the mode, starting at row top of a screen w by h, and puts
	// the terminal cursor where it belongs.
	draw(a *App, top, w, h int)
}

// mode is what has the keyboard, or nil when the log itself does. Should two
// be up at once, the one that most needs an answer wins.
func (a *App) mode() mode {
	switch {
	case a.confirm != nil:
		return a.confirm
	case a.find != nil:
		return a.find
	case a.prompt != nil:
		return a.prompt
	case a.ov != nil:
		return a.ov
	}
	return nil
}

// keyTable names the key table for what currently has the keyboard.
func (a *App) keyTable() string {
	switch {
	case a.mode() != nil:
		return a.mode().keyTable()
	case a.visualActive():
		return "visual"
	default:
		return "normal"
	}
}

func (c *confirm) keyTable() string             { return "confirm" }
func (c *confirm) screenRows(a *App, h int) int { return 1 }
func (c *confirm) draw(a *App, top, w, h int)   { a.drawConfirm(top, w) }

func (f *finder) keyTable() string             { return "finder" }
func (f *finder) screenRows(a *App, h int) int { return a.finderHeight(h) }
func (f *finder) draw(a *App, top, w, h int)   { a.drawFinder(top, w, h-top) }

func (p *prompt) keyTable() string             { return "prompt" }
func (p *prompt) screenRows(a *App, h int) int { return 1 }
func (p *prompt) draw(a *App, top, w, h int)   { a.drawPrompt(top, w) }

func (o *overlay) keyTable() string {
	if o.kind == ovRules {
		return "rules"
	}
	return "overlay"
}
func (o *overlay) screenRows(a *App, h int) int { return 0 }
func (o *overlay) draw(a *App, top, w, h int) {
	o.drawPanel(a, w, h)
	a.scr.HideCursor()
}

// editing is the line editor that has the keyboard, if one does, and what
// has to happen when its text changes: the match window searches again, and
// the prompt forgets its completions.
func (a *App) editing() (*lineEdit, func()) {
	switch {
	case a.find != nil:
		return &a.find.lineEdit, a.restartFinder
	case a.prompt != nil:
		p := a.prompt
		return &p.lineEdit, func() { p.comps = nil }
	}
	return nil, nil
}

// selfInsert types a character no binding claimed into the line being
// edited. Anywhere else an unbound key does nothing.
func (a *App) selfInsert(r rune) {
	if e, changed := a.editing(); e != nil && e.insert(r) {
		changed()
	}
}

// editOp adapts a line-editor operation into a handler.
func editOp(op func(*lineEdit) bool) handler {
	return func(a *App, c call) {
		if e, changed := a.editing(); e != nil && op(e) {
			changed()
		}
	}
}

func onFinder(fn func(a *App, f *finder, c call)) handler {
	return func(a *App, c call) {
		if a.find != nil {
			fn(a, a.find, c)
		}
	}
}

func onPanel(fn func(a *App, o *overlay, c call)) handler {
	return func(a *App, c call) {
		if a.ov != nil {
			fn(a, a.ov, c)
		}
	}
}

// onRule runs fn on the rule the rule list's selection is on.
func onRule(fn func(a *App, i int)) handler {
	return func(a *App, c call) {
		if i := a.selectedRule(); i >= 0 {
			fn(a, i)
		}
	}
}

func onRuleList(fn func(a *App)) handler {
	return func(a *App, c call) {
		if a.ov != nil && a.ov.kind == ovRules {
			fn(a)
		}
	}
}

func onConfirm(answer string) handler {
	return func(a *App, c call) {
		if a.confirm != nil {
			a.answer(answer)
		}
	}
}

// modeHandlers implements the commands of every mode but the log's own.
func modeHandlers() map[string]handler {
	return map[string]handler{
		// ------------------------------------------------------------ line
		"edit-left":        editOp((*lineEdit).left),
		"edit-right":       editOp((*lineEdit).right),
		"edit-start":       editOp((*lineEdit).home),
		"edit-end":         editOp((*lineEdit).toEnd),
		"edit-backspace":   editOp((*lineEdit).backspace),
		"edit-delete":      editOp((*lineEdit).del),
		"edit-clear":       editOp((*lineEdit).clear),
		"edit-kill-end":    editOp((*lineEdit).killEnd),
		"edit-delete-word": editOp((*lineEdit).deleteWord),
		"history-prev":     editOp(func(e *lineEdit) bool { return e.history(-1) }),
		"history-next":     editOp(func(e *lineEdit) bool { return e.history(1) }),
		"edit-accept": func(a *App, c call) {
			switch {
			case a.find != nil:
				a.commitFinder()
			case a.prompt != nil:
				a.acceptLine()
			}
		},
		"edit-cancel": func(a *App, c call) {
			switch {
			case a.find != nil:
				a.closeFinder()
			case a.prompt != nil:
				a.closePrompt()
			}
		},

		// ---------------------------------------------------------- prompt
		"prompt-complete": func(a *App, c call) {
			if p := a.prompt; p != nil && p.kind == pCommand {
				a.completeCommand(p)
			}
		},

		// ---------------------------------------------------------- finder
		"finder-select-prev": onFinder(func(a *App, f *finder, c call) { a.moveSel(-int(c.n)) }),
		"finder-select-next": onFinder(func(a *App, f *finder, c call) { a.moveSel(int(c.n)) }),
		"finder-page-up":     onFinder(func(a *App, f *finder, c call) { a.moveSel(-f.height) }),
		"finder-page-down":   onFinder(func(a *App, f *finder, c call) { a.moveSel(f.height) }),
		"finder-preview":     onFinder(func(a *App, f *finder, c call) { a.previewMatch() }),
		"finder-toggle-regex": onFinder(func(a *App, f *finder, c call) {
			a.toggleRegex()
		}),

		// ----------------------------------------------------------- panel
		"panel-close":     onPanel(func(a *App, o *overlay, c call) { a.ov = nil }),
		"panel-up":        onPanel(func(a *App, o *overlay, c call) { o.moveSel(-int(c.n)) }),
		"panel-down":      onPanel(func(a *App, o *overlay, c call) { o.moveSel(int(c.n)) }),
		"panel-page-up":   onPanel(func(a *App, o *overlay, c call) { o.scroll(-o.page()) }),
		"panel-page-down": onPanel(func(a *App, o *overlay, c call) { o.scroll(o.page()) }),
		"panel-top":       onPanel(func(a *App, o *overlay, c call) { o.scroll(-len(o.rows)) }),
		"panel-bottom":    onPanel(func(a *App, o *overlay, c call) { o.scroll(len(o.rows)) }),
		"panel-scroll-up": onPanel(func(a *App, o *overlay, c call) { o.scroll(-int(c.n)) }),
		"panel-scroll-down": onPanel(func(a *App, o *overlay, c call) {
			o.scroll(int(c.n))
		}),

		// ----------------------------------------------------------- rules
		"rules-toggle": onRule(func(a *App, i int) { a.updateRuleList(a.rules.Toggle(i)) }),
		"rules-delete": onRule(func(a *App, i int) { a.updateRuleList(a.rules.Remove(i)) }),
		"rules-edit": onRule(func(a *App, i int) {
			a.ov = nil
			a.editRule(i)
		}),
		"rules-cycle-action": onRule(func(a *App, i int) { a.cycleRuleAction(i) }),
		"rules-recolour":     onRule(func(a *App, i int) { a.recolourRule(i) }),
		"rules-enable-all":   onRuleList(func(a *App) { a.updateRuleList(a.rules.SetAll(true)) }),
		"rules-disable-all":  onRuleList(func(a *App) { a.updateRuleList(a.rules.SetAll(false)) }),

		// --------------------------------------------------------- confirm
		"confirm-yes":    onConfirm(answerYes),
		"confirm-no":     onConfirm(answerNo),
		"confirm-other":  onConfirm(answerOther),
		"confirm-cancel": onConfirm(answerCancel),
	}
}
