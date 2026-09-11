package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

type overlayKind int

const (
	ovHelp overlayKind = iota
	ovRules
)

// ovRow is one line of an overlay. item is the index into the underlying list
// for interactive overlays, or -1 for headings and blank lines.
type ovRow struct {
	text  string
	style string
	item  int
	dim   bool
	// swatch, when set, is drawn as a colour block before the text so each
	// rule's colour is identifiable in the list.
	swatch filter.Color
}

type overlay struct {
	kind  overlayKind
	title string
	hint  string
	rows  []ovRow
	top   int
	sel   int
	// body is how many rows the panel showed last time it was drawn, so a
	// page key moves by what is actually on screen.
	body int
}

// page is how far Space and its counterparts move.
func (o *overlay) page() int {
	if o.body > 1 {
		return o.body - 1
	}
	return 1
}

func (o *overlay) scroll(n int) {
	o.top += n
	if o.top < 0 {
		o.top = 0
	}
	if o.top >= len(o.rows) {
		o.top = len(o.rows) - 1
	}
	if o.top < 0 {
		o.top = 0
	}
}

// items lists the row indices that can be selected.
func (o *overlay) items() []int {
	var out []int
	for i, r := range o.rows {
		if r.item >= 0 {
			out = append(out, i)
		}
	}
	return out
}

func (o *overlay) moveSel(delta int) {
	items := o.items()
	if len(items) == 0 {
		o.scroll(delta)
		return
	}
	o.sel += delta
	if o.sel < 0 {
		o.sel = 0
	}
	if o.sel >= len(items) {
		o.sel = len(items) - 1
	}
}

// selected returns the item index the cursor is on.
func (o *overlay) selected() int {
	items := o.items()
	if o.sel < 0 || o.sel >= len(items) {
		return -1
	}
	return o.rows[items[o.sel]].item
}

func (o *overlay) selectedRow() int {
	items := o.items()
	if o.sel < 0 || o.sel >= len(items) {
		return -1
	}
	return items[o.sel]
}

// draw paints the overlay as a bordered panel over the log.
func (o *overlay) draw(a *App, w, h int) {
	if w < 8 || h < 5 {
		return
	}
	x0, y0 := 1, 0
	x1, y1 := w-2, h-2
	if y1 <= y0 {
		return
	}

	st := a.cfg.Style("overlay")
	title := a.cfg.Style("overlay.title")

	inner := x1 - x0 - 1
	body := y1 - y0 - 1
	o.body = body

	drawBox(a.scr, x0, y0, x1, y1, st)
	a.putStr(x0+2, y0, " "+o.title+" ", title)
	if o.hint != "" {
		hint := o.hint
		if len(hint) > inner {
			hint = hint[:inner]
		}
		a.putStr(x1-1-len([]rune(hint)), y1, " "+hint+" ", a.cfg.Style("overlay.dim"))
	}

	if o.top > len(o.rows)-body {
		o.top = len(o.rows) - body
	}
	if o.top < 0 {
		o.top = 0
	}
	// Keep the selection visible.
	if r := o.selectedRow(); r >= 0 {
		if r < o.top {
			o.top = r
		}
		if r >= o.top+body {
			o.top = r - body + 1
		}
	}

	selRow := o.selectedRow()
	for i := 0; i < body; i++ {
		y := y0 + 1 + i
		idx := o.top + i
		rowStyle := st
		text := ""
		var swatch filter.Color
		if idx < len(o.rows) {
			r := o.rows[idx]
			text = r.text
			swatch = r.swatch
			if r.style != "" {
				rowStyle = a.cfg.Style(r.style)
			}
			if r.dim {
				rowStyle = a.cfg.Style("overlay.dim")
			}
			if idx == selRow {
				rowStyle = rowStyle.Reverse(true)
			}
		}
		if len([]rune(text)) > inner {
			text = string([]rune(text)[:inner])
		}
		text += strings.Repeat(" ", inner-len([]rune(text)))
		a.putStr(x0+1, y, text, rowStyle)

		if swatch != (filter.Color{}) {
			a.putStr(x0+1, y, "  ", a.styleFor(swatch))
		}
	}
}

func drawBox(scr tcell.Screen, x0, y0, x1, y1 int, st tcell.Style) {
	for x := x0 + 1; x < x1; x++ {
		scr.SetContent(x, y0, '─', nil, st)
		scr.SetContent(x, y1, '─', nil, st)
	}
	for y := y0 + 1; y < y1; y++ {
		scr.SetContent(x0, y, '│', nil, st)
		scr.SetContent(x1, y, '│', nil, st)
	}
	scr.SetContent(x0, y0, '╭', nil, st)
	scr.SetContent(x1, y0, '╮', nil, st)
	scr.SetContent(x0, y1, '╰', nil, st)
	scr.SetContent(x1, y1, '╯', nil, st)
}

func (a *App) putStr(x, y int, s string, st tcell.Style) {
	for _, r := range s {
		if x >= a.cols {
			return
		}
		if x >= 0 {
			a.scr.SetContent(x, y, r, nil, st)
		}
		x++
	}
}

