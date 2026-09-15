package ui

import (
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
)

// TestSetRefusesStartupSettings: a :set that is accepted and then does nothing
// is worse than one that says why.
func TestSetRefusesStartupSettings(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	before := a.cfg.Str("block-size")

	a.exec(config.Command{Name: "set", Args: []string{"block-size", "4096"}}, 0)
	if got := a.cfg.Str("block-size"); got != before {
		t.Errorf("block-size = %s, want it left at %s", got, before)
	}
	if !strings.Contains(a.msg, "startup") {
		t.Errorf("message = %q, want it to say the setting is read at startup", a.msg)
	}

	a.exec(config.Command{Name: "set", Args: []string{"follow"}}, 0)
	if !strings.Contains(a.msg, "startup") {
		t.Errorf("toggling follow with :set: message = %q", a.msg)
	}
}

func TestSetStillReadsStartupSettings(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	a.exec(config.Command{Name: "set", Args: []string{"block-size"}}, 0)
	if !strings.Contains(a.msg, "block-size = ") {
		t.Errorf("message = %q, want the value", a.msg)
	}
}
