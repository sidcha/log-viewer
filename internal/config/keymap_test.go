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
