package config

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func seq(t *testing.T, spec string) []Key {
	t.Helper()
	k, err := ParseSequence(spec)
	if err != nil {
		t.Fatalf("ParseSequence(%q): %v", spec, err)
	}
	return k
}

func TestParseSequence(t *testing.T) {
	cases := map[string]int{
		"q":       1,
		"C-a":     1,
		"PgUp":    1, // a named key must not be read as five presses
		"Escape":  1,
		"F5":      1,
		"gg":      2,
		"zz":      2,
		"ZZ":      2,
		"<C-w>s":  2,
		"<C-d>":   1,
		"g<Home>": 2,
		"abc":     3,
	}
	for spec, want := range cases {
		got, err := ParseSequence(spec)
		if err != nil {
			t.Errorf("ParseSequence(%q): %v", spec, err)
			continue
		}
		if len(got) != want {
			t.Errorf("ParseSequence(%q) produced %d keys, want %d", spec, len(got), want)
		}
	}

	for _, bad := range []string{"", "<C-d", "<>", "<NoSuchKey>"} {
		if _, err := ParseSequence(bad); err == nil {
			t.Errorf("ParseSequence(%q) should have failed", bad)
		}
	}
}

func TestParseSequenceAgreesWithParseKey(t *testing.T) {
	// Both notations for the same key must land on the same Key, or a vim-style
	// config and a tmux-style one would disagree.
	for _, pair := range [][2]string{
		{"C-d", "<C-d>"},
		{"Enter", "<CR>"},
		{"Escape", "<Esc>"},
		{"Space", "<Space>"},
		{"PgUp", "<PgUp>"},
	} {
		a := seq(t, pair[0])
		b := seq(t, pair[1])
		if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
			t.Errorf("%q and %q parsed differently: %+v vs %+v", pair[0], pair[1], a, b)
		}
	}
}

func TestSequenceStringRoundTrips(t *testing.T) {
	for _, spec := range []string{"gg", "zz", "<C-d>", "<CR>", "q", "<C-w>s"} {
		s := SequenceString(seq(t, spec))
		if !sameSeq(seq(t, s), seq(t, spec)) {
			t.Errorf("SequenceString(%q) = %q, which parses differently", spec, s)
		}
	}
}

func TestLookupPartialAndFull(t *testing.T) {
	c := New()
	c.bindSeq(seq(t, "gg"), "gg", Command{Name: "goto-start"})
	c.bindSeq(seq(t, "G"), "G", Command{Name: "goto-end"})

	// A lone g is a prefix, not a command: nothing should happen yet.
	if _, m := c.Lookup(seq(t, "g")); m != Partial {
		t.Errorf("Lookup(g) = %v, want Partial", m)
	}
	cmd, m := c.Lookup(seq(t, "gg"))
	if m != Full || cmd.Name != "goto-start" {
		t.Errorf("Lookup(gg) = (%v, %v), want goto-start/Full", cmd, m)
	}
	if _, m := c.Lookup(seq(t, "gx")); m != NoMatch {
		t.Errorf("Lookup(gx) = %v, want NoMatch", m)
	}
	if cmd, m := c.Lookup(seq(t, "G")); m != Full || cmd.Name != "goto-end" {
		t.Errorf("Lookup(G) = (%v, %v)", cmd, m)
	}
	if _, m := c.Lookup(seq(t, "x")); m != NoMatch {
		t.Errorf("Lookup(x) = %v, want NoMatch", m)
	}
}

// TestShorterBindingWinsImmediately is why there is no timeout: if `d` is
// bound and `dd` is too, pressing `d` must act at once.
func TestShorterBindingWinsImmediately(t *testing.T) {
	c := New()
	c.bindSeq(seq(t, "y"), "y", Command{Name: "yank"})
	c.bindSeq(seq(t, "yy"), "yy", Command{Name: "yank-line"})

	cmd, m := c.Lookup(seq(t, "y"))
	if m != Full || cmd.Name != "yank" {
		t.Fatalf("Lookup(y) = (%v, %v), want yank/Full", cmd, m)
	}
	cmd, m = c.Lookup(seq(t, "yy"))
	if m != Full || cmd.Name != "yank-line" {
		t.Fatalf("Lookup(yy) = (%v, %v), want yank-line/Full", cmd, m)
	}
}

