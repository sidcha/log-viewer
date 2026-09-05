package config

import (
	"fmt"
	"strings"
)

// Command is a named action with its arguments, as produced by a key binding
// or typed at the `:` prompt.
type Command struct {
	Name string
	Args []string
}

// String renders the command back into config syntax.
func (c Command) String() string {
	if len(c.Args) == 0 {
		return c.Name
	}
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, c.Name)
	for _, a := range c.Args {
		if strings.ContainsAny(a, " \t\"'") {
			parts = append(parts, "'"+a+"'")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// Spec describes one command in the vocabulary.
//
// Every interactive action goes through this table, which is what makes each
// one bindable, typeable at the `:` prompt and documented in the help overlay
// without any per-feature plumbing.
type Spec struct {
	Name    string
	Group   string
	MinArgs int
	MaxArgs int // -1 for unlimited
	Help    string
}

// Specs is the complete command vocabulary.
var Specs = []Spec{
	{"up", "motion", 0, 1, "cursor up"},
	{"down", "motion", 0, 1, "cursor down"},
	{"left", "motion", 0, 1, "cursor left"},
	{"right", "motion", 0, 1, "cursor right"},
	{"word-forward", "motion", 0, 1, "cursor to the start of the next word"},
	{"word-back", "motion", 0, 1, "cursor to the start of the previous word"},
	{"word-end", "motion", 0, 1, "cursor to the end of the word"},
	{"line-start", "motion", 0, 0, "cursor to column 0"},
	{"line-first", "motion", 0, 0, "cursor to the first non-blank character"},
	{"line-end", "motion", 0, 0, "cursor to the end of the line"},
	{"scroll-down", "motion", 0, 1, "scroll the window down, cursor follows"},
	{"scroll-up", "motion", 0, 1, "scroll the window up, cursor follows"},
	{"page-up", "motion", 0, 0, "up one screen"},
	{"page-down", "motion", 0, 0, "down one screen"},
	{"half-page-up", "motion", 0, 0, "up half a screen"},
	{"half-page-down", "motion", 0, 0, "down half a screen"},
	{"goto-start", "motion", 0, 0, "first line"},
	{"goto-end", "motion", 0, 0, "last line, or the counted line"},
	{"goto-line", "motion", 0, 1, "go to a line number (prompts when given no argument)"},
	{"screen-top", "motion", 0, 0, "cursor to the top of the window"},
	{"screen-middle", "motion", 0, 0, "cursor to the middle of the window"},
	{"screen-bottom", "motion", 0, 0, "cursor to the bottom of the window"},
	{"align-center", "motion", 0, 0, "redraw with the cursor centred"},
	{"align-top", "motion", 0, 0, "redraw with the cursor at the top"},
	{"align-bottom", "motion", 0, 0, "redraw with the cursor at the bottom"},
	{"column-reset", "motion", 0, 0, "scroll back to column 0"},

	{"visual", "select", 0, 0, "start or end a character-wise selection"},
	{"visual-line", "select", 0, 0, "start or end a line-wise selection"},
	{"yank", "select", 0, 0, "copy the selection, or the current line"},
	{"yank-line", "select", 0, 1, "copy whole lines"},

	{"follow-toggle", "view", 0, 0, "follow the end of the file, like tail -f"},
	{"wrap-toggle", "view", 0, 0, "wrap long lines instead of truncating them"},
	{"line-numbers-toggle", "view", 0, 0, "show source line numbers in the gutter"},
	{"status-toggle", "view", 0, 0, "pin the status line on or off"},
	{"reload", "view", 0, 0, "re-open and re-index the file"},
	{"redraw", "view", 0, 0, "force a full repaint"},
	{"set", "view", 1, 2, "change a setting; with no value, toggle it"},

	{"search-forward", "search", 0, 1, "search forwards (prompts when given no argument)"},
	{"search-backward", "search", 0, 1, "search backwards (prompts when given no argument)"},
	{"search-next", "search", 0, 0, "jump to the next match"},
	{"search-prev", "search", 0, 0, "jump to the previous match"},
	{"search-word", "search", 0, 0, "search for the word under the cursor"},
	{"search-word-back", "search", 0, 0, "search backwards for the word under the cursor"},
	{"search-clear", "search", 0, 0, "forget the current search"},

	{"filter-only", "filter", 0, 1, "show only lines matching a pattern"},
	{"filter-never", "filter", 0, 1, "never show lines matching a pattern"},
	{"highlight", "filter", 0, 1, "mark a pattern without filtering"},
	{"filter-pop", "filter", 0, 0, "remove the most recent rule"},
	{"filter-clear", "filter", 0, 0, "remove every rule"},
	{"highlight-clear", "filter", 0, 0, "remove every highlight, keeping the filters"},
	{"filter-bypass", "filter", 0, 0, "temporarily show all lines without losing the rules"},
	{"filter-list", "filter", 0, 0, "edit, enable and disable the rules"},

	{"save-view", "file", 0, 1, "write the current view to a file (prompts when given no argument)"},

	{"command", "other", 0, 0, "open the command line"},
	{"help", "other", 0, 0, "show key bindings"},
	{"cancel", "other", 0, 0, "leave visual mode, close an overlay, or cancel an operation"},
	{"quit", "other", 0, 0, "exit"},
	{"nop", "other", 0, 0, "do nothing"},
}

var specIndex = func() map[string]*Spec {
	m := make(map[string]*Spec, len(Specs))
	for i := range Specs {
		m[Specs[i].Name] = &Specs[i]
	}
	return m
}()

// LookupCommand finds a command specification by name.
func LookupCommand(name string) (*Spec, bool) {
	s, ok := specIndex[name]
	return s, ok
}

// ParseCommand validates a command and its arguments.
func ParseCommand(words []string) (Command, error) {
	if len(words) == 0 {
		return Command{}, fmt.Errorf("empty command")
	}
	name := words[0]
	spec, ok := specIndex[name]
	if !ok {
		return Command{}, fmt.Errorf("unknown command %q", name)
	}
	args := words[1:]
	if len(args) < spec.MinArgs {
		return Command{}, fmt.Errorf("%s takes at least %d argument(s)", name, spec.MinArgs)
	}
	if spec.MaxArgs >= 0 && len(args) > spec.MaxArgs {
		return Command{}, fmt.Errorf("%s takes at most %d argument(s)", name, spec.MaxArgs)
	}
	return Command{Name: name, Args: args}, nil
}
