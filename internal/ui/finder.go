package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/view"
)

// The finder is the window that opens for &, !, m and /.
//
// It replaced an inline preview that could only describe the lines already on
// screen: a pattern matching something further down the file showed nothing at
// all, and the only feedback available was a count crammed against the cursor.
// Here the matches themselves are the feedback.
//
// The log above is left exactly as it is. Nothing is filtered, dimmed or moved
// until a rule is committed, so the window can be opened, typed into and
// abandoned without disturbing where you were reading.

// finderKind is which of the four operations the window was opened for.
type finderKind int

const (
	findOnly finderKind = iota
	findNever
	findHighlight
	findSearch
	findJump
)

func (k finderKind) String() string {
	switch k {
	case findOnly:
		return "only"
	case findNever:
		return "never"
	case findHighlight:
		return "highlight"
	case findJump:
		return "jump"
	default:
		return "search"
	}
}

// wraps reports whether running out of file means starting again from the top.
//
// Going somewhere wraps; making a rule does not. A rule applies to the whole
// file however it was found, so the matches below are a fair sample of it --
// but a search or a jump is about reaching a line, and the line wanted is as
// likely to be behind you. A window that answered "no matches" while the
// pattern was ten lines up, and then lit those matches up the moment it was
// committed, would be worse than no window at all.
func (k finderKind) wraps() bool { return k == findJump || k == findSearch }

// rule returns the rule action this window commits, for the three that make one.
func (k finderKind) rule() (filter.Action, bool) {
	switch k {
	case findOnly:
		return filter.Only, true
	case findNever:
		return filter.Never, true
	case findHighlight:
		return filter.Highlight, true
	}
	return filter.Only, false
}

// finderRow is one match.
type finderRow struct {
	num  int64 // source line number, 1-based
	text []byte
}

type finder struct {
	lineEdit
	kind finderKind

	m   filter.Matcher
	err error

	rows []finderRow
	sel  int
	top  int
	// height is how many result rows the window showed last time it was
	// drawn, which is also how many matches are worth looking for.
	height int

	// gen rises on every edit; a scan that finishes under an old one is stale.
	gen      int
	scanning bool
	atEnd    bool
	from     int64 // where the next scan resumes
	start    int64 // where this pattern's search began
	wrapped  bool  // the search has carried on from the top of the file
	scanned  int64
	cancel   context.CancelFunc

	// mode is how the typed pattern is read. Searching is fuzzy, because you
	// are reaching for a line you half remember; a rule is literal, because it
	// is saved and should mean what it says. C-r switches either to PCRE.
	mode filter.Mode

	// editing is the rule this window is rewriting, or -1.
	editing int

	// back is the direction a committed search steps in, for ?.
	back bool

	color color.RGB
	style tcell.Style
}

// finderLookahead is how many matches past the visible rows are gathered, so
// moving the selection down does not stall on a fresh scan.
const finderLookahead = 20

// defaultMode is how a pattern is read before C-r is pressed.
//
// Only the jump window is loose, because that is the whole point of it: you
// are reaching for a line from a half-remembered fragment. Everywhere else the
// pattern means the characters in it. A search that quietly matched anything
// with those letters somewhere in it would be a different tool than the one
// everybody reaches for / expecting.
func defaultMode(kind finderKind) filter.Mode {
	if kind == findJump {
		return filter.Fuzzy
	}
	return filter.Literal
}

// openFinder starts one of the four operations.
func (a *App) openFinder(kind finderKind, pattern string) {
	a.closeFinder()
	a.clearMsg()

	f := &finder{
		lineEdit: newLineEdit(&a.histPattern),
		kind:     kind,
		color:    filter.NextColor(a.rules.Colors()),
		height:   1,
		editing:  -1,
		mode:     defaultMode(kind),
	}
	f.style = a.styleFor(f.color)
	f.set(pattern)
	a.find = f
	a.restartFinder()
}

// openSearchFinder is / and ?, which differ only in the direction n and N
// will step once the pattern is committed. Both list the same matches: the
// window is about finding the line, not about which way to walk from it.
func (a *App) openSearchFinder(back bool) {
	a.openFinder(findSearch, "")
	a.find.back = back
}

func (a *App) closeFinder() {
	if a.find == nil {
		return
	}
	if a.find.cancel != nil {
		a.find.cancel()
	}
	a.find = nil
	a.scr.HideCursor()
}