func TestRebindAndUnbind(t *testing.T) {
	c := New()
	c.bindSeq(seq(t, "gg"), "gg", Command{Name: "goto-start"})
	c.bindSeq(seq(t, "gg"), "gg", Command{Name: "goto-end"})

	if cmd, _ := c.Lookup(seq(t, "gg")); cmd.Name != "goto-end" {
		t.Errorf("rebinding did not replace: %v", cmd)
	}
	if n := len(c.Binds()); n != 1 {
		t.Errorf("rebinding duplicated the help entry: %d binds", n)
	}

	c.unbindSeq(seq(t, "gg"))
	if _, m := c.Lookup(seq(t, "gg")); m == Full {
		t.Error("unbind left the command in place")
	}
	if n := len(c.Binds()); n != 0 {
		t.Errorf("unbind left %d help entries", n)
	}
}

// TestUnbindLeavesSiblings guards the trie: removing one binding must not take
// out another that shares its prefix.
func TestUnbindLeavesSiblings(t *testing.T) {
	c := New()
	c.bindSeq(seq(t, "gg"), "gg", Command{Name: "goto-start"})
	c.bindSeq(seq(t, "ge"), "ge", Command{Name: "goto-end"})

	c.unbindSeq(seq(t, "gg"))
	if cmd, m := c.Lookup(seq(t, "ge")); m != Full || cmd.Name != "goto-end" {
		t.Errorf("Lookup(ge) after unbinding gg = (%v, %v)", cmd, m)
	}
	if _, m := c.Lookup(seq(t, "g")); m != Partial {
		t.Errorf("Lookup(g) = %v, want Partial", m)
	}
}

func TestConfigBindsSequences(t *testing.T) {
	c := New()
	src := `
bind gg   goto-start
bind <C-d> half-page-down
bind zz   redraw
unbind zz
`
	if err := c.Parse("t", src); err != nil {
		t.Fatal(err)
	}
	if cmd, m := c.Lookup(seq(t, "gg")); m != Full || cmd.Name != "goto-start" {
		t.Errorf("gg = (%v, %v)", cmd, m)
	}
	ctrlD := []Key{FromEvent(tcell.NewEventKey(tcell.KeyCtrlD, rune(4), tcell.ModCtrl))}
	if cmd, m := c.Lookup(ctrlD); m != Full || cmd.Name != "half-page-down" {
		t.Errorf("C-d = (%v, %v)", cmd, m)
	}
	if _, m := c.Lookup(seq(t, "zz")); m == Full {
		t.Error("unbind zz did not take effect")
	}
}

// TestLongBareSpecIsRejected keeps a misspelled key name from silently
// becoming a long sequence of single presses.
func TestLongBareSpecIsRejected(t *testing.T) {
	for _, bad := range []string{"NoSuchKey", "PgDown", "Contrl-a"} {
		if _, err := ParseSequence(bad); err == nil {
			t.Errorf("ParseSequence(%q) should have been rejected", bad)
		}
	}
	// Real sequences are short and must still work.
	for _, ok := range []string{"gg", "zz", "ZZ", "g0"} {
		if _, err := ParseSequence(ok); err != nil {
			t.Errorf("ParseSequence(%q): %v", ok, err)
		}
	}
	// Longer sequences are still expressible, just explicitly.
	if got, err := ParseSequence("<C-w><C-w>gg"); err != nil || len(got) != 4 {
		t.Errorf("ParseSequence(bracketed) = %d keys, %v", len(got), err)
	}
}

