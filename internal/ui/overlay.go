package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

type overlayKind int

const (
	ovHelp overlayKind = iota
	ovRules
	ovBuffers
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
	swatch color.RGB
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
	// itemRows caches items; a panel's rows do not change once it is built.
	itemRows []int
	// typed is a buffer number typed so far, in the buffer list.
	typed int
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
	if o.itemRows == nil {
		o.itemRows = []int{}
		for i, r := range o.rows {
			if r.item >= 0 {
				o.itemRows = append(o.itemRows, i)
			}
		}
	}
	return o.itemRows
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

// drawPanel paints the overlay as a bordered panel over the log.
func (o *overlay) drawPanel(a *App, w, h int) {
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
		var swatch color.RGB
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

		if swatch != (color.RGB{}) {
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

// selectedRule is the rule the rule list's selection is on, or -1.
func (a *App) selectedRule() int {
	if a.ov == nil || a.ov.kind != ovRules {
		return -1
	}
	return a.ov.selected()
}

// updateRuleList applies a change made from the rule list and redraws it.
func (a *App) updateRuleList(next *filter.Set) {
	a.applyRules(next, true)
	a.showRules()
}

// ------------------------------------------------------------ overlay builders

func (a *App) showHelp() {
	o := &overlay{kind: ovHelp, title: "lv — key bindings", hint: a.panelHint("overlay", "page", "panel-page-down", "panel-page-up", "close", "panel-close")}

	// The log's own keys, grouped by what they do. Visual mode's are folded
	// in, marked, since it is the log with a selection.
	groups := map[string][]config.Bind{}
	for _, table := range []string{"normal", "visual"} {
		for _, b := range a.cfg.Binds(table) {
			g := "other"
			if spec, ok := config.LookupCommand(b.Cmd.Name); ok {
				g = spec.Group
			}
			groups[g] = append(groups[g], b)
		}
	}
	for _, g := range []string{"motion", "search", "filter", "view", "select", "file", "other"} {
		o.helpSection(strings.ToUpper(g), groups[g])
	}

	// Every other mode, straight from its table, so a rebinding shows here.
	for _, t := range config.Tables {
		if t.Name == "normal" || t.Name == "visual" {
			continue
		}
		o.helpSection(strings.ToUpper(t.Help), a.cfg.Binds(t.Name))
	}

	o.rows = append(o.rows,
		ovRow{text: "", item: -1},
		ovRow{text: "READING THE KEYS", style: "overlay.title", item: -1},
		ovRow{text: "  Two letters together are a sequence: gg is g then g.", style: "overlay.dim", item: -1},
		ovRow{text: "  <C-d> is Control and d.  <M-d> is Alt and d, or Escape then d.", style: "overlay.dim", item: -1},
		ovRow{text: "  Every key is rebound by name: bind -T <table> <key> <command>.", style: "overlay.dim", item: -1},
		ovRow{text: "", item: -1},
		ovRow{text: "COUNTS AND SEQUENCES", style: "overlay.title", item: -1},
		ovRow{text: "  Type a number before a motion to repeat it: 5j, 12k, 42G.", style: "overlay.dim", item: -1},
		ovRow{text: "  : opens the command line; Tab completes command names.", style: "overlay.dim", item: -1},
		ovRow{text: "", item: -1},
		ovRow{text: "THE MATCH WINDOW", style: "overlay.title", item: -1},
		ovRow{text: "  f / ? & ! and m open a window listing what the pattern matches.", style: "overlay.dim", item: -1},
		ovRow{text: "  Only f matches loosely; the rest match the text exactly as typed.", style: "overlay.dim", item: -1},
		ovRow{text: "  f just goes there and leaves nothing behind; / also lights up", style: "overlay.dim", item: -1},
		ovRow{text: "  every match for n and N, and ? makes them step backwards.", style: "overlay.dim", item: -1},
		ovRow{text: "  f / and ? carry on from the top when the matches below run out.", style: "overlay.dim", item: -1},
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
	if a.eng.Profiles != nil {
		if d := a.eng.Profiles.Describe(a.cursorOffset()); d != "" {
			o.rows = append(o.rows, ovRow{text: "  syntax profile: " + d, style: "overlay.dim", item: -1})
		}
	}

	a.ov = o
}

// cursorOffset is the byte offset of the line the cursor is on.
func (a *App) cursorOffset() int64 {
	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		return 0
	}
	return ln.Off
}

// helpSection lists bindings under a heading, collapsing the keys bound to
// one command onto one line.
func (o *overlay) helpSection(title string, binds []config.Bind) {
	if len(binds) == 0 {
		return
	}
	o.rows = append(o.rows,
		ovRow{text: "", item: -1},
		ovRow{text: title, style: "overlay.title", item: -1})

	byCmd := map[string][]string{}
	visualOnly := map[string]bool{}
	var seen []string
	for _, b := range binds {
		k := b.Cmd.String()
		if _, ok := byCmd[k]; !ok {
			seen = append(seen, k)
			visualOnly[k] = true
		}
		visualOnly[k] = visualOnly[k] && b.Table == "visual"
		byCmd[k] = append(byCmd[k], b.Spec)
	}
	for _, cmd := range seen {
		help := ""
		if spec, ok := config.LookupCommand(strings.Fields(cmd)[0]); ok {
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

// panelHint spells out a panel's keys from the table's bindings, so a
// rebinding shows: each label is followed by the commands it describes.
func (a *App) panelHint(table string, pairs ...string) string {
	var parts []string
	for i := 0; i < len(pairs); {
		label := pairs[i]
		i++
		var keys []string
		for ; i < len(pairs) && strings.Contains(pairs[i], "-"); i++ {
			if k := a.cfg.KeysFor(table, pairs[i]); len(k) > 0 {
				keys = append(keys, k[0])
			}
		}
		if len(keys) > 0 {
			parts = append(parts, strings.Join(keys, "/")+" "+label)
		}
	}
	return strings.Join(parts, "  ")
}

func (a *App) showRules() {
	o := &overlay{
		kind:  ovRules,
		title: "rules",
		hint: a.panelHint("rules", "on/off", "rules-toggle", "edit", "rules-edit", "action", "rules-cycle-action",
			"recolour", "rules-recolour", "delete", "rules-delete", "close", "panel-close"),
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
