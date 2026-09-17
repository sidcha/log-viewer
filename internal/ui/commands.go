package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
			m, err := filter.Compile(p, filter.Literal, a.opts().Case)
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
	if set == a.rules {
		return nil // nothing was given
	}
	a.applyRules(set, false)
	return nil
}

// setBool changes a boolean setting.
func (a *App) setBool(name string, value bool) {
	_ = a.cfg.Set(name, onOff(value))
	a.infof("%s %s", name, onOff(value))
}

// setOption implements :set, including vim's bare-name toggle for booleans.
func (a *App) setOption(args []string) {
	if len(args) == 0 {
		a.errorf("usage: set <name> [value]")
		return
	}
	name := args[0]
	spec, ok := config.LookupSetting(name)
	if !ok {
		a.errorf("unknown setting %q", name)
		return
	}
	changing := len(args) > 1 || spec.IsBool()
	if spec.Scope == config.Startup && changing {
		a.errorf("%s only takes effect at startup; set it in a config file or with --set", name)
		return
	}

	if len(args) == 1 {
		if !spec.IsBool() {
			a.infof("%s = %s", name, a.cfg.Str(name))
			return
		}
		a.setBool(name, !a.cfg.Bool(name))
		return
	}

	if err := a.cfg.Set(name, args[1]); err != nil {
		a.errorf("%v", err)
		return
	}
	a.opts()
	a.infof("%s = %s", name, a.cfg.Str(name))
}

// searchWordUnderCursor is * and #.
func (a *App) searchWordUnderCursor(back bool) {
	word := a.wordUnderCursor()
	if word == "" {
		a.errorf("no word under the cursor")
		return
	}
	// The word is taken exactly: * means this word, not something like it.
	a.doSearchMode(word, filter.Literal, back)
}

// ruleCommand adds a rule directly when given a pattern, and otherwise opens
// the window where the pattern's matches can be seen before it is committed.
func (a *App) ruleCommand(cmd config.Command, kind finderKind) {
	if len(cmd.Args) == 1 {
		action, _ := kind.rule()
		pattern, mode := splitModePrefix(cmd.Args[0], defaultMode(kind))
		a.addRuleMode(action, pattern, mode, filter.Color{})
		return
	}
	a.openFinder(kind, "")
}

// splitModePrefix reads an explicit mode off a pattern given on the command
// line, where there is no key to toggle one: `re:` asks for PCRE.
func splitModePrefix(pattern string, def filter.Mode) (string, filter.Mode) {
	if rest, ok := strings.CutPrefix(pattern, "re:"); ok {
		return rest, filter.Regex
	}
	return pattern, def
}

func (a *App) pageStep() int64 {
	n := int64(a.contentRows() - a.opts().PageOverlap)
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
	case a.confirm != nil:
		a.closeConfirm()
	case a.visualActive():
		a.endVisual()
	case len(a.pending) > 0 || a.count > 0:
		a.pending, a.count = a.pending[:0], 0
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
	a.infof("re-indexing %s", filepath.Base(a.src.Name()))
}

// ------------------------------------------------------------------- rules

// addRule appends a literal-match rule. color may be zero, in which case one
// is assigned.
func (a *App) addRule(action filter.Action, pattern string, color filter.Color) {
	a.addRuleMode(action, pattern, filter.Literal, color)
}

