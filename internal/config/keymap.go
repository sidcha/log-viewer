package config

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

// keyNode is one step of the keymap trie.
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

// LookupVisual resolves a sequence against the visual-mode map.
//
// Visual mode needs its own table because the same key means different things
// in each: y copies the selection when there is one, and is the first half of
// yy when there is not. One table cannot express that, and a y that fired
// immediately would make yy unreachable.
func (c *Config) LookupVisual(seq []Key) (Command, Match) {
	return lookupIn(c.vroot, seq)
}

// Lookup resolves a sequence of key presses.
//
// A sequence can be both a complete binding and a prefix of a longer one. That
// is reported as Full, so the shorter binding wins immediately rather than
// waiting on a timeout: a viewer that paused after every `g` to see whether a
// second one was coming would feel broken.
func (c *Config) Lookup(seq []Key) (Command, Match) {
	return lookupIn(c.root, seq)
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

// bindSeq installs a normal-mode binding.
func (c *Config) bindSeq(seq []Key, spec string, cmd Command) {
	c.bindIn(&c.root, &c.order, seq, spec, cmd, false)
}

// bindVisual installs a visual-mode binding.
func (c *Config) bindVisual(seq []Key, spec string, cmd Command) {
	c.bindIn(&c.vroot, &c.vorder, seq, spec, cmd, true)
}

func (c *Config) bindIn(root **keyNode, order *[]Bind, seq []Key, spec string, cmd Command, visual bool) {
	if *root == nil {
		*root = &keyNode{}
	}
	n := *root
	for _, k := range seq {
		n = n.child(k)
	}
	replaced := n.cmd != nil
	cp := cmd
	n.cmd = &cp

	entry := Bind{Keys: seq, Spec: spec, Cmd: cmd, Visual: visual}
	if replaced {
		for i := range *order {
			if sameSeq((*order)[i].Keys, seq) {
				(*order)[i] = entry
				return
			}
		}
	}
	*order = append(*order, entry)
}

// unbindSeq removes a binding. Interior nodes are left in place; they are tiny
// and removing them would complicate nothing useful.
func (c *Config) unbindSeq(seq []Key) {
	c.unbindIn(c.root, &c.order, seq)
}

func (c *Config) unbindVisual(seq []Key) {
	c.unbindIn(c.vroot, &c.vorder, seq)
}

func (c *Config) unbindIn(root *keyNode, order *[]Bind, seq []Key) {
	if root == nil {
		return
	}
	n := root
	for _, k := range seq {
		if n.children == nil {
			return
		}
		next, ok := n.children[k]
		if !ok {
			return
		}
		n = next
	}
	n.cmd = nil

	for i := range *order {
		if sameSeq((*order)[i].Keys, seq) {
			*order = append((*order)[:i], (*order)[i+1:]...)
			return
		}
	}
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
