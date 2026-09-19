package config

import "fmt"

// Match is the result of feeding a key sequence to the keymap.
type Match int

const (
	// NoMatch means nothing is bound to this sequence, or any extension of it.
	NoMatch Match = iota
	// Partial means the sequence is a prefix of a binding; more keys are
	// needed. This is what makes `gg` work without `g` doing anything first.
	Partial
	// Full means the sequence resolves to a command.
	Full
)

// Table is a set of key bindings in force while lv is in one mode.
//
// Every key lv responds to is bound in one of these, so every key can be
// rebound: `bind -T finder <C-t> finder-toggle-regex`. A key a table does not
// bind is looked up in its fallback, which is how visual mode inherits the
// motions and how the prompt and the match window share one set of editing
// keys.
type Table struct {
	Name string
	// Fallback is consulted for sequences this table does not bind.
	Fallback string
	// Context is the kind of command this table's own bindings run; a table
	// also accepts the commands of its fallbacks.
	Context string
	Help    string
}

// Tables lists every key table.
var Tables = []Table{
	{"normal", "", "view", "reading the log"},
	{"visual", "normal", "view", "while selecting"},
	{"line", "", "line", "editing a line of text, at the prompt and in the match window"},
	{"prompt", "line", "prompt", "at the bottom-row prompt"},
	{"finder", "line", "finder", "in the match window"},
	{"overlay", "", "panel", "in a panel: the help and the rule list"},
	{"rules", "overlay", "rules", "in the rule list"},
	{"confirm", "", "confirm", "answering a question on the status row"},
}

var tableIndex = func() map[string]*Table {
	m := make(map[string]*Table, len(Tables))
	for i := range Tables {
		m[Tables[i].Name] = &Tables[i]
	}
	return m
}()

// LookupTable finds a key table by name.
func LookupTable(name string) (*Table, bool) {
	t, ok := tableIndex[name]
	return t, ok
}

// chain is the table and its fallbacks, nearest first.
func (t *Table) chain() []*Table {
	var out []*Table
	for cur := t; cur != nil; {
		out = append(out, cur)
		cur = tableIndex[cur.Fallback]
	}
	return out
}

// accepts reports whether a command may be bound in the table: whether the
// mode the table serves is one in which the command makes sense.
func (t *Table) accepts(s *Spec) bool {
	if s.Name == "nop" {
		return true // unbinds a key a fallback would otherwise supply
	}
	for _, c := range t.chain() {
		if s.Context() == c.Context {
			return true
		}
	}
	return false
}

// keyNode is one step of a keymap trie.
type keyNode struct {
	cmd      *Command
	children map[Key]*keyNode
}

func (n *keyNode) child(k Key) *keyNode {
	if n.children == nil {
		n.children = make(map[Key]*keyNode, 4)
	}
	c, ok := n.children[k]
	if !ok {
		c = &keyNode{}
		n.children[k] = c
	}
	return c
}

// Lookup resolves a sequence of key presses in the normal table.
func (c *Config) Lookup(seq []Key) (Command, Match) { return c.LookupIn("normal", seq) }

// LookupIn resolves a sequence of key presses in a table, then in each of its
// fallbacks until one of them knows the sequence.
//
// A sequence can be both a complete binding and a prefix of a longer one. That
// is reported as Full, so the shorter binding wins immediately rather than
// waiting on a timeout: a viewer that paused after every `g` to see whether a
// second one was coming would feel broken. For the same reason a table that
// has the sequence as a prefix wins over a fallback that binds it outright:
// visual mode's y copies, while in normal mode y is the first half of yy.
func (c *Config) LookupIn(table string, seq []Key) (Command, Match) {
	t, ok := tableIndex[table]
	if !ok {
		return Command{}, NoMatch
	}
	for _, tt := range t.chain() {
		if cmd, m := lookupIn(c.tables[tt.Name], seq); m != NoMatch {
			return cmd, m
		}
	}
	return Command{}, NoMatch
}

func lookupIn(n *keyNode, seq []Key) (Command, Match) {
	for _, k := range seq {
		if n == nil || n.children == nil {
			return Command{}, NoMatch
		}
		next, ok := n.children[k]
		if !ok {
			return Command{}, NoMatch
		}
		n = next
	}
	switch {
	case n == nil:
		return Command{}, NoMatch
	case n.cmd != nil:
		return *n.cmd, Full
	case len(n.children) > 0:
		return Command{}, Partial
	default:
		return Command{}, NoMatch
	}
}

// Binds lists a table's own bindings in declaration order, for the help
// overlay.
func (c *Config) Binds(table string) []Bind { return c.orders[table] }

// KeysFor lists the key specifications that run a command in a table or its
// fallbacks, nearest first, so a question on screen can say which key answers
// it.
func (c *Config) KeysFor(table, command string) []string {
	t, ok := tableIndex[table]
	if !ok {
		return nil
	}
	var out []string
	for _, tt := range t.chain() {
		for _, b := range c.orders[tt.Name] {
			if b.Cmd.Name == command {
				out = append(out, b.Spec)
			}
		}
	}
	return out
}

// bind installs a binding in a table, replacing any earlier one for the same
// sequence.
func (c *Config) bind(table string, seq []Key, spec string, cmd Command) error {
	t, ok := tableIndex[table]
	if !ok {
		return fmt.Errorf("unknown key table %q", table)
	}
	s, _ := LookupCommand(cmd.Name)
	if !t.accepts(s) {
		return fmt.Errorf("%s is not a command for the %s table", cmd.Name, table)
	}

	if c.tables[table] == nil {
		c.tables[table] = &keyNode{}
	}
	n := c.tables[table]
	for _, k := range seq {
		n = n.child(k)
	}
	replaced := n.cmd != nil
	cp := cmd
	n.cmd = &cp

	entry := Bind{Keys: seq, Spec: spec, Cmd: cmd, Table: table}
	order := c.orders[table]
	if replaced {
		for i := range order {
			if sameSeq(order[i].Keys, seq) {
				order[i] = entry
				return nil
			}
		}
	}
	c.orders[table] = append(order, entry)
	return nil
}

// unbind removes a binding. Interior nodes are left in place; they are tiny
// and removing them would complicate nothing useful.
func (c *Config) unbind(table string, seq []Key) error {
	if _, ok := tableIndex[table]; !ok {
		return fmt.Errorf("unknown key table %q", table)
	}
	n := c.tables[table]
	if n == nil {
		return nil
	}
	for _, k := range seq {
		if n.children == nil {
			return nil
		}
		next, ok := n.children[k]
		if !ok {
			return nil
		}
		n = next
	}
	n.cmd = nil

	order := c.orders[table]
	for i := range order {
		if sameSeq(order[i].Keys, seq) {
			c.orders[table] = append(order[:i], order[i+1:]...)
			return nil
		}
	}
	return nil
}

func sameSeq(a, b []Key) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