// TestShiftSpaceCannotBeDistinguished records a limitation of the terminal
// library rather than of lv.
//
// Shift changes nothing about the space character, so the modifier is the only
// thing that could tell Shift-Space from Space -- and tcell's NewEventKey
// drops a lone ModShift from every rune event before lv sees it. Until that
// changes, Shift-Space is unbindable, which is why b, C-b and PgUp carry
// paging back instead.
func TestShiftSpaceCannotBeDistinguished(t *testing.T) {
	plain, err := ParseKey("Space")
	if err != nil {
		t.Fatal(err)
	}
	shifted, err := ParseKey("S-Space")
	if err != nil {
		t.Fatal(err)
	}
	if plain == shifted {
		t.Fatal("Space and S-Space should parse to different keys")
	}
	if got := shifted.String(); got != "S-Space" {
		t.Errorf("S-Space renders as %q", got)
	}

	// The event, however, arrives indistinguishable from a plain space.
	got := FromEvent(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModShift))
	if got == shifted {
		t.Log("tcell now preserves Shift on space; S-Space could be bound by default")
		return
	}
	if got != plain {
		t.Errorf("Shift-Space arrived as %+v, want it to look like a plain Space", got)
	}
}

// TestShiftedNamedKeysBind: shift on an arrow or a page key is not encoded in
// the key code, so it has to survive normalisation or S-PgUp cannot be bound.
func TestShiftedNamedKeysBind(t *testing.T) {
	for _, spec := range []string{"S-PgUp", "S-Up", "S-Home"} {
		want, err := ParseKey(spec)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", spec, err)
		}
		got := FromEvent(tcell.NewEventKey(want.Key, 0, tcell.ModShift))
		if got != want {
			t.Errorf("%s: event %+v does not match binding %+v", spec, got, want)
		}
		// And the unshifted key stays distinct.
		plain, _ := ParseKey(spec[2:])
		if plain == want {
			t.Errorf("%s and %s parse the same", spec, spec[2:])
		}
	}
}

// TestCtrlLetterStillNormalises guards the other half: Ctrl is already in the
// key code there, so keeping the modifier would stop <C-d> matching.
func TestCtrlLetterStillNormalises(t *testing.T) {
	want, err := ParseKey("C-d")
	if err != nil {
		t.Fatal(err)
	}
	got := FromEvent(tcell.NewEventKey(tcell.KeyCtrlD, rune(4), tcell.ModCtrl))
	if got != want {
		t.Errorf("C-d: event %+v does not match binding %+v", got, want)
	}
}

// TestBindAcceptsAnAlias: :q works at the command line, so bind x q must too.
func TestBindAcceptsAnAlias(t *testing.T) {
	c := New()
	if err := c.Parse("t", "bind x q\nbind y noh\n"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[rune]string{'x': "quit", 'y': "search-clear"} {
		cmd, m := c.Lookup([]Key{{Key: tcell.KeyRune, Ch: key}})
		if m != Full || cmd.Name != want {
			t.Errorf("%c = %v %v, want %s", key, cmd, m, want)
		}
	}
}

// TestEveryAliasNamesACommand keeps the alias table from pointing nowhere.
func TestEveryAliasNamesACommand(t *testing.T) {
	for alias, name := range Aliases {
		if _, ok := LookupCommand(name); !ok {
			t.Errorf("alias %q names %q, which is not a command", alias, name)
		}
	}
}

// TestBackspaceMatchesWhatTheTerminalSends: terminals send DEL or ^H for the
// backspace key, and tcell reports both as one key; a binding naming the
// other code would never fire.
func TestBackspaceMatchesWhatTheTerminalSends(t *testing.T) {
	want := seq(t, "<BSpace>")[0]
	for _, k := range []tcell.Key{tcell.KeyBackspace2, tcell.KeyBackspace} {
		if got := FromEvent(tcell.NewEventKey(k, 0, tcell.ModNone)); got != want {
			t.Errorf("key %d arrives as %v, <BSpace> is %v", k, got, want)
		}
	}
}
