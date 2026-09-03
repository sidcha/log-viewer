package ui

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

type promptKind int

const (
	pSearchFwd promptKind = iota
	pSearchBack
	pOnly
	pNever
	pHighlight
	pSave
	pGotoLine
	pCommand
)

// prompt is the single-line editor that replaces the status row.
type prompt struct {
	kind  promptKind
	label string
	buf   []rune
	cur   int

	hist  *[]string
	hidx  int    // index into history; len(hist) means "the live buffer"
	stash []rune // the live buffer while browsing history
}

func (a *App) openPrompt(kind promptKind, label string, hist *[]string) {
	a.clearMsg()
	a.prompt = &prompt{kind: kind, label: label, hist: hist}
	if hist != nil {
		a.prompt.hidx = len(*hist)
	}
	if action, ok := previewKind(kind); ok {
		a.startPreview(action)
	}
}

func (a *App) closePrompt() {
	a.prompt = nil
	a.endPreview()
	a.scr.HideCursor()
}

func (a *App) promptKey(ev *tcell.EventKey) {
	p := a.prompt

	switch ev.Key() {
	case tcell.KeyEnter:
		text := string(p.buf)
		kind := p.kind
		// Carry the previewed colour through, so the rule ends up the colour
		// the user was just looking at.
		var color filter.Color
		if a.prev != nil {
			color = a.prev.color
		}
		if p.hist != nil && strings.TrimSpace(text) != "" {
			*p.hist = appendHistory(*p.hist, text)
		}
		a.closePrompt()
		a.acceptPrompt(kind, text, color)
		return

	case tcell.KeyEscape, tcell.KeyCtrlG:
		a.closePrompt()
		return

	case tcell.KeyLeft:
		if p.cur > 0 {
			p.cur--
		}
	case tcell.KeyRight:
		if p.cur < len(p.buf) {
			p.cur++
		}
	case tcell.KeyHome, tcell.KeyCtrlA:
		p.cur = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		p.cur = len(p.buf)

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if p.cur > 0 {
			p.buf = append(p.buf[:p.cur-1], p.buf[p.cur:]...)
			p.cur--
		}
	case tcell.KeyDelete, tcell.KeyCtrlD:
		if p.cur < len(p.buf) {
			p.buf = append(p.buf[:p.cur], p.buf[p.cur+1:]...)
		}
	case tcell.KeyCtrlU:
		p.buf = p.buf[:0]
		p.cur = 0
	case tcell.KeyCtrlK:
		p.buf = p.buf[:p.cur]
	case tcell.KeyCtrlW:
		p.deleteWord()

	case tcell.KeyUp, tcell.KeyCtrlP:
		p.history(-1)
	case tcell.KeyDown, tcell.KeyCtrlN:
		p.history(1)

	case tcell.KeyRune:
		p.buf = append(p.buf, 0)
		copy(p.buf[p.cur+1:], p.buf[p.cur:])
		p.buf[p.cur] = ev.Rune()
		p.cur++
	}

	a.updatePreview(string(p.buf))
}

func (p *prompt) deleteWord() {
	i := p.cur
	for i > 0 && p.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && p.buf[i-1] != ' ' {
		i--
	}
	p.buf = append(p.buf[:i], p.buf[p.cur:]...)
	p.cur = i
}

func (p *prompt) history(delta int) {
	if p.hist == nil || len(*p.hist) == 0 {
		return
	}
	h := *p.hist
	if p.hidx == len(h) && delta < 0 {
		p.stash = append(p.stash[:0], p.buf...)
	}
	n := p.hidx + delta
	if n < 0 {
		n = 0
	}
	if n > len(h) {
		n = len(h)
	}
	p.hidx = n
	if n == len(h) {
		p.buf = append(p.buf[:0], p.stash...)
	} else {
		p.buf = append(p.buf[:0], []rune(h[n])...)
	}
	p.cur = len(p.buf)
}

// appendHistory keeps the most recent entries, most recent last, without
// duplicating the previous one.
func appendHistory(h []string, s string) []string {
	if len(h) > 0 && h[len(h)-1] == s {
		return h
	}
	h = append(h, s)
	const limit = 100
	if len(h) > limit {
		h = h[len(h)-limit:]
	}
	return h
}

func (a *App) drawPrompt(row, w int) {
	p := a.prompt
	st := a.cfg.Style("prompt")
	if a.prev != nil && a.prev.err != nil {
		st = a.cfg.Style("status.error")
	}

	text := p.label + string(p.buf)
	a.fillRow(row, text, st)
	if a.prev != nil {
		a.putStr(len([]rune(text)), row, a.prev.summary(), st)
	}

	cx := len([]rune(p.label)) + p.cur
	if cx >= w {
		cx = w - 1
	}
	a.scr.ShowCursor(cx, row)
}

// acceptPrompt performs the action the prompt was opened for.
func (a *App) acceptPrompt(kind promptKind, text string, color filter.Color) {
	trimmed := strings.TrimSpace(text)

	switch kind {
	case pSearchFwd, pSearchBack:
		if trimmed == "" {
			return
		}
		a.doSearch(trimmed, kind == pSearchBack)

	case pOnly:
		if trimmed == "" {
			return
		}
		a.addRule(filter.Only, trimmed, color)

	case pNever:
		if trimmed == "" {
			return
		}
		a.addRule(filter.Never, trimmed, color)

	case pHighlight:
		if trimmed == "" {
			return
		}
		a.addRule(filter.Highlight, trimmed, color)

	case pSave:
		if trimmed == "" {
			return
		}
		a.startSave(trimmed)

	case pGotoLine:
		if trimmed == "" {
			return
		}
		n, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			a.errorf("not a line number: %s", trimmed)
			return
		}
		a.gotoSourceLine(n)

	case pCommand:
		if trimmed == "" {
			return
		}
		words, err := config.ParseLine(trimmed)
		if err != nil {
			a.errorf("%v", err)
			return
		}
		cmd, err := config.ParseCommand(words)
		if err != nil {
			a.errorf("%v", err)
			return
		}
		a.exec(cmd)
	}
}
