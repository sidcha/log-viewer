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

	// set is the rule set that would be in force if the candidate were
	// committed. The verdict comes from this rather than from the candidate
	// alone, so what is dimmed is exactly what would go.
	set *filter.Set

	// counted over a bounded sample
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

	// While a further `only` pattern is being typed, lift the ones already in
	// force. They are or-ed, so the lines the new pattern would bring back are
	// exactly the ones currently hidden -- and a preview that cannot show them
	// is showing the wrong log. What survives is still marked by the verdict
	// below, so nothing is lost by widening the view to type against.
	if act == actOnly && a.rules.Count(filter.Only) > 0 {
		a.suspendOnly(true)
	}
	a.updatePreview("")
}

func (a *App) endPreview() {
	a.suspendOnly(false)
	a.prev = nil
}

// suspendOnly switches between the filtered view and the whole log, keeping
// the cursor on the same source line across the change.
func (a *App) suspendOnly(on bool) {
	if a.previewBase == on {
		return
	}
	src := a.anchorSource()
	a.previewBase = on
	if on {
		a.setCursor(src)
		return
	}
	a.seekSource(src)
}

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

	// The rule set as it would be once committed.
	if action, ok := p.act.rule(); ok && action != filter.Highlight {
		p.set = a.rules.Add(filter.Rule{Action: action, M: m, Enabled: true, Color: p.color})
	}

	limit := a.cfg.Int("preview-sample")
	if limit <= 0 {
		limit = 2000
	}

	_ = a.activeView().Iterate(a.top, func(_ int64, ln view.Line) bool {
		if p.keeps(ln.Text) {
			p.matched++
		}
		p.sampled++
		return p.sampled < limit
	})
}

// keeps reports whether a line would still be shown once the candidate is
// committed. For a highlight, which hides nothing, that is just a match.
func (p *preview) keeps(line []byte) bool {
	if p.set != nil {
		return p.set.Pass(line)
	}
	return p.m != nil && p.m.Match(line)
}

// previewVerdict reports how a line should be drawn under the active preview:
// whether the rule would remove it, and whether it matched.
func (p *preview) verdict(line []byte) (dropped, hit bool) {
	if p == nil || p.m == nil {
		return false, false
	}
	hit = p.m.Match(line)
	if p.set != nil {
		// Dim by the prospective set rather than by the candidate alone: a
		// line kept by a rule already in force stays, whether or not the
		// pattern being typed matches it.
		dropped = !p.set.Pass(line)
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

	verb := "matched"
	if p.set != nil {
		verb = "shown"
	}
	return "  " + commas(int64(p.matched)) + " of " + commas(int64(p.sampled)) + " " + verb
}
