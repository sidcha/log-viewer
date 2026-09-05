package ui

import (
	"path/filepath"

	"github.com/sidcha/log-viewer/internal/ruleset"
)

// Rules built up during a session are worth keeping, so lv will not throw them
// away without asking. The question is only raised when there is something to
// lose and it has changed since it was last written, which keeps it out of the
// way of the common case of opening a log, looking, and quitting.

// currentRuleFile captures what would be written right now.
func (a *App) currentRuleFile() *ruleset.File {
	return ruleset.From(a.rules, a.cfg.Case(), a.search.pat, a.search.back)
}

// rulesDirty reports whether there is anything worth saving that is not already
// on disk.
func (a *App) rulesDirty() bool {
	f := a.currentRuleFile()
	if f.Empty() {
		return false
	}
	return f.Digest() != a.savedDigest
}

// LoadRules reads a rules file and installs it.
func (a *App) LoadRules(path string) error {
	f, err := ruleset.Load(path)
	if err != nil {
		return err
	}
	return a.ApplyRuleFile(f, path)
}

// ApplyRuleFile installs an already-parsed rules file and remembers where it
// came from, so quitting can offer to update it in place.
//
// Parsing is separate so a bad path or a broken file is reported before the
// terminal is taken over, rather than flashing an alternate screen first.
func (a *App) ApplyRuleFile(f *ruleset.File, path string) error {
	if f == nil {
		return nil
	}
	set, cs, err := f.Set(a.cfg.Case())
	if err != nil {
		return err
	}
	if err := a.cfg.Set("case", cs.String()); err != nil {
		return err
	}

	a.applyRules(set, false)
	if f.Search != nil && f.Search.Pattern != "" {
		a.setSearchPattern(f.Search.Pattern, f.Search.Backward)
	}

	a.rulesPath = path
	a.savedDigest = a.currentRuleFile().Digest()
	return nil
}

// saveRules writes the current set to path and marks it as the saved state.
func (a *App) saveRules(path string) bool {
	f := a.currentRuleFile()
	if err := f.Save(path); err != nil {
		a.errorf("save rules: %v", err)
		return false
	}
	a.rulesPath = path
	a.savedDigest = f.Digest()
	a.infof("wrote %d rule(s) to %s", len(f.Rules), path)
	return true
}

// loadRulesAt replaces the current set from a file during a session.
func (a *App) loadRulesAt(path string) {
	if err := a.LoadRules(path); err != nil {
		a.errorf("%v", err)
		return
	}
	a.infof("loaded %d rule(s) from %s", a.rules.Len(), path)
}

// promptSaveRules asks for a path, then saves and runs after.
func (a *App) promptSaveRules(after func()) {
	a.openPrompt(pSaveRules, "save rules to: ", &a.histPath)
	a.prompt.then = after
	if a.rulesPath != "" {
		a.prompt.buf = []rune(a.rulesPath)
		a.prompt.cur = len(a.prompt.buf)
	}
}

// requestQuit is the quit command. It leaves without a word when there is
// nothing to lose, and otherwise asks once.
func (a *App) requestQuit() {
	if !a.rulesDirty() {
		a.quit = true
		return
	}

	if a.rulesPath != "" {
		a.ask("Rules changed. Update "+filepath.Base(a.rulesPath)+"?",
			nil,
			choice{'y', "update", func() {
				if a.saveRules(a.rulesPath) {
					a.quit = true
				}
			}},
			choice{'n', "discard", func() { a.quit = true }},
			choice{'a', "save as…", func() {
				a.promptSaveRules(func() { a.quit = true })
			}},
			choice{'c', "cancel", nil},
		)
		return
	}

	a.ask("Save rules before quitting?",
		nil,
		choice{'y', "save as…", func() {
			a.promptSaveRules(func() { a.quit = true })
		}},
		choice{'n', "discard", func() { a.quit = true }},
		choice{'c', "cancel", nil},
	)
}
