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

	// widening is set when the candidate is an `only` rule and the view is
	// already restricted by one. Those are or-ed, so the new pattern can only
	// bring lines back, never take any away.
	widening bool

	// counted over a bounded sample
	matched int
	sampled int
}

// startPreview picks the colour the rule would take, so the preview shows the
// real thing rather than a placeholder.
func (a *App) startPreview(act promptAction) {
	a.prev = &preview{
		act:      act,
		color:    filter.NextColor(a.rules.Colors()),
		widening: act == actOnly && a.rules.Count(filter.Only) > 0,
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

	if p.widening {
		a.countAdditions(p, m, limit)
		return
	}

	_ = a.activeView().Iterate(a.top, func(_ int64, ln view.Line) bool {
		if m.Match(ln.Text) {
			p.matched++
		}
		p.sampled++
		return p.sampled < limit
	})
}

// countAdditions counts lines the candidate would bring back into a view that
// is already restricted.
//
// Sampling what is on screen would be useless here: those lines are shown
// already, so the answer would always be about what is visible rather than
// what is missing. The count has to come from the unfiltered log.
func (a *App) countAdditions(p *preview, m filter.Matcher, limit int) {
	from := a.topSourceLine()
	_ = a.base.Iterate(from, func(_ int64, ln view.Line) bool {
		if m.Match(ln.Text) && !a.rules.Pass(ln.Text) {
			p.matched++
		}
		p.sampled++
		return p.sampled < limit
	})
}

// topSourceLine is the source line number at the top of the screen, 0-based.
func (a *App) topSourceLine() int64 {
	ln, err := a.activeView().At(a.top, a.walkbuf[:0])
	if err != nil {
		return 0
	}
	a.walkbuf = ln.Text
	return ln.Num - 1
}

// previewVerdict reports how a line should be drawn under the active preview:
// whether the rule would remove it, and whether it matched.
func (p *preview) verdict(line []byte) (dropped, hit bool) {
	if p == nil || p.m == nil {
		return false, false
	}
	hit = p.m.Match(line)
	switch {
	case p.widening:
		// Or-ed with the rules already in force, so nothing on screen goes.
	case p.act == actOnly:
		dropped = !hit
	case p.act == actNever:
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

	if p.widening {
		return "  +" + commas(int64(p.matched)) + " more of " + commas(int64(p.sampled)) + " scanned"
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