// addRuleMode appends a rule read in a given mode.
func (a *App) addRuleMode(action filter.Action, pattern string, mode filter.Mode, color filter.Color) {
	m, err := filter.Compile(pattern, mode, a.opts().Case)
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
// minutes. A change that can only hide more lines -- a never rule added, an
// only rule dropped -- restarts it, but the new scan skips every stretch where
// the old one found nothing.
func (a *App) applyRules(next *filter.Set, preserve bool) {
	prev := a.rules
	sameFilter := prev != nil && prev.Fingerprint() == next.Fingerprint()
	a.rules = next
	a.syncMarks()

	if sameFilter && (a.filt != nil) == next.Filtering() {
		return
	}

	var anchor int64
	if preserve {
		anchor = a.anchorSource()
	}

	var narrowing *view.Narrowing
	if a.filt != nil {
		a.filt.Close()
		if next.Narrows(prev) {
			narrowing = a.filt.Narrowing()
		}
		a.filt = nil
		a.filtPtr.Store(nil)
	}
	a.bypass = false
	a.gen++
	a.search.cur = -1
	a.top = 0

	if next.Filtering() {
		a.filt = view.NewFiltered(a.src, a.idx, next, view.FilteredOptions{
			Block:           a.opts().BlockSize,
			MaxLine:         a.opts().MaxLineBytes,
			Poll:            a.opts().ScanPoll,
			Advise:          a.opts().PageCacheAdvise,
			CheckpointBytes: int64(a.opts().FilterCkBytes),
			Notify:          a.Wake,
			Narrowing:       narrowing,
		})
		a.filtPtr.Store(a.filt)
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
		a.setCursor(a.pendingAnchor)
		return
	}
	f := a.filt
	if f.SourceLinesScanned() <= a.pendingAnchor && !f.Done() {
		return // not scanned far enough yet; try again next frame
	}
	if i, ok := f.IndexForSource(a.pendingAnchor); ok {
		a.setCursor(i)
	} else {
		a.setCursor(f.Len() - 1)
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

// doSearchAt installs a search and goes straight to a line already chosen in
// the finder, instead of scanning for the next match.
func (a *App) doSearchAt(pattern string, mode filter.Mode, back bool, num int64) {
	m, err := filter.Compile(pattern, mode, a.opts().Case)
	if err != nil {
		a.errorf("bad pattern: %v", err)
		return
	}
	a.search = searchState{m: m, pat: pattern, back: back, cur: -1}
	if num > 0 {
		a.gotoSourceLine(num)
		return
	}
	// Nothing was picked -- the window found no match, or was committed
	// before the scan reached one. Run the search itself rather than leaving
	// a highlight and no movement.
	a.startSearch(a.top, back)
}

// doSearch runs a search the way / does, for callers that have no mode of
// their own to offer.
func (a *App) doSearch(pattern string, back bool) {
	a.doSearchMode(pattern, defaultMode(findSearch), back)
}

func (a *App) doSearchMode(pattern string, mode filter.Mode, back bool) {
	m, err := filter.Compile(pattern, mode, a.opts().Case)
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
		// Already at the first line: there is nothing above to search.
		a.wrapSearch(back)
		return
	}
	a.startSearch(from, back)
}

func (a *App) startSearch(from int64, back bool) {
	a.searchFrom(from, back, false)
}

// searchFrom runs a search, carrying on from the other end of the file when it
// runs out.
//
// The window that collects a pattern lists matches from anywhere, so n and N
// have to be able to reach them; stopping at the end while the window plainly
// showed a match above would be the same lie in a different place. wrapped
// says this is already the second pass, so a pattern that is genuinely absent
// reports that instead of going round for ever.
func (a *App) searchFrom(from int64, back, wrapped bool) {
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
			case !found && !wrapped:
				a.wrapSearch(back)
			case !found:
				a.infof("pattern not found: %s", pat)
			default:
				a.search.cur = hit
				a.reveal(hit)
			}
		})
	}()
}

// wrapSearch starts again from the far end of the file, as vi does.
func (a *App) wrapSearch(back bool) {
	n := a.viewLen()
	if n == 0 {
		a.infof("pattern not found: %s", a.search.pat)
		return
	}
	if back {
		a.infof("search hit the start, continuing at the end")
		a.searchFrom(n-1, true, true)
		return
	}
	a.infof("search hit the end, continuing at the start")
	a.searchFrom(0, false, true)
}

// ------------------------------------------------------------------- export

func (a *App) startSave(path string) {
	a.startSaveView(a.activeView(), path, nil)
}

// startSaveView writes v to path in the background, running then once the
// bytes are there. A failed or cancelled write does not run it: whatever it
// was waiting to do, it was waiting for the file to exist.
func (a *App) startSaveView(v view.View, path string, then func()) {
	a.cancelBusy()

	ctx, cancel := context.WithCancel(context.Background())
	op := &busyOp{what: "saving to " + path, cancel: cancel}
	a.busy = op

	whole := v == view.View(a.base)
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
				if whole {
					// Everything that was read is now somewhere that outlives
					// this process, so quitting has nothing left to warn about.
					a.streamSaved = true
				}
				a.infof("wrote %s lines (%s) to %s", commas(p.Lines), humanBytes(p.Bytes), path)
				if then != nil {
					then()
				}
			}
		})
	}()
}
