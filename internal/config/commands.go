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
	{"scroll-down", "motion", 0, 1, "scroll the window down scroll-step lines per count, cursor follows"},
	{"scroll-up", "motion", 0, 1, "scroll the window up scroll-step lines per count, cursor follows"},
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
	{"scroll-right", "motion", 0, 1, "scroll the window right by h-scroll-step columns"},
	{"scroll-left", "motion", 0, 1, "scroll the window left by h-scroll-step columns"},

	{"visual", "select", 0, 0, "start or end a character-wise selection"},
	{"visual-line", "select", 0, 0, "start or end a line-wise selection"},
	{"yank", "select", 0, 0, "copy the selection, or the current line"},
	{"yank-line", "select", 0, 1, "copy whole lines"},

	{"follow-toggle", "view", 0, 0, "follow the end of the file, like tail -f"},
	{"wrap-toggle", "view", 0, 0, "wrap long lines instead of truncating them"},
	{"line-numbers-toggle", "view", 0, 0, "show source line numbers in the gutter"},
	{"status-toggle", "view", 0, 0, "pin the status line on or off"},
	{"file-info", "view", 0, 0, "show the file, the position in it and its syntax profile, like vi's Ctrl-G"},
	{"reload", "view", 0, 0, "re-open and re-index the file"},
	{"redraw", "view", 0, 0, "force a full repaint"},
	{"set", "view", 1, 2, "change a setting; with no value, toggle it"},

	{"buffer-next", "buffer", 0, 1, "go to the next file; a count skips that many"},
	{"buffer-prev", "buffer", 0, 1, "go to the previous file"},
	{"buffer-go", "buffer", 1, 1, "go to file N, counting from 1"},
	{"buffer-list", "buffer", 0, 0, "list the open files and pick one"},

	{"search-forward", "search", 0, 1, "search forwards (prompts when given no argument)"},
	{"search-backward", "search", 0, 1, "search backwards (prompts when given no argument)"},
	{"search-next", "search", 0, 0, "jump to the next match"},
	{"search-prev", "search", 0, 0, "jump to the previous match"},
	{"search-word", "search", 0, 0, "search for the word under the cursor"},
	{"search-word-back", "search", 0, 0, "search backwards for the word under the cursor"},
	{"search-clear", "search", 0, 0, "drop the search highlight and repaint"},
	{"jump", "search", 0, 1, "fuzzy-find a line and go to it, leaving no highlight"},

	{"filter-only", "filter", 0, 1, "show only lines matching a pattern"},
	{"filter-never", "filter", 0, 1, "never show lines matching a pattern"},
	{"highlight", "filter", 0, 1, "mark a pattern without filtering"},
	{"filter-pop", "filter", 0, 0, "remove the most recent rule"},
	{"filter-clear", "filter", 0, 0, "remove every rule"},
	{"highlight-clear", "filter", 0, 0, "remove every highlight rule, keeping the filters and the search"},
	{"filter-bypass", "filter", 0, 0, "temporarily show all lines without losing the rules"},
	{"filter-list", "filter", 0, 0, "edit, enable and disable the rules"},

	{"save-view", "file", 0, 1, "write the current view to a file (prompts when given no argument)"},
	{"rules-save", "file", 0, 1, "write the rules to a file so they can be used again"},
	{"rules-load", "file", 0, 1, "replace the rules with those in a file"},

	{"command", "other", 0, 0, "open the command line"},
	{"help", "other", 0, 0, "show key bindings"},
	{"cancel", "other", 0, 0, "leave visual mode, close an overlay, or cancel an operation"},
	{"quit", "other", 0, 0, "exit, offering to save the rules first"},
	{"quit-force", "other", 0, 0, "exit without asking about the rules"},
	{"nop", "other", 0, 0, "do nothing; bound to a key, it takes that key away"},

	{"edit-left", "edit", 0, 0, "cursor left"},
	{"edit-right", "edit", 0, 0, "cursor right"},
	{"edit-start", "edit", 0, 0, "cursor to the start of the line"},
	{"edit-end", "edit", 0, 0, "cursor to the end of the line"},
	{"edit-backspace", "edit", 0, 0, "delete the character before the cursor"},
	{"edit-delete", "edit", 0, 0, "delete the character under the cursor"},
	{"edit-clear", "edit", 0, 0, "delete the whole line"},
	{"edit-kill-end", "edit", 0, 0, "delete from the cursor to the end"},
	{"edit-delete-word", "edit", 0, 0, "delete the word before the cursor"},
	{"history-prev", "edit", 0, 0, "recall the previous entry"},
	{"history-next", "edit", 0, 0, "recall the next entry"},
	{"edit-accept", "edit", 0, 0, "accept what was typed"},
	{"edit-cancel", "edit", 0, 0, "close without doing anything"},

	{"prompt-complete", "prompt", 0, 0, "complete a command name, cycling on repeat"},

	{"finder-select-prev", "finder", 0, 1, "select the previous match"},
	{"finder-select-next", "finder", 0, 1, "select the next match"},
	{"finder-page-up", "finder", 0, 0, "select a window's height up"},
	{"finder-page-down", "finder", 0, 0, "select a window's height down"},
	{"finder-preview", "finder", 0, 0, "go to the selected match, keeping the window open"},
	{"finder-toggle-regex", "finder", 0, 0, "read the pattern as PCRE, and back"},

	{"panel-close", "panel", 0, 0, "close the panel"},
	{"panel-up", "panel", 0, 1, "move up a line"},
	{"panel-down", "panel", 0, 1, "move down a line"},
	{"panel-page-up", "panel", 0, 0, "page up"},
	{"panel-page-down", "panel", 0, 0, "page down"},
	{"panel-top", "panel", 0, 0, "go to the top"},
	{"panel-bottom", "panel", 0, 0, "go to the bottom"},
	{"panel-scroll-up", "panel", 0, 1, "scroll up, leaving the selection"},
	{"panel-scroll-down", "panel", 0, 1, "scroll down, leaving the selection"},

	{"rules-toggle", "rules", 0, 0, "enable or disable the rule"},
	{"rules-delete", "rules", 0, 0, "delete the rule"},
	{"rules-edit", "rules", 0, 0, "edit the rule's pattern in the match window"},
	{"rules-cycle-action", "rules", 0, 0, "cycle the rule between only, never and highlight"},
	{"rules-recolour", "rules", 0, 0, "give the rule a different colour"},
	{"rules-enable-all", "rules", 0, 0, "enable every rule"},
	{"rules-disable-all", "rules", 0, 0, "disable every rule"},

	{"buffers-select", "buffers", 0, 0, "go to the selected file, or the number typed"},
	{"buffers-cancel", "buffers", 0, 0, "drop a typed number, or close the list"},

	{"confirm-yes", "confirm", 0, 0, "answer yes"},
	{"confirm-no", "confirm", 0, 0, "answer no"},
	{"confirm-other", "confirm", 0, 0, "take the third answer, when there is one"},
	{"confirm-cancel", "confirm", 0, 0, "cancel the question"},
}