// restartFinder recompiles the pattern and starts the search again from the
// reading position.
func (a *App) restartFinder() {
	f := a.find
	if f == nil {
		return
	}
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}

	f.gen++
	f.rows, f.sel, f.top = f.rows[:0], 0, 0
	f.scanning, f.atEnd, f.scanned, f.wrapped = false, false, 0, false
	f.m, f.err = nil, nil
	f.start = a.finderStart(f)
	f.from = f.start

	pattern := strings.TrimSpace(string(f.buf))
	if pattern == "" {
		return
	}
	m, err := filter.Compile(pattern, f.mode, a.opts().Case)
	if err != nil {
		f.err = err
		return
	}
	f.m = m
	a.scanFinder()
}

// finderStart is the line the search begins at: where you are reading.
func (a *App) finderStart(f *finder) int64 {
	if f.kind == findOnly {
		// An only rule brings lines back, so it searches past the filter --
		// but by source line, since that search runs over the whole log.
		return a.anchorSource()
	}
	return a.cursorLine()
}

// finderSource is what the search walks.
//
// Adding an only rule is a question about lines that are not currently shown,
// so it has to see the whole log. The other three are questions about what is
// in front of you, and searching lines you have already filtered away would
// only produce results you cannot act on.
func (a *App) finderSource(f *finder) view.View {
	if f.kind == findOnly {
		return a.base
	}
	return a.activeView()
}

// finderSkip drops lines the search should not offer.
//
// When widening with an only rule the never rules still apply: those lines
// were banished on purpose and should not come back just because a new
// pattern happens to match them.
func (a *App) finderSkip(f *finder) func([]byte) bool {
	if f.kind != findOnly || a.rules.Count(filter.Never) == 0 {
		return nil
	}
	nevers := a.rules.ClearAction(filter.Only)
	return func(line []byte) bool { return !nevers.Pass(line) }
}

// wanted is how many matches are worth having gathered by now.
func (f *finder) wanted() int {
	return f.top + f.height + finderLookahead
}

// scanFinder walks forward for more matches, stopping as soon as there are
// enough to fill the window.
//
// The scan is bounded by need rather than by the size of the file: with
// matches nearby it stops after a few hundred kilobytes. A pattern that
// matches nothing does run to the end, which is why it runs in the background
// and Escape cancels it.
func (a *App) scanFinder() {
	f := a.find
	if f == nil || f.m == nil || f.scanning || f.atEnd {
		return
	}
	want := f.wanted()
	if len(f.rows) >= want {
		return
	}

	f.scanning = true
	gen := f.gen
	from := f.from
	have := len(f.rows)
	m := f.m
	skip := a.finderSkip(f)
	src := a.finderSource(f)
	// On the pass that came back round to the top, the line the search began
	// at is where the candidates run out.
	stopAt := int64(-1)
	if f.wrapped {
		stopAt = f.start
	}

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel

	go func() {
		var (
			found     []finderRow
			next      = from
			scanned   int64
			stopped   bool // gave up early: filled the window or was cancelled
			exhausted bool // ran out of lines to offer
		)
		err := src.Iterate(from, func(i int64, ln view.Line) bool {
			if ctx.Err() != nil {
				stopped = true
				return false
			}
			if stopAt >= 0 && i >= stopAt {
				exhausted = true
				return false
			}
			next = i + 1
			scanned++
			if skip != nil && skip(ln.Text) {
				return true
			}
			if m.Match(ln.Text) {
				found = append(found, finderRow{
					num:  ln.Num,
					text: append([]byte(nil), ln.Text...),
				})
				if have+len(found) >= want {
					stopped = true
					return false
				}
			}
			return true
		})
		if !stopped && err == nil {
			exhausted = true
		}

		a.postFn(func() {
			cur := a.find
			if cur != f || gen != f.gen {
				return // the pattern moved on while this was running
			}
			f.scanning = false
			f.rows = append(f.rows, found...)
			f.from = next
			f.scanned += scanned
			if err != nil {
				// Reading failed; stop rather than asking again forever.
				f.err = err
				f.atEnd = true
				return
			}
			switch {
			case !exhausted:
				// Stopped short; there may well be more further on.
			case f.kind.wraps() && !f.wrapped && f.start > 0:
				f.wrapped = true
				f.from = 0
			default:
				f.atEnd = true
			}
			// Scrolling may have raised the target while this was in flight,
			// and a wrap leaves the rest of the file still to read.
			a.scanFinder()
		})
		a.Wake()
	}()
}

// selected is the highlighted row, or nil.
func (f *finder) selected() *finderRow {
	if f.sel < 0 || f.sel >= len(f.rows) {
		return nil
	}
	return &f.rows[f.sel]
}

