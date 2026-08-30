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
	{"up", "motion", 0, 1, "scroll up n lines (default 1)"},
	{"down", "motion", 0, 1, "scroll down n lines (default 1)"},
	{"left", "motion", 0, 1, "scroll left n columns"},
	{"right", "motion", 0, 1, "scroll right n columns"},
	{"page-up", "motion", 0, 0, "scroll up one screen"},
	{"page-down", "motion", 0, 0, "scroll down one screen"},
	{"half-page-up", "motion", 0, 0, "scroll up half a screen"},
	{"half-page-down", "motion", 0, 0, "scroll down half a screen"},
	{"goto-start", "motion", 0, 0, "jump to the first line"},
	{"goto-end", "motion", 0, 0, "jump to the last line"},
	{"goto-line", "motion", 0, 1, "jump to a line number (prompts when given no argument)"},
	{"column-reset", "motion", 0, 0, "scroll back to column 0"},

	{"follow-toggle", "view", 0, 0, "follow the end of the file, like tail -f"},
	{"wrap-toggle", "view", 0, 0, "wrap long lines instead of truncating them"},
	{"line-numbers-toggle", "view", 0, 0, "show source line numbers in the gutter"},
	{"status-toggle", "view", 0, 0, "pin the status line on or off"},
	{"reload", "view", 0, 0, "re-open and re-index the file"},
	{"redraw", "view", 0, 0, "force a full repaint"},

	{"search-forward", "search", 0, 1, "search forwards (prompts when given no argument)"},
	{"search-backward", "search", 0, 1, "search backwards (prompts when given no argument)"},
	{"search-next", "search", 0, 0, "jump to the next match"},
	{"search-prev", "search", 0, 0, "jump to the previous match"},
	{"search-clear", "search", 0, 0, "forget the current search"},

	{"filter-only", "filter", 0, 1, "show only lines matching a pattern"},
	{"filter-never", "filter", 0, 1, "never show lines matching a pattern"},
	{"highlight", "filter", 0, 1, "mark a pattern without filtering"},
	{"filter-pop", "filter", 0, 0, "remove the most recent rule"},
	{"filter-clear", "filter", 0, 0, "remove every rule"},
	{"highlight-clear", "filter", 0, 0, "remove every highlight, keeping the filters"},
	{"filter-bypass", "filter", 0, 0, "temporarily show all lines without losing the rules"},
	{"filter-list", "filter", 0, 0, "show the rules"},

	{"save-view", "file", 0, 1, "write the current view to a file (prompts when given no argument)"},

	{"command", "other", 0, 0, "open the command prompt"},
	{"help", "other", 0, 0, "show key bindings"},
	{"cancel", "other", 0, 0, "close an overlay or cancel a running operation"},
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