// overlayKey handles a key while an overlay is open.
//
// It always reports the key as consumed. Letting an unrecognised key through
// meant the log scrolled about behind the help, and a stray `/` opened a search
// prompt under a panel covering it. A panel on screen owns the keyboard; q and
// Escape close it.
func (a *App) overlayKey(ev *tcell.EventKey) bool {
	o := a.ov

	switch ev.Key() {
	case tcell.KeyEscape:
		a.ov = nil
	case tcell.KeyUp:
		o.moveSel(-1)
	case tcell.KeyDown:
		o.moveSel(1)
	case tcell.KeyPgUp, tcell.KeyCtrlB:
		o.scroll(-o.page())
	case tcell.KeyPgDn, tcell.KeyCtrlF:
		o.scroll(o.page())
	case tcell.KeyHome:
		o.scroll(-len(o.rows))
	case tcell.KeyEnd:
		o.scroll(len(o.rows))
	case tcell.KeyEnter:
		if o.kind == ovRules {
			if i := o.selected(); i >= 0 {
				a.applyRules(a.rules.Toggle(i), true)
				a.showRules()
			}
		}
	case tcell.KeyRune:
		a.overlayRune(o, ev.Rune(), ev.Modifiers())
	}
	return true
}

// overlayRune dispatches a printable key inside a panel.
//
// The rules list claims a handful of letters for editing, so those are taken
// first; everything else is panel navigation, shared by every overlay.
func (a *App) overlayRune(o *overlay, r rune, mod tcell.ModMask) {
	if o.kind == ovRules && a.rulesListRune(o, r) {
		return
	}

	switch r {
	case 'q':
		a.ov = nil
	case 'k':
		o.moveSel(-1)
	case 'j':
		o.moveSel(1)
	case 'g':
		o.scroll(-len(o.rows))
	case 'G':
		o.scroll(len(o.rows))
	case 'b':
		o.scroll(-o.page())
	case ' ':
		// Shift-Space pages back, on terminals that report it.
		if mod&tcell.ModShift != 0 {
			o.scroll(-o.page())
			return
		}
		o.scroll(o.page())
	}
}

// rulesListRune handles the keys that edit rules, reporting whether it took
// the key.
func (a *App) rulesListRune(o *overlay, r rune) bool {
	switch r {
	case ' ':
		if i := o.selected(); i >= 0 {
			a.applyRules(a.rules.Toggle(i), true)
			a.showRules()
		}
	case 'd', 'x':
		if i := o.selected(); i >= 0 {
			a.applyRules(a.rules.Remove(i), true)
			a.showRules()
		}
	case 'e', 'i', 'c':
		if i := o.selected(); i >= 0 {
			a.ov = nil
			a.editRule(i)
		}
	case 'a':
		if i := o.selected(); i >= 0 {
			a.cycleRuleAction(i)
		}
	case 'r':
		if i := o.selected(); i >= 0 {
			a.recolourRule(i)
		}
	case 'A':
		a.applyRules(a.rules.SetAll(true), true)
		a.showRules()
	case 'D':
		a.applyRules(a.rules.SetAll(false), true)
		a.showRules()
	default:
		return false
	}
	return true
}

// ------------------------------------------------------------ overlay builders