// moveSel moves the selection, fetching more matches when it runs off the end.
func (a *App) moveSel(delta int) {
	f := a.find
	f.sel += delta
	if f.sel < 0 {
		f.sel = 0
	}
	if f.sel >= len(f.rows) {
		f.sel = len(f.rows) - 1
		if f.sel < 0 {
			f.sel = 0
		}
	}
	if f.sel < f.top {
		f.top = f.sel
	}
	if f.height > 0 && f.sel >= f.top+f.height {
		f.top = f.sel - f.height + 1
	}
	a.scanFinder()
}

// ---------------------------------------------------------------------- keys

// previewMatch goes to the selected match without giving up the window.
func (a *App) previewMatch() {
	if r := a.find.selected(); r != nil {
		a.gotoSourceLine(r.num)
	}
}

// toggleRegex switches between reading the pattern plainly and as PCRE. It
// has a key of its own, so every printable character stays typable -- * and ?
// are part of the syntax being toggled into.
func (a *App) toggleRegex() {
	f := a.find
	if f.mode == filter.Regex {
		f.mode = defaultMode(f.kind)
	} else {
		f.mode = filter.Regex
	}
	a.restartFinder()
}

// commitFinder applies what the window was opened for.
func (a *App) commitFinder() {
	f := a.find
	pattern := strings.TrimSpace(string(f.buf))
	if pattern == "" || f.m == nil {
		return
	}
	if f.hist != nil {
		*f.hist = appendHistory(*f.hist, pattern, a.opts().HistorySize)
	}

	kind, col, mode, editing, back := f.kind, f.color, f.mode, f.editing, f.back
	row := f.selected()
	var num int64
	if row != nil {
		num = row.num
	}
	a.closeFinder()

	if action, ok := kind.rule(); ok {
		if editing >= 0 {
			a.replaceRuleMode(editing, pattern, mode)
			return
		}
		a.addRuleMode(action, pattern, mode, col)
		return
	}

	if kind == findJump {
		// A jump leaves nothing behind: no highlight to clear afterwards, and
		// whatever n and N were stepping through is still theirs.
		if num > 0 {
			a.gotoSourceLine(num)
		}
		return
	}

	// A search: make it the pattern n and N step through, and go to the match
	// that was highlighted.
	a.doSearchAt(pattern, mode, back, num)
}

// ------------------------------------------------------------------ drawing

// finderHeight is how many rows the window takes, including its borders.
func (a *App) finderHeight(screen int) int {
	h := screen / 2
	if h < 6 {
		h = 6
	}
	if h > screen-2 {
		h = screen - 2
	}
	if h < 3 {
		h = 3
	}
	return h
}

// drawFinder paints the window across the bottom of the screen.
func (a *App) drawFinder(top, w, h int) {
	f := a.find
	st := a.cfg.Style("overlay")
	dim := a.cfg.Style("overlay.dim")

	bottom := top + h - 1
	drawBox(a.scr, 0, top, w-1, bottom, st)

	title := " " + f.kind.String() + " "
	if f.mode == filter.Regex {
		title = " " + f.kind.String() + " · PCRE "
	}
	a.putStr(2, top, title, a.cfg.Style("overlay.title"))

	if s := a.finderStatus(); s != "" {
		if x := w - 2 - len([]rune(s)) - 1; x > 2+len([]rune(title)) {
			a.putStr(x, top, " "+s+" ", dim)
		}
	}
	if s := a.rulesSummary(); s != "" {
		if x := w - 2 - len([]rune(s)) - 1; x > 2 {
			a.putStr(x, bottom, " "+s+" ", dim)
		}
	}

	// The input line.
	promptStyle := a.cfg.Style("prompt")
	if f.err != nil {
		promptStyle = a.cfg.Style("status.error")
	}
	inputRow := top + 1
	text := "> " + string(f.buf)
	a.putStr(1, inputRow, text+strings.Repeat(" ", maxInt(0, w-2-len([]rune(text)))), promptStyle)
	if f.err != nil {
		if msg := " ✗ " + f.err.Error(); len([]rune(text))+len([]rune(msg)) < w-2 {
			a.putStr(1+len([]rune(text)), inputRow, msg, promptStyle)
		}
	}

	f.height = maxInt(1, bottom-inputRow-1)
	a.drawFinderRows(inputRow+1, w, f.height, st, dim)

	a.scr.ShowCursor(1+2+f.cur, inputRow)
}

