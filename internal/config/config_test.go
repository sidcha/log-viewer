package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func words(t *testing.T, src string) [][]string {
	t.Helper()
	stmts, err := tokenize("t", src)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	out := make([][]string, len(stmts))
	for i, s := range stmts {
		out[i] = s.Words
	}
	return out
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"plain", "set follow on", "set|follow|on"},
		{"comment line", "# nothing\nset wrap off", "set|wrap|off"},
		{"trailing comment", "set wrap off # why not", "set|wrap|off"},
		{"double quotes", `bind x highlight "a b"`, "bind|x|highlight|a b"},
		{"escapes", `style s fg="\tred"`, "style|s|fg=\tred"},
		{"single quotes keep backslashes", `rule '\d+\s' n`, `rule|\d+\s|n`},
		{"hash inside a word", "style s fg=#ff0000", "style|s|fg=#ff0000"},
		{"continuation", "bind x \\\n  quit", "bind|x|quit"},
		{"braces are words", "profile k {\n}", "profile|k|{ }"},
		{"empty lines ignored", "\n\n\nset wrap on\n\n", "set|wrap|on"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := words(t, tc.src)
			var parts []string
			for _, w := range got {
				parts = append(parts, strings.Join(w, "|"))
			}
			if strings.Join(parts, " ") != tc.want {
				t.Fatalf("got %q, want %q", strings.Join(parts, " "), tc.want)
			}
		})
	}
}

func TestTokenizeErrors(t *testing.T) {
	for _, src := range []string{`set a "unterminated`, `set a 'unterminated`} {
		if _, err := tokenize("t", src); err == nil {
			t.Errorf("tokenize(%q) should have failed", src)
		}
	}
}

func TestParseKeyRoundTrip(t *testing.T) {
	cases := map[string]string{
		"q":      "q",
		"/":      "/",
		"C-a":    "C-a",
		"C-A":    "C-a",
		"M-x":    "M-x",
		"Space":  "Space",
		"Enter":  "Enter",
		"Escape": "Escape",
		"PgUp":   "Pgup",
		"F5":     "F5",
		"Up":     "Up",
	}
	for spec, want := range cases {
		k, err := ParseKey(spec)
		if err != nil {
			t.Errorf("ParseKey(%q): %v", spec, err)
			continue
		}
		if got := k.String(); got != want {
			t.Errorf("ParseKey(%q).String() = %q, want %q", spec, got, want)
		}
	}

	for _, bad := range []string{"", "NoSuchKey", "C-Enter", "abc"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) should have failed", bad)
		}
	}
}

func TestKeyFromEventMatchesParsedBinding(t *testing.T) {
	// The normalisation in FromEvent must agree with ParseKey or nothing is
	// ever dispatched.
	cases := []struct {
		spec string
		ev   *tcell.EventKey
	}{
		{"q", tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)},
		{"C-a", tcell.NewEventKey(tcell.KeyCtrlA, rune(1), tcell.ModCtrl)},
		{"M-x", tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModAlt)},
		{"Enter", tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone)},
		{"Up", tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)},
		{"F1", tcell.NewEventKey(tcell.KeyF1, 0, tcell.ModNone)},
	}
	for _, c := range cases {
		want, err := ParseKey(c.spec)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", c.spec, err)
		}
		if got := FromEvent(c.ev); got != want {
			t.Errorf("FromEvent for %s = %+v, want %+v", c.spec, got, want)
		}
	}
}

func TestParseStyle(t *testing.T) {
	s, err := parseStyle([]string{"fg=red", "bg=#102030", "bold", "underline"})
	if err != nil {
		t.Fatal(err)
	}
	fg, bg, attr := s.Decompose()
	if fg != tcell.ColorRed {
		t.Errorf("fg = %v, want red", fg)
	}
	if r, g, b := bg.RGB(); r != 0x10 || g != 0x20 || b != 0x30 {
		t.Errorf("bg = %02x%02x%02x, want 102030", r, g, b)
	}
	if attr&tcell.AttrBold == 0 || attr&tcell.AttrUnderline == 0 {
		t.Errorf("attrs = %v, want bold+underline", attr)
	}

	for _, bad := range [][]string{{"fg=notacolour"}, {"fg=#12345"}, {"sparkly"}, {"fg"}} {
		if _, err := parseStyle(bad); err == nil {
			t.Errorf("parseStyle(%v) should have failed", bad)
		}
	}

	if _, err := parseStyle([]string{"fg=color214"}); err != nil {
		t.Errorf("palette colour: %v", err)
	}
}

func TestBuiltinConfigLoads(t *testing.T) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(c.Binds()) == 0 {
		t.Error("no key bindings in the built-in config")
	}
	if len(c.Profiles()) == 0 {
		t.Error("no syntax profiles in the built-in config")
	}
	// Resolve must have bound every rule's style; a missing one is an error at
	// load time, but check that the styles actually landed.
	for _, p := range c.Profiles() {
		for _, r := range p.Rules {
			if r.Style == (tcell.Style{}) && r.StyleName != "" {
				// tcell.StyleDefault is the zero value, so compare names instead.
				if !c.HasStyle(r.StyleName) {
					t.Errorf("profile %s rule references undefined style %q", p.Name, r.StyleName)
				}
			}
		}
	}

	q, _ := ParseKey("q")
	if cmd, ok := c.Command(q); !ok || cmd.Name != "quit" {
		t.Errorf("q is bound to %v (ok=%v), want quit", cmd, ok)
	}
}

