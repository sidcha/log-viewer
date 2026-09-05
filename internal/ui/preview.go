package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/view"
)

// preview is what a rule would do, shown while its pattern is still being
// typed.
//
// Committing a filter over a very large log is a slow thing to undo, and a
// pattern that looked right often is not. Evaluating the candidate against the
// lines already on screen costs nothing and answers the question before the
// rule is applied: lines that would go are dimmed, matches are painted in the
// colour the rule is about to take.
type preview struct {
	act   promptAction
	m     filter.Matcher
	color filter.Color
	style tcell.Style
	err   error

	// counted over a bounded sample below the top of the screen
	matched int
	sampled int
}

// startPreview picks the colour the rule would take, so the preview shows the
// real thing rather than a placeholder.
func (a *App) startPreview(act promptAction) {
	a.prev = &preview{
		act:   act,
		color: filter.NextColor(a.rules.Colors()),
	}
	a.prev.style = a.styleFor(a.prev.color)
	a.updatePreview("")
}

func (a *App) endPreview() { a.prev = nil }

// updatePreview recompiles the candidate pattern and recounts what it would
// affect. It runs on every keystroke, so it stays bounded: compiling a pattern
// is microseconds, and the sample is a few hundred kilobytes at most.
func (a *App) updatePreview(pattern string) {
	if a.prev == nil {
		return
	}
	p := a.prev
	p.m, p.err = nil, nil
	p.matched, p.sampled = 0, 0

	if pattern == "" {
		return
	}
	m, err := filter.Compile(pattern, a.cfg.Case())
	if err != nil {
		p.err = err
		return
	}
	p.m = m

	limit := a.cfg.Int("preview-sample")
	if limit <= 0 {
		limit = 2000
	}
	v := a.activeView()
	_ = v.Iterate(a.top, func(_ int64, ln view.Line) bool {
		if m.Match(ln.Text) {
			p.matched++
		}
		p.sampled++
		return p.sampled < limit
	})
}

// previewVerdict reports how a line should be drawn under the active preview:
// whether the rule would remove it, and whether it matched.
func (p *preview) verdict(line []byte) (dropped, hit bool) {
	if p == nil || p.m == nil {
		return false, false
	}
	hit = p.m.Match(line)
	switch p.act {
	case actOnly:
		dropped = !hit
	case actNever:
		dropped = hit
	}
	return dropped, hit
}

// summary is the readout shown beside the prompt.
func (p *preview) summary() string {
	switch {
	case p == nil:
		return ""
	case p.err != nil:
		return "  ✗ " + p.err.Error()
	case p.m == nil:
		return ""
	}

	var verb string
	switch p.act {
	case actOnly:
		verb = "shown"
	case actNever:
		verb = "hidden"
	default:
		verb = "matched"
	}
	return "  " + commas(int64(p.matched)) + " of " + commas(int64(p.sampled)) + " " + verb
}