func (a *App) drawFinderRows(top, w, rows int, st, dim tcell.Style) {
	f := a.find
	gutter := len(fmt.Sprint(a.idx.Lines())) + 2

	for i := 0; i < rows; i++ {
		y := top + i
		idx := f.top + i

		for x := 1; x < w-1; x++ {
			a.scr.SetContent(x, y, ' ', nil, st)
		}
		if idx >= len(f.rows) {
			if idx == len(f.rows) && f.m != nil && !f.scanning {
				a.putStr(2, y, a.finderEmptyNote(), dim)
			}
			continue
		}

		row := f.rows[idx]
		rowStyle := st
		if f.kind == findNever {
			// These are the lines the rule would banish; show them as struck
			// out rather than merely listed.
			rowStyle = rowStyle.StrikeThrough(true).Dim(true)
		}
		if idx == f.sel {
			rowStyle = rowStyle.Reverse(true)
		}

		num := fmt.Sprintf("%*d ", gutter-1, row.num)
		a.putStr(1, y, num, dim)
		a.putFinderText(1+len(num), y, w-2-len(num), row, rowStyle)
	}
}

// putFinderText draws one matched line, marking where the pattern hit.
func (a *App) putFinderText(x, y, width int, row finderRow, base tcell.Style) {
	f := a.find
	a.lay.build(row.text, width)

	n := a.lay.cols()
	if cap(a.styles) < n {
		a.styles = make([]tcell.Style, n, n*2+16)
	}
	a.styles = a.styles[:n]
	for i := range a.styles {
		a.styles[i] = base
	}

	if f.m != nil {
		hit := f.style
		if base != (tcell.Style{}) && f.kind == findNever {
			hit = hit.StrikeThrough(true)
		}
		a.locs = f.m.Find(row.text[:minInt(len(row.text), a.lay.used)], a.locs[:0], a.opts().MaxHighlights)
		for _, l := range a.locs {
			cs, ce := a.lay.cellRange(l[0], l[1])
			for k := cs; k < ce && k < n; k++ {
				a.styles[k] = hit
			}
		}
	}

	for c := 0; c < n && c < width; c++ {
		r := a.lay.cells[c]
		if r == 0 {
			r = ' '
		}
		a.scr.SetContent(x+c, y, r, nil, a.styles[c])
	}
}

// finderStatus is the counter shown in the window's own frame, rather than
// against the cursor where it was in the way.
func (a *App) finderStatus() string {
	f := a.find
	switch {
	case f.m == nil:
		return ""
	case f.scanning && len(f.rows) == 0:
		return fmt.Sprintf("searching %s lines…", commas(f.scanned))
	case f.scanning:
		return fmt.Sprintf("%s so far…", commas(int64(len(f.rows))))
	case f.atEnd:
		return fmt.Sprintf("%s found", commas(int64(len(f.rows))))
	case f.wrapped:
		// Say so, or a list that runs 4,812 then 31 looks like a bug.
		return fmt.Sprintf("%s shown · wrapped", commas(int64(len(f.rows))))
	default:
		return fmt.Sprintf("%s shown", commas(int64(len(f.rows))))
	}
}

func (a *App) finderEmptyNote() string {
	f := a.find
	if len(f.rows) == 0 {
		if f.atEnd {
			if f.kind.wraps() {
				return "no matches anywhere in the file"
			}
			return "no matches below here"
		}
		return ""
	}
	if f.atEnd {
		if f.wrapped {
			return "— back where the search started —"
		}
		return "— end of file —"
	}
	return ""
}

// rulesSummary is the one-line reminder of what is already in force.
func (a *App) rulesSummary() string {
	only := a.rules.Count(filter.Only)
	never := a.rules.Count(filter.Never)
	hl := a.rules.Count(filter.Highlight)
	if only+never+hl == 0 {
		return ""
	}
	var parts []string
	if only > 0 {
		parts = append(parts, fmt.Sprintf("only %d", only))
	}
	if never > 0 {
		parts = append(parts, fmt.Sprintf("never %d", never))
	}
	if hl > 0 {
		parts = append(parts, fmt.Sprintf("hl %d", hl))
	}
	return strings.Join(parts, "  ") + "  (= to edit)"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// editRule reopens an existing rule in the finder, so changing a pattern shows
// what the new one catches exactly as writing it did.
func (a *App) editRule(i int) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	r := rules[i]

	kind := findHighlight
	switch r.Action {
	case filter.Only:
		kind = findOnly
	case filter.Never:
		kind = findNever
	}

	a.openFinder(kind, r.M.Pattern())
	a.find.editing = i
	a.find.color = r.Color
	a.find.style = a.styleFor(r.Color)
	a.find.mode = r.M.Mode()
	a.restartFinder()
}
