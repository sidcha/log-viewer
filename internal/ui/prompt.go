package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
)

// The bottom row collects the things that are a single value rather than a
// search: a file path, a line number, a command. Patterns go through the
// finder window instead, which can show what they would match.
type promptKind int

const (
	pSave promptKind = iota
	pSaveStream
	pSaveRules
	pLoadRules
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

	// then runs after the prompt is accepted, used to quit once rules have
	// been written.
	then func()

	// completion state for the command line
	comps   []string
	compIdx int
	compAt  int
}

func (a *App) openPrompt(kind promptKind, label string, hist *[]string) {
	a.clearMsg()
	a.prompt = &prompt{kind: kind, label: label, hist: hist}
	if hist != nil {
		a.prompt.hidx = len(*hist)
	}
}

func (a *App) closePrompt() {
	a.prompt = nil
	a.scr.HideCursor()
}

func (a *App) promptKey(ev *tcell.EventKey) {
	p := a.prompt

	switch ev.Key() {
	case tcell.KeyEnter:
		text := string(p.buf)
		if p.hist != nil && strings.TrimSpace(text) != "" {
			*p.hist = appendHistory(*p.hist, text, a.opts().HistorySize)
		}
		kind, then := p.kind, p.then
		a.closePrompt()
		a.acceptPrompt(kind, text, then)
		return

	case tcell.KeyEscape, tcell.KeyCtrlG:
		a.closePrompt()
		return

	case tcell.KeyTab:
		if p.kind == pCommand {
			a.completeCommand(p)
		}
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
			p.comps = nil
		}
	case tcell.KeyDelete, tcell.KeyCtrlD:
		if p.cur < len(p.buf) {
			p.buf = append(p.buf[:p.cur], p.buf[p.cur+1:]...)
			p.comps = nil
		}
	case tcell.KeyCtrlU:
		p.buf, p.cur, p.comps = p.buf[:0], 0, nil
	case tcell.KeyCtrlK:
		p.buf, p.comps = p.buf[:p.cur], nil
	case tcell.KeyCtrlW:
		p.deleteWord()
		p.comps = nil

	case tcell.KeyUp, tcell.KeyCtrlP:
		p.history(-1)
	case tcell.KeyDown, tcell.KeyCtrlN:
		p.history(1)

	case tcell.KeyRune:
		p.buf = append(p.buf, 0)
		copy(p.buf[p.cur+1:], p.buf[p.cur:])
		p.buf[p.cur] = ev.Rune()
		p.cur++
		p.comps = nil
	}
}

// completeCommand fills in a command name from the first word, cycling through
// the candidates on repeated Tab presses.
func (a *App) completeCommand(p *prompt) {
	text := string(p.buf)

	if p.comps == nil {
		word := text
		if i := strings.IndexByte(text, ' '); i >= 0 {
			// Only the command name completes; arguments are free text.
			if p.cur > i {
				return
			}
			word = text[:i]
		}
		p.comps = commandCompletions(word)
		p.compAt = len(word)
		p.compIdx = 0
		if len(p.comps) == 0 {
			return
		}
	} else {
		p.compIdx = (p.compIdx + 1) % len(p.comps)
	}

	rest := ""
	if p.compAt <= len(text) {
		rest = text[p.compAt:]
	}
	chosen := p.comps[p.compIdx]
	p.buf = []rune(chosen + rest)
	p.cur = len([]rune(chosen))
}

// commandCompletions lists command names and aliases starting with prefix.
func commandCompletions(prefix string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if strings.HasPrefix(s, prefix) && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range config.Specs {
		add(s.Name)
	}
	for alias := range cmdAliases {
		add(alias)
	}
	sort.Strings(out)
	return out
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

// appendHistory keeps the most recent limit entries, most recent last,
// without duplicating the previous one.
func appendHistory(h []string, s string, limit int) []string {
	if len(h) > 0 && h[len(h)-1] == s {
		return h
	}
	h = append(h, s)
	if len(h) > limit {
		h = h[len(h)-limit:]
	}
	return h
}

func (a *App) drawPrompt(row, w int) {
	p := a.prompt
	st := a.cfg.Style("prompt")

	text := p.label + string(p.buf)
	a.fillRow(row, text, st)

	cx := len([]rune(p.label)) + p.cur
	if cx >= w {
		cx = w - 1
	}
	a.scr.ShowCursor(cx, row)
}

// acceptPrompt performs the action the prompt was opened for.
//
// then is what the prompt was opened on behalf of -- quitting, in every case
// so far -- and each action decides when it has earned it. A write runs in the
// background, so it must not be until the bytes are on disk: quitting the
// moment a path was typed would lose exactly what was being saved.
func (a *App) acceptPrompt(kind promptKind, text string, then func()) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}

	switch kind {
	case pSave:
		a.startSaveView(a.activeView(), expandPath(trimmed), then)

	case pSaveStream:
		a.startSaveView(a.base, expandPath(trimmed), then)

	case pSaveRules:
		if a.saveRules(expandPath(trimmed)) && then != nil {
			then()
		}

	case pLoadRules:
		a.loadRulesAt(expandPath(trimmed))

	case pGotoLine:
		n, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			a.errorf("not a line number: %s", trimmed)
			return
		}
		a.gotoSourceLine(n)

	case pCommand:
		a.runCommandLine(trimmed)
	}
}

// expandPath resolves a leading ~ so a path typed at the prompt behaves the
// way it would in a shell.
func expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