func (a *App) showHelp() {
	o := &overlay{kind: ovHelp, title: "lv — key bindings", hint: "Space/b page  j/k line  g/G ends  Esc close"}

	groups := map[string][]config.Bind{}
	for _, b := range append(append([]config.Bind{}, a.cfg.Binds()...), a.cfg.VisualBinds()...) {
		spec, ok := config.LookupCommand(b.Cmd.Name)
		g := "other"
		if ok {
			g = spec.Group
		}
		groups[g] = append(groups[g], b)
	}

	order := []string{"motion", "search", "filter", "highlight", "view", "file", "other"}
	for _, g := range order {
		binds := groups[g]
		if len(binds) == 0 {
			continue
		}
		o.rows = append(o.rows, ovRow{text: "", item: -1})
		o.rows = append(o.rows, ovRow{text: strings.ToUpper(g), style: "overlay.title", item: -1})

		// Collapse keys bound to the same command onto one line.
		byCmd := map[string][]string{}
		visualOnly := map[string]bool{}
		var seen []string
		for _, b := range binds {
			if b.Visual {
				visualOnly[b.Cmd.String()] = true
			} else {
				delete(visualOnly, b.Cmd.String())
			}
			k := b.Cmd.String()
			if _, ok := byCmd[k]; !ok {
				seen = append(seen, k)
			}
			byCmd[k] = append(byCmd[k], b.Spec)
		}
		for _, cmd := range seen {
			help := ""
			name := strings.Fields(cmd)[0]
			if spec, ok := config.LookupCommand(name); ok {
				help = spec.Help
			}
			if visualOnly[cmd] {
				help = "(while selecting) " + help
			}
			o.rows = append(o.rows, ovRow{
				text: fmt.Sprintf("  %-16s %-22s %s", strings.Join(byCmd[cmd], " "), cmd, help),
				item: -1,
			})
		}
	}

	o.rows = append(o.rows,
		ovRow{text: "", item: -1},
		ovRow{text: "READING THE KEYS", style: "overlay.title", item: -1},
		ovRow{text: "  Two letters together are a sequence: gg is g then g.", style: "overlay.dim", item: -1},
		ovRow{text: "  <C-d> is Control and d.  <M-d> is Alt and d, or Escape then d.", style: "overlay.dim", item: -1},
		ovRow{text: "", item: -1},
		ovRow{text: "COUNTS AND SEQUENCES", style: "overlay.title", item: -1},
		ovRow{text: "  Type a number before a motion to repeat it: 5j, 12k, 42G.", style: "overlay.dim", item: -1},
		ovRow{text: "  : opens the command line; Tab completes command names.", style: "overlay.dim", item: -1},
		ovRow{text: "", item: -1},
		ovRow{text: "THE MATCH WINDOW", style: "overlay.title", item: -1},
		ovRow{text: "  f / ? & ! and m open a window listing what the pattern matches.", style: "overlay.dim", item: -1},
		ovRow{text: "  Up/Down pick a match, Tab goes to it, Enter applies, Esc cancels.", style: "overlay.dim", item: -1},
		ovRow{text: "  <C-r> reads the pattern as a regular expression (PCRE) instead.", style: "overlay.dim", item: -1},
		ovRow{text: "  <C-p> and <C-n> recall patterns typed earlier.", style: "overlay.dim", item: -1},
		ovRow{text: "  f and / match loosely; & ! and m match the text exactly as typed.", style: "overlay.dim", item: -1},
		ovRow{text: "  f just goes there and leaves nothing behind; / also lights up", style: "overlay.dim", item: -1},
		ovRow{text: "  every match for n and N. f carries on from the top of the file.", style: "overlay.dim", item: -1},
		ovRow{text: "", item: -1},
		ovRow{text: "CONFIG", style: "overlay.title", item: -1},
	)
	files := a.cfg.Files()
	o.rows = append(o.rows, ovRow{text: "  read: " + strings.Join(files, ", "), style: "overlay.dim", item: -1})
	for _, lvl := range config.Layers() {
		if path, err := config.Path(lvl); err == nil {
			o.rows = append(o.rows, ovRow{
				text:  fmt.Sprintf("  %-7s %s", lvl, path),
				style: "overlay.dim",
				item:  -1,
			})
		}
	}
	if a.eng.Profile != nil {
		o.rows = append(o.rows, ovRow{text: "  syntax profile: " + a.eng.Profile.Name, style: "overlay.dim", item: -1})
	}

	a.ov = o
}

func (a *App) showRules() {
	o := &overlay{
		kind:  ovRules,
		title: "rules",
		hint:  "Space on/off  e edit  a action  r recolour  d delete  Esc close",
	}
	if a.ov != nil && a.ov.kind == ovRules {
		o.sel = a.ov.sel
	}

	rules := a.rules.All()
	if len(rules) == 0 {
		o.rows = append(o.rows, ovRow{
			text:  "  no rules yet -- & only, ! never, m highlight",
			style: "overlay.dim",
			item:  -1,
		})
	}
	for i, r := range rules {
		mark := "on "
		if !r.Enabled {
			mark = "off"
		}
		// How the pattern is read changes what it matches, so a rule that is
		// not plain text says so.
		pattern := r.M.Pattern()
		if m := r.M.Mode(); m != filter.Literal {
			pattern = fmt.Sprintf("%s  [%s]", pattern, m)
		}
		o.rows = append(o.rows, ovRow{
			text:   fmt.Sprintf("  %s  %-9s %s", mark, r.Action, pattern),
			dim:    !r.Enabled,
			swatch: r.Color,
			item:   i,
		})
	}

	// Spell out how they combine. Several only rules are or-ed, which is not
	// something anyone should have to work out by experiment.
	if onlys, nevers := a.rules.Count(filter.Only), a.rules.Count(filter.Never); onlys+nevers > 0 {
		var how string
		switch {
		case onlys > 1 && nevers > 0:
			how = fmt.Sprintf("a line is shown if it matches any of the %d only rules and none of the %d never rules", onlys, nevers)
		case onlys > 1:
			how = fmt.Sprintf("a line is shown if it matches any of the %d only rules", onlys)
		case onlys == 1 && nevers > 0:
			how = "a line is shown if it matches the only rule and none of the never rules"
		case onlys == 1:
			how = "a line is shown if it matches the only rule"
		default:
			how = "every line is shown except those matching a never rule"
		}
		o.rows = append(o.rows,
			ovRow{text: "", item: -1},
			ovRow{text: "  " + how, style: "overlay.dim", item: -1})
	}

	if a.filt != nil {
		kept, scanned := a.filt.Len(), a.filt.SourceLinesScanned()
		o.rows = append(o.rows, ovRow{
			text:  fmt.Sprintf("  %s of %s lines shown", commas(kept), commas(scanned)),
			style: "overlay.dim",
			item:  -1,
		})
	}
	a.ov = o
}
