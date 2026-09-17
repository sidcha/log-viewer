package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/ansi"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/view"
)

// draw paints one frame. Cost is proportional to what is on screen, never to
// the size of the file.
func (a *App) draw() {
	w, h := a.scr.Size()
	if w <= 0 || h <= 0 {
		return
	}
	a.cols = w
	a.curX, a.curY = -1, -1

	// The finder takes the bottom of the screen and brings its own status, so
	// the log simply gets fewer rows while it is open.
	findH := 0
	if a.find != nil {
		findH = a.finderHeight(h)
	}
	status := findH == 0 && a.showStatus()

	rows := h - findH
	if status {
		rows--
	}
	if rows < 1 {
		rows = 1
	}
	a.rows = rows

	a.resolveAnchor()

	base := a.cfg.Style("default")
	v := a.activeView()
	n := v.Len()
	a.lastLen = n
	a.lastScan, _ = v.Progress()

	if a.follow {
		a.pinToTail()
	}

	gw := a.gutterWidth()

	row := 0
	for i := a.top; row < rows && i < n; i++ {
		ln, err := v.At(i, a.linebuf[:0])
		if err != nil {
			break
		}
		a.linebuf = ln.Text // keep the grown buffer for the next line
		row += a.drawLine(row, rows-row, gw, i, ln, base)
	}
	for ; row < rows; row++ {
		a.clearRow(row, base)
	}

	if status {
		a.drawStatus(h-1, w)
	}
	if findH > 0 {
		a.drawFinder(h-findH, w, findH)
	}
	if a.ov != nil {
		a.ov.draw(a, w, h)
	}
	a.placeCursor()
	a.scr.Show()
}

// placeCursor puts the terminal cursor where the column actually is.
//
// Without it the column is invisible state: h, l, w, b, e and $ move something
// with nothing to show for it, char-wise visual anchors somewhere unknowable,
// and * can only ever read whatever column the cursor was left on. A tinted
// line says which row; it cannot say which character.
func (a *App) placeCursor() {
	switch {
	case a.find != nil:
		return // the finder put it on its own input line
	case a.prompt != nil || a.confirm != nil:
		return // the prompt put it on the status row
	case a.ov != nil || a.curX < 0:
		a.scr.HideCursor()
	default:
		a.scr.ShowCursor(a.curX, a.curY)
	}
}

// trackCursor records where on screen the cursor column landed, while the line
// holding it is being laid out and its columns are known.
func (a *App) trackCursor(row, gw, maxCols int, text []byte) {
	col := displayColumn(text, a.curCol, a.opts().TabWidth)

	x, y := gw+col-a.hoff, row
	if a.opts().Wrap && maxCols > 0 {
		x, y = gw+col%maxCols, row+col/maxCols
	}
	if x < gw || x >= a.cols || y < 0 || y >= a.rows {
		return // scrolled out of view
	}
	a.curX, a.curY = x, y
}

// statusLagBytes is how far behind indexing or filtering must fall before the
// automatic status line appears.
//
// Testing "not caught up" alone would flicker: a followed log is momentarily
// behind on every append, and a status line that appears and disappears shifts
// the whole screen by a row each time. A threshold means the bar shows for the
// initial pass over a large file, where it is useful, and stays out of the way
// while tailing, where it is not.
const statusLagBytes = 4 << 20

func (a *App) showStatus() bool {
	switch a.opts().StatusLine {
	case "on":
		return true
	case "off":
		return a.prompt != nil || a.confirm != nil
	default:
		if a.prompt != nil || a.confirm != nil || a.liveMsg() || a.busy != nil || a.pendingKeys() != "" {
			return true
		}
		scanned, total := a.activeView().Progress()
		return total-scanned > statusLagBytes
	}
}

func (a *App) gutterWidth() int {
	if !a.opts().LineNumbers {
		return 0
	}
	digits := len(strconv.FormatInt(maxInt64(a.idx.Lines(), 1), 10))
	if digits < 3 {
		digits = 3
	}
	return digits + len([]rune(a.opts().Gutter))
}

// drawLine renders one logical line and returns how many terminal rows it used.
func (a *App) drawLine(row, avail, gw int, idx int64, ln view.Line, base tcell.Style) int {
	maxCols := a.cols - gw
	if maxCols < 1 {
		maxCols = 1
	}

	budget := a.hoff + maxCols
	if a.opts().Wrap {
		budget = maxCols * avail
	}
	a.lay.build(ln.Text, budget)

	text := ln.Text
	if a.lay.used < len(text) {
		text = text[:a.lay.used]
	}

	rowBase := base
	if bg, ok := a.cursorTint(idx); ok {
		// The band has to cover the whole row, not just the characters. Tinting
		// only the text leaves it ending raggedly wherever the line happens to
		// stop, which reads as a smudge rather than a cursor line.
		rowBase = base.Background(bg)
	}

	a.applyStyles(text, idx, ln.Off, base)
	a.markCursorLine(idx)
	a.markSelection(idx, len(ln.Text))
	if idx == a.cursorLine() {
		a.trackCursor(row, gw, maxCols, ln.Text)
	}

	if !a.opts().Wrap {
		a.paintRow(row, gw, a.hoff, maxCols, rowBase, true, ln)
		return 1
	}

	used := (a.lay.cols() + maxCols - 1) / maxCols
	if used < 1 {
		used = 1
	}
	if used > avail {
		used = avail
	}
	for r := 0; r < used; r++ {
		a.paintRow(row+r, gw, r*maxCols, maxCols, rowBase, r == 0, ln)
	}
	return used
}

