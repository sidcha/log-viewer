package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/filter"
)

func ctrlKey(r rune) *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyCtrlA+tcell.Key(r-'a'), 0, tcell.ModCtrl)
}

// TestFinderKeysCanBeRebound: the match window's keys were a hard-coded
// switch that no configuration could reach.
func TestFinderKeysCanBeRebound(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	if err := a.cfg.Parse("t", "bind -T finder <C-t> finder-toggle-regex\nunbind -T finder <C-r>\n"); err != nil {
		t.Fatal(err)
	}
	keys(a, "/")
	a.key(ctrlKey('r'))
	if a.find.mode == filter.Regex {
		t.Fatal("the unbound C-r still toggled regex")
	}
	a.key(ctrlKey('t'))
	if a.find.mode != filter.Regex {
		t.Error("the new C-t did not toggle regex")
	}
}

// TestConfirmShowsTheBoundKey: a question has to name the key that answers
// it, whatever that has been rebound to.
func TestConfirmShowsTheBoundKey(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	if err := a.cfg.Parse("t", "unbind -T confirm n\nunbind -T confirm N\nbind -T confirm d confirm-no\n"); err != nil {
		t.Fatal(err)
	}
	quit := false
	a.ask("Really?", nil,
		choice{answerYes, "keep", nil},
		choice{answerNo, "discard", func() { quit = true }})
	a.draw()
	if row := rowText(scr, testH-1); !strings.Contains(row, "(d) discard") {
		t.Errorf("status row = %q, want the rebound key shown", row)
	}
	keys(a, "n")
	if a.confirm == nil {
		t.Fatal("n still answered the question")
	}
	keys(a, "d")
	if a.confirm != nil || !quit {
		t.Error("d did not answer no")
	}
}

// TestTypingInTheFinderIsNotShadowedByTheLog: the log's own keys, j and k and
// the rest, are letters in the match window.
func TestTypingInTheFinderIsNotShadowedByTheLog(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	keys(a, "/")
	typeInto(a, "jkq5G")
	if got := string(a.find.buf); got != "jkq5G" {
		t.Errorf("buffer = %q", got)
	}
}