// groupContext says which kind of key table a command group belongs to.
// Everything not listed acts on the log view.
var groupContext = map[string]string{
	"edit":    "line",
	"prompt":  "prompt",
	"finder":  "finder",
	"panel":   "panel",
	"rules":   "rules",
	"buffers": "buffers",
	"confirm": "confirm",
}

// Context is the kind of key table the command can be bound in: "view" for
// the log itself, or the mode it acts on.
func (s *Spec) Context() string {
	if c, ok := groupContext[s.Group]; ok {
		return c
	}
	return "view"
}

// Aliases maps the short names vim users reach for onto lv's commands.
//
// The full names are the vocabulary; these exist so that :q, :w and :noh do
// what a decade of muscle memory expects rather than reporting an error. They
// are accepted wherever a command is, so a binding can use them too.
var Aliases = map[string]string{
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
	"f":          "jump",
	"follow":     "follow-toggle",
	"n":          "search-next",
	"N":          "search-prev",
	"g":          "goto-line",
	"bn":         "buffer-next",
	"bnext":      "buffer-next",
	"bp":         "buffer-prev",
	"bprev":      "buffer-prev",
	"b":          "buffer-go",
	"buffer":     "buffer-go",
	"buffers":    "buffer-list",
	"files":      "buffer-list",
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

// ParseCommand validates a command and its arguments, resolving an alias to
// the command it stands for.
func ParseCommand(words []string) (Command, error) {
	if len(words) == 0 {
		return Command{}, fmt.Errorf("empty command")
	}
	name := words[0]
	if full, ok := Aliases[name]; ok {
		name = full
	}
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
