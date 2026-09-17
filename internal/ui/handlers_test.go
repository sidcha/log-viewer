package ui

import (
	"testing"

	"github.com/sidcha/log-viewer/internal/config"
)

// TestEveryCommandIsImplemented: a command declared but not implemented is
// only found when someone presses its key; one implemented but not declared
// can never be bound, typed or listed in the help.
func TestEveryCommandIsImplemented(t *testing.T) {
	for _, s := range config.Specs {
		if handlers[s.Name] == nil {
			t.Errorf("command %q is declared but has no handler", s.Name)
		}
	}
	for name := range handlers {
		if _, ok := config.LookupCommand(name); !ok {
			t.Errorf("handler %q implements no declared command", name)
		}
	}
}