func TestDumpConfigRoundTrips(t *testing.T) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	dump := c.Dump()

	// The dump is documentation only if it parses back.
	c2 := New()
	if err := c2.Parse("<dump>", dump); err != nil {
		t.Fatalf("re-parsing --dump-config output failed: %v", err)
	}
	if err := c2.Resolve(); err != nil {
		t.Fatalf("re-resolving --dump-config output failed: %v", err)
	}
	if len(c2.Binds()) != len(c.Binds()) {
		t.Errorf("round trip changed binding count: %d -> %d", len(c.Binds()), len(c2.Binds()))
	}
	if len(c2.Profiles()) != len(c.Profiles()) {
		t.Errorf("round trip changed profile count: %d -> %d", len(c.Profiles()), len(c2.Profiles()))
	}
}

func TestUserConfigOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lv.conf")
	user := `
set follow on
set tab-width 4
unbind q
bind Q quit
style syntax.time fg=#010203
profile kernel {
    clear
    rule 'BOOM' syntax.level.fatal
}
`
	if err := os.WriteFile(path, []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !c.Bool("follow") || c.Int("tab-width") != 4 {
		t.Errorf("settings not applied: follow=%v tab-width=%d", c.Bool("follow"), c.Int("tab-width"))
	}

	q, _ := ParseKey("q")
	if _, ok := c.Command(q); ok {
		t.Error("q should have been unbound")
	}
	bigQ, _ := ParseKey("Q")
	if cmd, ok := c.Command(bigQ); !ok || cmd.Name != "quit" {
		t.Error("Q should be bound to quit")
	}

	if r, g, b := styleFG(c.Style("syntax.time")); r != 1 || g != 2 || b != 3 {
		t.Errorf("style override not applied: %d,%d,%d", r, g, b)
	}

	for _, p := range c.Profiles() {
		if p.Name != "kernel" {
			continue
		}
		if len(p.Rules) != 1 || p.Rules[0].Re.String() != "BOOM" {
			t.Errorf("profile clear+rule did not replace the built-in rules: %d rules", len(p.Rules))
		}
		if len(p.Globs) == 0 {
			t.Error("re-opening a profile should keep its existing globs")
		}
	}
}

func styleFG(s tcell.Style) (int32, int32, int32) {
	fg, _, _ := s.Decompose()
	return fg.RGB()
}

func TestConfigErrors(t *testing.T) {
	cases := map[string]string{
		"unknown setting":   "set nosuchthing on",
		"bad bool":          "set follow maybe",
		"bad enum":          "set status-line sometimes",
		"bad duration":      "set watch-backstop soon",
		"bad action":        "rule sideways 'x'",
		"rule needs args":   "rule only",
		"bad rule colour":   "rule highlight 'x' color=nope",
		"unknown rule opt":  "rule highlight 'x' shade=red",
		"unknown command":   "bind x frobnicate",
		"unknown key":       "bind NoSuchKey quit",
		"too many args":     "bind x quit extra",
		"unknown directive": "frobnicate x",
		"bad regex":         "profile p {\n rule '[' s\n}",
		"unclosed block":    "profile p {\n rule 'a' s",
		"bad profile dir":   "profile p {\n nonsense\n}",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			c := New()
			if err := c.Parse("t", src); err == nil {
				t.Fatalf("expected an error for %q", src)
			}
		})
	}

	t.Run("undefined style", func(t *testing.T) {
		c := New()
		if err := c.Parse("t", "profile p {\n rule 'a' no.such.style\n}"); err != nil {
			t.Fatal(err)
		}
		if err := c.Resolve(); err == nil {
			t.Fatal("Resolve should reject a rule with an undefined style")
		}
	})
}

func TestParseLine(t *testing.T) {
	got, err := ParseLine(`filter-out 'a b'`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "filter-out|a b" {
		t.Fatalf("ParseLine = %q", got)
	}
	if _, err := ParseCommand([]string{"nope"}); err == nil {
		t.Error("ParseCommand should reject an unknown command")
	}
}

// commandLineOnly lists commands deliberately reachable only from the `:`
// prompt, because they take an argument or duplicate something a key already
// does. Anything else without a binding is an oversight.
var commandLineOnly = map[string]bool{
	"nop":          true,
	"set":          true, // :set wrap, like vim
	"goto-line":    true, // :42, or 42G
	"column-reset": true, // 0 already resets the horizontal offset
	"search-clear": true, // :noh
}

func TestEverySpecIsReachable(t *testing.T) {
	c, err := Load(LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	bound := map[string]bool{}
	for _, b := range c.Binds() {
		bound[b.Cmd.Name] = true
	}
	for _, b := range c.VisualBinds() {
		bound[b.Cmd.Name] = true
	}
	for _, s := range Specs {
		if commandLineOnly[s.Name] {
			if bound[s.Name] {
				t.Errorf("command %q is listed as command-line only but is bound", s.Name)
			}
			continue
		}
		if !bound[s.Name] {
			t.Errorf("command %q has no default key binding", s.Name)
		}
	}
}
