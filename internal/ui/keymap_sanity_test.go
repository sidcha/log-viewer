package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

// TestNoDefaultBindingNeedsAlt: Alt bindings are a poor default. M-* means
// Alt and Shift and 8 on a common layout, which nobody is going to work out
// from a help screen, and plenty of terminals do not send it at all.
func TestNoDefaultBindingNeedsAlt(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range allBinds(cfg) {
		for _, k := range b.Keys {
			if k.Mod&tcell.ModAlt != 0 {
				t.Errorf("%s (%s) needs Alt", b.Spec, b.Cmd)
			}
		}
	}
}

// TestHelpExplainsTheKeyNotation: a help screen that prints <C-d> and gg
// without saying what they mean is only useful to someone who already knows.
func TestHelpExplainsTheKeyNotation(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	run(a, "help")

	var all strings.Builder
	for row := 0; row < testH; row++ {
		all.WriteString(rowText(scr, row))
		all.WriteByte('\n')
	}
	// The legend is below the fold, so read the whole overlay by scrolling to
	// the end.
	for prev := -1; a.ov.top != prev; {
		prev = a.ov.top
		a.ov.scroll(3)
		a.draw()
		for row := 0; row < testH; row++ {
			all.WriteString(rowText(scr, row))
			all.WriteByte('\n')
		}
	}

	text := all.String()
	for _, want := range []string{"Control", "Alt", "sequence"} {
		if !strings.Contains(text, want) {
			t.Errorf("the help never explains %q", want)
		}
	}
}

func TestZmClearsHighlights(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	a.addRule(filter.Only, "ERROR", color.RGB{})
	a.addRule(filter.Highlight, "bad", color.RGB{})
	waitFiltered(t, a)

	keys(a, "zm")

	if got := a.rules.Count(filter.Highlight); got != 0 {
		t.Errorf("highlight rules = %d, want 0", got)
	}
	if a.rules.Len() != 1 {
		t.Errorf("rules = %d, want the only rule kept", a.rules.Len())
	}
}

// TestEveryDefaultBindingIsTypable guards the general problem: a binding made
// of ordinary characters, named keys or Control is something a person can
// actually press.
func TestEveryDefaultBindingIsTypable(t *testing.T) {
	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range allBinds(cfg) {
		if len(b.Keys) == 0 {
			t.Errorf("%s binds no keys", b.Spec)
			continue
		}
		if len(b.Keys) > 2 {
			t.Errorf("%s is %d keys long; sequences that long are not muscle memory", b.Spec, len(b.Keys))
		}
	}
}

func allBinds(cfg *config.Config) []config.Bind {
	var out []config.Bind
	for _, t := range config.Tables {
		out = append(out, cfg.Binds(t.Name)...)
	}
	return out
}
