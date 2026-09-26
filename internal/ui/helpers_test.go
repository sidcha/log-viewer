package ui

import (
	"time"

	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

// doSearch runs a search the way / does.
func (a *App) doSearch(pattern string, back bool) {
	a.doSearchMode(pattern, defaultMode(findSearch), back)
}

// addRule appends a literal-match rule. col may be zero, in which case one
// is assigned.
func (a *App) addRule(action filter.Action, pattern string, col color.RGB) {
	a.addRuleMode(action, pattern, filter.Literal, col)
}

// settle runs whatever background work has posted back, until it stops.
func settle(a *App) {
	for {
		select {
		case f := <-a.post:
			f()
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}
