package ui

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/export"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/view"
)

// maxHOff bounds horizontal scrolling so a stray key repeat cannot walk the
// viewport off into the distance.
const maxHOff = 1 << 16

// ApplyStartup installs rules given on the command line.
func (a *App) ApplyStartup(only, never, marks []string) error {
	set := a.rules
	add := func(action filter.Action, pats []string, flag string) error {
		for _, p := range pats {
			m, err := filter.Compile(p, a.cfg.Case())
			if err != nil {
				return fmt.Errorf("%s %q: %w", flag, p, err)
			}
			set = set.Add(filter.Rule{Action: action, M: m, Enabled: true})
		}
		return nil
	}
	if err := add(filter.Only, only, "--only"); err != nil {
		return err
	}
	if err := add(filter.Never, never, "--never"); err != nil {
		return err
	}
	if err := add(filter.Highlight, marks, "--highlight"); err != nil {
		return err
	}
	a.applyRules(set, false)
	return nil
}

// exec runs one command. Every interactive action arrives here, whether from a
// key binding or the `:` prompt.
func (a *App) exec(cmd config.Command) {
	step := int64(a.cfg.Int("scroll-step"))
	if step < 1 {
		step = 1
	}
	hstep := a.cfg.Int("h-scroll-step")
	if hstep < 1 {
		hstep = 8
	}

	switch cmd.Name {
	case "nop":
	case "quit":
		a.quit = true
	case "cancel":
		a.cancel()
	case "help":
		a.showHelp()
	case "command":
		a.openPrompt(pCommand, ":", &a.histCmd)
	case "redraw":
		a.scr.Sync()

	// ---------------------------------------------------------------- motion
	case "up":
		a.follow = false
		a.moveBy(-a.argN(cmd, step))
	case "down":
		a.moveBy(a.argN(cmd, step))
	case "left":
		a.hoff -= int(a.argN(cmd, int64(hstep)))
		if a.hoff < 0 {
			a.hoff = 0
		}
	case "right":
		a.hoff += int(a.argN(cmd, int64(hstep)))
		if a.hoff > maxHOff {
			a.hoff = maxHOff
		}
	case "column-reset":
		a.hoff = 0
	case "page-up":
		a.follow = false
		a.moveBy(-a.pageStep())
	case "page-down":
		a.moveBy(a.pageStep())
	case "half-page-up":
		a.follow = false
		a.moveBy(-int64(a.contentRows() / 2))
	case "half-page-down":
		a.moveBy(int64(a.contentRows() / 2))
	case "goto-start":
		a.follow = false
		a.setTop(0)
	case "goto-end":
		a.gotoEnd()
	case "goto-line":
		if len(cmd.Args) == 1 {
			n, err := strconv.ParseInt(cmd.Args[0], 10, 64)
			if err != nil {
				a.errorf("not a line number: %s", cmd.Args[0])
				return
			}
			a.gotoSourceLine(n)
			return
		}
		a.openPrompt(pGotoLine, "goto line: ", nil)

	// ------------------------------------------------------------------ view
	case "follow-toggle":
		a.follow = !a.follow
		if a.follow {
			a.gotoEnd()
			a.infof("following %s", shortName(a.src.Name()))
		} else {
			a.infof("follow off")
		}
	case "wrap-toggle":
		a.wrap = !a.wrap
		a.hoff = 0
		a.infof("wrap %s", onOff(a.wrap))
	case "line-numbers-toggle":
		a.lineNums = !a.lineNums
	case "status-toggle":
		if a.statusOn == "on" {
			a.statusOn = "off"
		} else {
			a.statusOn = "on"
		}
	case "reload":
		a.reload()

	// ---------------------------------------------------------------- search
	case "search-forward":
		if len(cmd.Args) == 1 {
			a.doSearch(cmd.Args[0], false)
			return
		}
		a.openPrompt(pSearchFwd, "/", &a.histPattern)
	case "search-backward":
		if len(cmd.Args) == 1 {
			a.doSearch(cmd.Args[0], true)
			return
		}
		a.openPrompt(pSearchBack, "?", &a.histPattern)
	case "search-next":
		a.searchStep(false)
	case "search-prev":
		a.searchStep(true)
	case "search-clear":
		a.search = searchState{cur: -1}
		a.infof("search cleared")

	// ----------------------------------------------------------------- rules
	case "filter-only":
		a.ruleCommand(cmd, filter.Only, pOnly, "show only: ")
	case "filter-never":
		a.ruleCommand(cmd, filter.Never, pNever, "never show: ")
	case "highlight":
		a.ruleCommand(cmd, filter.Highlight, pHighlight, "highlight: ")

	case "filter-pop":
		if a.rules.Len() == 0 {
			a.infof("no rules")
			return
		}
		a.applyRules(a.rules.Pop(), true)
		a.infof("%d rule(s) remaining", a.rules.Len())
	case "filter-clear":
		if a.rules.Len() == 0 {
			a.infof("no rules")
			return
		}
		a.applyRules(a.rules.Clear(), true)
		a.infof("rules cleared")
	case "highlight-clear":
		if a.rules.Count(filter.Highlight) == 0 {
			a.infof("no highlights")
			return
		}
		a.applyRules(a.rules.ClearAction(filter.Highlight), true)
		a.infof("highlights cleared")
	case "filter-bypass":
		if a.filt == nil {
			a.infof("nothing is being filtered")
			return
		}
		anchor := a.anchorSource()
		a.bypass = !a.bypass
		a.seekSource(anchor)
		a.infof("filters %s", onOff(!a.bypass))
	case "filter-list":
		a.showRules()

	// ------------------------------------------------------------------ file
	case "save-view":
		if len(cmd.Args) == 1 {
			a.startSave(cmd.Args[0])
			return
		}
		a.openPrompt(pSave, "save to: ", &a.histPath)

	default:
		a.errorf("unimplemented command %q", cmd.Name)
	}
}

