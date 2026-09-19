package ui

// Whatever is in front of the user owns the keyboard: a question, the match
// window, the prompt, a panel, a selection, or the log. Each has a key table
// of its own (config.Tables), so every key in every mode is bound by name and
// can be rebound, and a mode's commands are handlers like any other.

// keyTable names the key table for what currently has the keyboard.
func (a *App) keyTable() string {
	switch {
	case a.confirm != nil:
		return "confirm"
	case a.find != nil:
		return "finder"
	case a.prompt != nil:
		return "prompt"
	case a.ov != nil && a.ov.kind == ovRules:
		return "rules"
	case a.ov != nil:
		return "overlay"
	case a.visualActive():
		return "visual"
	default:
		return "normal"
	}
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
