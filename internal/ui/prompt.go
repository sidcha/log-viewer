package ui

import (
	"sort"
	"strconv"
	"strings"

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
	lineEdit
	kind  promptKind
	label string

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
	a.prompt = &prompt{lineEdit: newLineEdit(hist), kind: kind, label: label}
}

func (a *App) closePrompt() {
	a.prompt = nil
	a.scr.HideCursor()
}

// acceptLine runs what was typed at the prompt.
func (a *App) acceptLine() {
	p := a.prompt
	text := p.text()
	if p.hist != nil && strings.TrimSpace(text) != "" {
		*p.hist = appendHistory(*p.hist, text, a.opts().HistorySize)
	}
	kind, then := p.kind, p.then
	a.closePrompt()
	a.acceptPrompt(kind, text, then)
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
	for alias := range config.Aliases {
		add(alias)
	}
	sort.Strings(out)
	return out
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
		a.startSaveView(a.activeView(), config.ExpandHome(trimmed), then)

	case pSaveStream:
		a.startSaveView(a.base, config.ExpandHome(trimmed), then)

	case pSaveRules:
		if a.saveRules(config.ExpandHome(trimmed)) && then != nil {
			then()
		}

	case pLoadRules:
		a.loadRulesAt(config.ExpandHome(trimmed))

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