// flatStyles paints every cell of the laid-out line in one style.
func (a *App) flatStyles(st tcell.Style) {
	n := a.lay.cols()
	if cap(a.styles) < n {
		a.styles = make([]tcell.Style, n, n*2+16)
	}
	a.styles = a.styles[:n]
	for i := range a.styles {
		a.styles[i] = st
	}
}

// applyStyles paints the style array for the laid-out line, in the order of
// who gets the last word: lv's guess at the shape of the line, then the
// colours the line was written with, then the rules the user added, then the
// search.
func (a *App) applyStyles(text []byte, idx, off int64, base tcell.Style) {
	a.flatStyles(base)
	n := a.lay.cols()

	a.paintSpans(a.eng.SyntaxSpansAt(highlight.LineKey{Gen: a.idx.Gen(), Off: off, Len: len(text)}, text), n)
	a.paintANSI(text, n)
	a.paintSpans(a.eng.MarkSpans(text), n)

	if a.search.m != nil {
		st := a.cfg.Style("match")
		if idx == a.search.cur {
			st = a.cfg.Style("match.current")
		}
		a.paint(a.search.m, text, st, n)
	}
}

func (a *App) paintSpans(spans []highlight.Span, n int) {
	for _, sp := range spans {
		cs, ce := a.lay.cellRange(sp.Start, sp.End)
		for k := cs; k < ce && k < n; k++ {
			a.styles[k] = sp.Style
		}
	}
}

// paintANSI puts back the colours the line arrived with.
//
// They are laid over what is already there rather than replacing it, because a
// sequence that asks only for bold has said nothing about colour, and the
// bytes it does not cover keep whatever lv decided for them.
func (a *App) paintANSI(text []byte, n int) {
	if !a.opts().ANSI {
		return
	}
	a.ansiSpans = ansi.Spans(text, a.ansiSpans[:0])
	for _, sp := range a.ansiSpans {
		cs, ce := a.lay.cellRange(sp.Start, sp.End)
		for k := cs; k < ce && k < n; k++ {
			a.styles[k] = sp.Attrs.Apply(a.styles[k])
		}
	}
}

// cursorTint reports the background for the row the cursor is on.
func (a *App) cursorTint(idx int64) (tcell.Color, bool) {
	if !a.opts().CursorLine || idx != a.cursorLine() || a.visualActive() {
		return tcell.ColorDefault, false
	}
	_, bg, _ := a.cfg.Style("cursor.line").Decompose()
	return bg, bg != tcell.ColorDefault
}

// markCursorLine tints the text on the cursor's row.
//
// Cells that already carry a background belong to a rule or a match, and those
// keep it: overwriting them would drop the colour that says why the line is on
// screen, and would leave dark text sitting on the tint.
func (a *App) markCursorLine(idx int64) {
	bg, ok := a.cursorTint(idx)
	if !ok {
		return
	}
	for i := range a.styles {
		if _, cur, _ := a.styles[i].Decompose(); cur == tcell.ColorDefault {
			a.styles[i] = a.styles[i].Background(bg)
		}
	}
}

// markSelection tints the selected part of a line.
func (a *App) markSelection(idx int64, length int) {
	start, end, ok := a.selectedRange(idx, length)
	if !ok {
		return
	}
	st := a.cfg.Style("selection")
	cs, ce := a.lay.cellRange(start, end)
	for k := cs; k < ce && k < len(a.styles); k++ {
		a.styles[k] = st
	}
	if a.sel.mode == visualLine {
		// A line-wise selection covers the row, not just its text.
		for k := ce; k < len(a.styles); k++ {
			a.styles[k] = st
		}
	}
}

// paint applies a style to every place m matches within text.
func (a *App) paint(m filter.Matcher, text []byte, st tcell.Style, n int) {
	a.locs = m.Find(text, a.locs[:0], a.opts().MaxHighlights)
	for _, l := range a.locs {
		cs, ce := a.lay.cellRange(l[0], l[1])
		for k := cs; k < ce && k < n; k++ {
			a.styles[k] = st
		}
	}
}

