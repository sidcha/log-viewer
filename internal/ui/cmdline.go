package ui

import (
	"strconv"
	"strings"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

// cmdAliases maps the short names vim users reach for onto lv's commands.
//
// The full names are the vocabulary; these exist so that :q, :w and :noh do
// what a decade of muscle memory expects rather than reporting an error.
var cmdAliases = map[string]string{
	"q":          "quit",
	"q!":         "quit-force",
	"quit!":      "quit-force",
	"qa":         "quit",
	"qa!":        "quit-force",
	"w":          "save-view",
	"write":      "save-view",
	"mks":        "rules-save",
	"mksession":  "rules-save",
	"rules":      "rules-save",
	"source":     "rules-load",
	"noh":        "search-clear",
	"nohl":       "search-clear",
	"nohlsearch": "search-clear",
	"only":       "filter-only",
	"never":      "filter-never",
	"hl":         "highlight",
	"filters":    "filter-list",
	"ls":         "filter-list",
	"e":          "reload",
	"edit":       "reload",
	"e!":         "reload",
	"h":          "help",
	"f":          "follow-toggle",
	"follow":     "follow-toggle",
	"n":          "search-next",
	"N":          "search-prev",
	"g":          "goto-line",
}

// runCommandLine executes one line typed at the `:` prompt.
//
// It accepts the lv command vocabulary, the vim aliases above, and a bare line
// number, which in vim jumps there.
func (a *App) runCommandLine(text string) {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), ":"))
	if text == "" {
		return
	}

	// :42 jumps to line 42.
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		a.gotoSourceLine(n)
		return
	}
	// :$ goes to the end, as it does in vim.
	if text == "$" {
		a.exec(config.Command{Name: "goto-end"}, 0)
		return
	}

	words, err := config.ParseLine(text)
	if err != nil {
		a.errorf("%v", err)
		return
	}
	if len(words) == 0 {
		return
	}
	if full, ok := cmdAliases[words[0]]; ok {
		words[0] = full
	}

	cmd, err := config.ParseCommand(words)
	if err != nil {
		a.errorf("%v", err)
		return
	}
	a.exec(cmd, 0)
}

// replaceRule swaps a rule's pattern, keeping its action, colour and position
// so the list does not reorder under the user mid-edit.
func (a *App) replaceRule(i int, pattern string) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	a.replaceRuleMode(i, pattern, rules[i].M.Mode())
}

// replaceRuleMode swaps a rule's pattern, keeping its action, colour and
// position so the list does not reorder under the user mid-edit.
func (a *App) replaceRuleMode(i int, pattern string, mode filter.Mode) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	old := rules[i]
	m, err := filter.Compile(pattern, mode, a.cfg.Case())
	if err != nil {
		a.errorf("bad pattern: %v", err)
		return
	}

	next := a.rules.Replace(i, filter.Rule{
		Action:  old.Action,
		M:       m,
		Enabled: old.Enabled,
		Color:   old.Color,
	})
	a.applyRules(next, true)
	a.infof("rule %d is now %s", i+1, pattern)

	if a.ov != nil && a.ov.kind == ovRules {
		a.showRules()
	}
}

// cycleRuleAction moves a rule between only, never and highlight in place,
// which is usually what you want after seeing what it actually caught.
func (a *App) cycleRuleAction(i int) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	old := rules[i]

	var next filter.Action
	switch old.Action {
	case filter.Only:
		next = filter.Never
	case filter.Never:
		next = filter.Highlight
	default:
		next = filter.Only
	}

	a.applyRules(a.rules.Replace(i, filter.Rule{
		Action:  next,
		M:       old.M,
		Enabled: old.Enabled,
		Color:   old.Color,
	}), true)
	a.showRules()
}

// recolourRule assigns a fresh colour, for when two rules landed too close
// together to tell apart.
func (a *App) recolourRule(i int) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	old := rules[i]

	// Exclude this rule's own colour from the set being avoided, or it would
	// be treated as one to stay away from and the choice would be needlessly
	// constrained.
	used := make([]filter.Color, 0, len(rules))
	for j, r := range rules {
		if j != i {
			used = append(used, r.Color)
		}
	}

	a.applyRules(a.rules.Replace(i, filter.Rule{
		Action:  old.Action,
		M:       old.M,
		Enabled: old.Enabled,
		Color:   filter.NextColor(used),
	}), false)
	a.showRules()
}