// ruleCommand adds a rule directly when given a pattern, and otherwise opens
// the prompt so the pattern can be previewed before it is committed.
func (a *App) ruleCommand(cmd config.Command, action filter.Action, kind promptKind, label string) {
	if len(cmd.Args) == 1 {
		a.addRule(action, cmd.Args[0], filter.Color{})
		return
	}
	a.openPrompt(kind, label, &a.histPattern)
}

func (a *App) argN(cmd config.Command, def int64) int64 {
	if len(cmd.Args) == 0 {
		return def
	}
	n, err := strconv.ParseInt(cmd.Args[0], 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func (a *App) pageStep() int64 {
	n := int64(a.contentRows() - a.cfg.Int("page-overlap"))
	if n < 1 {
		n = 1
	}
	return n
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// cancel unwinds whatever is in front of the user, innermost first.
func (a *App) cancel() {
	switch {
	case a.busy != nil:
		a.cancelBusy()
		a.infof("cancelled")
	case a.ov != nil:
		a.ov = nil
	case a.search.m != nil:
		a.search = searchState{cur: -1}
	default:
		a.clearMsg()
	}
}

func (a *App) reload() {
	a.cancelBusy()
	a.idx.Reset()
	a.top = 0
	a.search.cur = -1
	a.infof("re-indexing %s", shortName(a.src.Name()))
}

// ------------------------------------------------------------------- rules

// addRule compiles a pattern and appends it. color may be zero, in which case
// one is assigned.
func (a *App) addRule(action filter.Action, pattern string, color filter.Color) {
	m, err := filter.Compile(pattern, a.cfg.Case())
	if err != nil {
		a.errorf("bad pattern: %v", err)
		return
	}
	a.applyRules(a.rules.Add(filter.Rule{
		Action:  action,
		M:       m,
		Enabled: true,
		Color:   color,
	}), true)

	switch action {
	case filter.Only:
		a.infof("showing only lines matching %s", pattern)
	case filter.Never:
		a.infof("hiding lines matching %s", pattern)
	default:
		a.infof("highlighting %s", pattern)
	}
}

// applyRules swaps in a new rule set.
//
// The filter scan is only restarted when the filtering behaviour actually
// changed. Adding a highlight to a set that is already filtering leaves the
// scan alone, which on a large file is the difference between instant and
// minutes.
func (a *App) applyRules(next *filter.Set, preserve bool) {
	sameFilter := a.rules != nil && a.rules.Fingerprint() == next.Fingerprint()
	a.rules = next
	a.syncMarks()

	if sameFilter && (a.filt != nil) == next.Filtering() {
		return
	}

	var anchor int64
	if preserve {
		anchor = a.anchorSource()
	}

	if a.filt != nil {
		a.filt.Close()
		a.filt = nil
	}
	a.bypass = false
	a.gen++
	a.search.cur = -1
	a.top = 0

	if next.Filtering() {
		a.filt = view.NewFiltered(
			a.src, a.idx, next,
			a.cfg.Int("block-size"),
			a.cfg.Int("max-line-bytes"),
			a.cfg.Dur("scan-poll"),
			a.Wake,
		)
	}
	if preserve {
		a.seekSource(anchor)
	}
}

// syncMarks rebuilds what the renderer paints. Rules that hide lines have
// nothing to mark; rules that show or highlight them do, so it is visible which
// rule caught which line.
func (a *App) syncMarks() {
	marks := a.eng.Marks[:0]
	for _, r := range a.rules.All() {
		if !r.Enabled || r.Action == filter.Never {
			continue
		}
		marks = append(marks, highlight.Mark{M: r.M, Style: a.styleFor(r.Color)})
	}
	a.eng.Marks = marks
}

// seekSource asks to place source line `src` (0-based) at the top of the
// screen. When the filtered scan has not yet reached that point the request is
// held and retried as results arrive.
func (a *App) seekSource(src int64) {
	a.pendingAnchor = src
	a.havePending = true
	a.resolveAnchor()
}

func (a *App) resolveAnchor() {
	if !a.havePending {
		return
	}
	if a.follow {
		a.havePending = false
		return
	}
	if !a.filtered() {
		a.havePending = false
		a.setTop(a.pendingAnchor)
		return
	}
	f := a.filt
	if f.SourceLinesScanned() <= a.pendingAnchor && !f.Done() {
		return // not scanned far enough yet; try again next frame
	}
	if i, ok := f.IndexForSource(a.pendingAnchor); ok {
		a.setTop(i)
	} else {
		a.setTop(f.Len() - 1)
	}
	a.havePending = false
}

func (a *App) gotoSourceLine(n int64) {
	if n < 1 {
		n = 1
	}
	a.follow = false
	a.seekSource(n - 1)
	if !a.havePending {
		return
	}
	a.infof("seeking to line %s …", commas(n))
}

// -------------------------------------------------------------------- search

func (a *App) doSearch(pattern string, back bool) {
	m, err := filter.Compile(pattern, a.cfg.Case())
	if err != nil {
		a.errorf("bad pattern: %v", err)
		return
	}
	a.search = searchState{m: m, pat: pattern, back: back, cur: -1}

	from := a.top
	if back {
		from = a.top + int64(a.contentRows()) - 1
		if n := a.viewLen(); from >= n {
			from = n - 1
		}
	}
	a.startSearch(from, back)
}

func (a *App) searchStep(reverse bool) {
	if a.search.m == nil {
		a.errorf("no search pattern")
		return
	}
	back := a.search.back != reverse

	from := a.search.cur
	if from < 0 {
		from = a.top
	}
	if back {
		from--
	} else {
		from++
	}
	if from < 0 {
		a.infof("start of file")
		return
	}
	a.startSearch(from, back)
}

func (a *App) startSearch(from int64, back bool) {
	a.cancelBusy()

	ctx, cancel := context.WithCancel(context.Background())
	op := &busyOp{what: "searching for " + a.search.pat, cancel: cancel}
	a.busy = op

	v := a.activeView()
	gen := a.gen
	m := a.search.m
	pat := a.search.pat

	go func() {
		hit, found, err := view.Search(ctx, v, from, m, back, func(done, total int64) {
			op.done.Store(done)
			op.total.Store(total)
			a.Wake()
		})
		a.postFn(func() {
			if a.busy == op {
				a.busy = nil
			}
			if gen != a.gen {
				return // the view was replaced while we searched
			}
			switch {
			case ctx.Err() != nil:
				// cancelled; cancel() already said so
			case err != nil:
				a.errorf("search: %v", err)
			case !found:
				a.infof("pattern not found: %s", pat)
			default:
				a.search.cur = hit
				a.reveal(hit)
			}
		})
	}()
}

// ------------------------------------------------------------------- export

func (a *App) startSave(path string) {
	a.cancelBusy()

	ctx, cancel := context.WithCancel(context.Background())
	op := &busyOp{what: "saving to " + path, cancel: cancel}
	a.busy = op

	v := a.activeView()
	src := a.src

	go func() {
		p, err := export.Save(ctx, src, v, path, func(pr export.Progress) {
			op.done.Store(pr.Scanned)
			op.total.Store(pr.Total)
			a.Wake()
		})
		a.postFn(func() {
			if a.busy == op {
				a.busy = nil
			}
			switch {
			case ctx.Err() != nil:
				a.infof("save cancelled")
			case err != nil:
				a.errorf("save: %v", err)
			default:
				a.infof("wrote %s lines (%s) to %s", commas(p.Lines), humanBytes(p.Bytes), path)
			}
		})
	}()
}
