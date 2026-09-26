package ui

import (
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/ruleset"
)

// Rules built up during a session are worth keeping, so lv will not throw them
// away without asking. The question is only raised when there is something to
// lose and it has changed since it was last written, which keeps it out of the
// way of the common case of opening a log, looking, and quitting.

// currentRuleFile captures what would be written right now.
func (b *buffer) currentRuleFile() *ruleset.File {
	return ruleset.From(b.rules, b.rulesCase)
}

// rulesDirty reports whether there is anything worth saving that is not already
// on disk.
func (a *App) rulesDirty() bool { return a.hasUnsavedRules() }

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
	set, cs, err := f.Set(a.opts().Case)
	if err != nil {
		return err
	}
	if err := a.cfg.Set("case", cs.String()); err != nil {
		return err
	}

	a.applyRules(set, false)
	a.rulesCase = cs
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
		a.prompt.set(a.rulesPath)
	}
}

// A rule is edited in place, keeping its position, so the list does not
// reorder under the user mid-edit.

// replaceRuleMode gives rule i a new pattern.
func (a *App) replaceRuleMode(i int, pattern string, mode filter.Mode) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	m, err := filter.Compile(pattern, mode, a.opts().Case)
	if err != nil {
		a.errorf("bad pattern: %v", err)
		return
	}
	a.applyRules(a.rules.Replace(i, rules[i].WithMatcher(m)), true)
	a.infof("rule %d is now %s", i+1, pattern)

	if a.ov != nil && a.ov.kind == ovRules {
		a.showRules()
	}
}

// cycleRuleAction moves a rule between only, never and highlight in place,
// which is usually what you want after seeing what it actually caught.
func (a *App) cycleRuleAction(i int) {
	r := a.rules.All()[i]
	a.updateRuleList(a.rules.Replace(i, r.WithAction(r.Action.Next())))
}

// recolourRule assigns a fresh colour, for when two rules landed too close
// together to tell apart.
func (a *App) recolourRule(i int) {
	rules := a.rules.All()

	// Exclude this rule's own colour from the set being avoided, or it would
	// be treated as one to stay away from and the choice would be needlessly
	// constrained.
	used := make([]color.RGB, 0, len(rules))
	for j, r := range rules {
		if j != i {
			used = append(used, r.Color)
		}
	}
	a.updateRuleList(a.rules.Replace(i, rules[i].WithColor(filter.NextColor(used))))
}
