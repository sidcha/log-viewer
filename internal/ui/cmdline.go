package ui

import (
	"strconv"
	"strings"

	"github.com/sidcha/log-viewer/internal/config"
)

// runCommandLine executes one line typed at the `:` prompt.
//
// It accepts the lv command vocabulary and its vim aliases, and a bare line
// number, which in vim jumps there.
func (a *App) runCommandLine(text string) {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), ":"))
	if text == "" {
		return
	}

	// :42 jumps to line 42.
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		a.gotoSourceLine(n)
		return
	}
	// :$ goes to the end, as it does in vim.
	if text == "$" {
		a.exec(config.Command{Name: "goto-end"}, 0)
		return
	}

	words, err := config.ParseLine(text)
	if err != nil {
		a.errorf("%v", err)
		return
	}
	if len(words) == 0 {
		return
	}
	cmd, err := config.ParseCommand(words)
	if err != nil {
		a.errorf("%v", err)
		return
	}
	a.exec(cmd, 0)
}