// paintRow writes one terminal row from the laid-out cells starting at column
// startCol.
func (a *App) paintRow(row, gw, startCol, maxCols int, base tcell.Style, first bool, ln view.Line) {
	x := 0
	if gw > 0 {
		sep := a.opts().Gutter
		gstyle := a.cfg.Style("gutter")
		if _, bg, _ := base.Decompose(); bg != tcell.ColorDefault {
			gstyle = gstyle.Background(bg)
		}
		if first {
			num := strconv.FormatInt(ln.Num, 10)
			pad := gw - len([]rune(sep)) - len(num)
			for ; pad > 0; pad-- {
				a.scr.SetContent(x, row, ' ', nil, gstyle)
				x++
			}
			x = a.drawText(x, row, num, gstyle)
		} else {
			for ; x < gw-len([]rune(sep)); x++ {
				a.scr.SetContent(x, row, ' ', nil, gstyle)
			}
		}
		x = a.drawText(x, row, sep, gstyle)
	}

	limit := x + maxCols
	c := startCol
	for x < limit && c < a.lay.cols() {
		r := a.lay.cells[c]
		st := a.styles[c]
		if r == 0 {
			// The left half of a wide rune is scrolled off; show a placeholder.
			a.scr.SetContent(x, row, ' ', nil, st)
			x++
			c++
			continue
		}
		a.scr.SetContent(x, row, r, nil, st)
		cw := int(a.lay.width[c])
		if cw < 1 {
			cw = 1
		}
		x += cw
		c += cw
	}
	for ; x < limit; x++ {
		a.scr.SetContent(x, row, ' ', nil, base)
	}

	// Mark content that continues past the right edge, or a line longer than
	// the retention cap, so nothing is silently hidden.
	if a.cols > 0 {
		switch {
		case ln.Truncated:
			a.scr.SetContent(a.cols-1, row, '…', nil, a.cfg.Style("truncated"))
		case !a.opts().Wrap && c < a.lay.cols():
			a.scr.SetContent(a.cols-1, row, '›', nil, a.cfg.Style("gutter"))
		}
	}
}

func (a *App) clearRow(row int, st tcell.Style) {
	for x := 0; x < a.cols; x++ {
		a.scr.SetContent(x, row, ' ', nil, st)
	}
}

func (a *App) drawText(x, row int, s string, st tcell.Style) int {
	for _, r := range s {
		if x >= a.cols {
			break
		}
		a.scr.SetContent(x, row, r, nil, st)
		x++
	}
	return x
}

// fill draws s at row, padding to width with the same style.
func (a *App) fillRow(row int, s string, st tcell.Style) {
	x := a.drawText(0, row, s, st)
	for ; x < a.cols; x++ {
		a.scr.SetContent(x, row, ' ', nil, st)
	}
}

// ---------------------------------------------------------------- status line

func (a *App) drawStatus(row, w int) {
	if a.confirm != nil {
		a.drawConfirm(row, w)
		return
	}
	if a.prompt != nil {
		a.drawPrompt(row, w)
		return
	}

	style := a.cfg.Style("status")
	if a.busy != nil {
		style = a.cfg.Style("status.busy")
	}

	var text string
	switch {
	case a.liveMsg():
		style = a.msgStyle
		text = a.msg
	case a.busy != nil:
		text = a.busyText()
	default:
		text = a.stateText()
	}
	a.fillRow(row, text, style)
}

func (a *App) busyText() string {
	b := a.busy
	done, total := b.done.Load(), b.total.Load()
	pct := ""
	if total > 0 {
		pct = fmt.Sprintf(" %d%%", clampPct(done*100/total))
	}
	return fmt.Sprintf(" %s%s … Esc to cancel", b.what, pct)
}

// stateText is the normal readout: where you are, and what is filtering or
// indexing behind your back.
func (a *App) stateText() string {
	v := a.activeView()
	n := v.Len()
	pos := a.top + 1
	if n == 0 {
		pos = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, " %s  %s/%s", filepath.Base(a.src.Name()), commas(pos), commas(n))
	if n > 0 {
		fmt.Fprintf(&b, " (%d%%)", clampPct((a.top+int64(a.rows))*100/n))
	}

	var flags []string
	switch a.sel.mode {
	case visualChar:
		flags = append(flags, "VISUAL")
	case visualLine:
		flags = append(flags, "VISUAL LINE")
	}
	if a.follow {
		flags = append(flags, "FOLLOW")
	}
	if a.filt != nil {
		f := fmt.Sprintf("FILTER×%d", a.rules.Len()-a.rules.Count(filter.Highlight))
		if a.bypass {
			f = "FILTER OFF"
		}
		flags = append(flags, f)
	}
	if n := a.rules.Count(filter.Highlight); n > 0 {
		flags = append(flags, fmt.Sprintf("HL×%d", n))
	}
	if a.opts().Wrap {
		flags = append(flags, "WRAP")
	}
	if len(flags) > 0 {
		fmt.Fprintf(&b, "  [%s]", strings.Join(flags, " "))
	}

	if scanned, total := v.Progress(); !v.Done() && total > 0 {
		what := "indexing"
		if a.filtered() {
			what = "filtering"
		}
		fmt.Fprintf(&b, "  %s %d%%", what, clampPct(scanned*100/total))
	}
	if err := a.idx.Err(); err != nil {
		fmt.Fprintf(&b, "  read error: %v", err)
	}
	if pk := a.pendingKeys(); pk != "" {
		fmt.Fprintf(&b, "   %s", pk)
	}
	return b.String()
}

// -------------------------------------------------------------------- helpers

func clampPct(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// commas formats a number with thousands separators, which matters when the
// counts routinely run to ten digits.
func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(s[:lead])
	for i := lead; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
