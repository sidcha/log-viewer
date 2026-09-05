package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

type promptKind int

const (
	pPattern promptKind = iota
	pSave
	pSaveRules
	pLoadRules
	pGotoLine
	pCommand
	pRuleEdit
)

// promptAction is what pressing Enter on a pattern prompt will do.
//
// Search, only, never and highlight are the same question asked four ways, so
// one prompt collects the pattern and Tab cycles what becomes of it. That is
// also why / previews: the search prompt is the pattern prompt, started on the
// action that only marks matches.
type promptAction int

const (
	actSearch promptAction = iota
	actOnly
	actNever
	actHighlight
)

var actionOrder = []promptAction{actSearch, actOnly, actNever, actHighlight}

func (p promptAction) String() string {
	switch p {
	case actOnly:
		return "only"
	case actNever:
		return "never"
	case actHighlight:
		return "highlight"
	default:
		return "search"
	}
}

// label is the prefix shown at the start of the prompt line.
func (p promptAction) label(back bool) string {
	switch p {
	case actOnly:
		return "only: "
	case actNever:
		return "never: "
	case actHighlight:
		return "highlight: "
	default:
		if back {
			return "?"
		}
		return "/"
	}
}

// rule returns the rule action, for the three that make one.
func (p promptAction) rule() (filter.Action, bool) {
	switch p {
	case actOnly:
		return filter.Only, true
	case actNever:
		return filter.Never, true
	case actHighlight:
		return filter.Highlight, true
	}
	return filter.Only, false
}

func nextAction(a promptAction) promptAction {
	for i, v := range actionOrder {
		if v == a {
			return actionOrder[(i+1)%len(actionOrder)]
		}
	}
	return actSearch
}

func prevAction(a promptAction) promptAction {
	for i, v := range actionOrder {
		if v == a {
			return actionOrder[(i-1+len(actionOrder))%len(actionOrder)]
		}
	}
	return actSearch
}

// prompt is the single-line editor that replaces the status row.
type prompt struct {
	kind  promptKind
	act   promptAction
	back  bool // searching backwards
	label string
	buf   []rune
	cur   int

	hist  *[]string
	hidx  int    // index into history; len(hist) means "the live buffer"
	stash []rune // the live buffer while browsing history

	// ruleIndex is the rule being edited, for pRuleEdit.
	ruleIndex int

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
	a.prompt = &prompt{kind: kind, label: label, hist: hist, ruleIndex: -1}
	if hist != nil {
		a.prompt.hidx = len(*hist)
	}
}

// openPattern opens the shared pattern prompt on a given action.
func (a *App) openPattern(act promptAction, back bool) {
	a.clearMsg()
	a.prompt = &prompt{
		kind:      pPattern,
		act:       act,
		back:      back,
		label:     act.label(back),
		hist:      &a.histPattern,
		hidx:      len(a.histPattern),
		ruleIndex: -1,
	}
	a.startPreview(act)
}

// openRuleEdit reopens an existing rule's pattern for editing, previewing the
// change against the current screen before it replaces the rule.
func (a *App) openRuleEdit(i int) {
	rules := a.rules.All()
	if i < 0 || i >= len(rules) {
		return
	}
	r := rules[i]

	act := actHighlight
	switch r.Action {
	case filter.Only:
		act = actOnly
	case filter.Never:
		act = actNever
	}

	a.clearMsg()
	p := &prompt{
		kind:      pRuleEdit,
		act:       act,
		label:     "edit " + r.Action.String() + ": ",
		buf:       []rune(r.M.Pattern()),
		hist:      &a.histPattern,
		hidx:      len(a.histPattern),
		ruleIndex: i,
	}
	p.cur = len(p.buf)
	a.prompt = p

	a.startPreview(act)
	// Keep the rule's own colour so the preview looks like what is on screen.
	a.prev.color = r.Color
	a.prev.style = a.styleFor(r.Color)
	a.updatePreview(string(p.buf))
}

func (a *App) closePrompt() {
	a.prompt = nil
	a.endPreview()
	a.scr.HideCursor()
}

func (a *App) promptKey(ev *tcell.EventKey) {
	p := a.prompt
	edited := true

	switch ev.Key() {
	case tcell.KeyEnter:
		text := string(p.buf)
		if p.hist != nil && strings.TrimSpace(text) != "" {
			*p.hist = appendHistory(*p.hist, text)
		}
		kind, act, back, idx := p.kind, p.act, p.back, p.ruleIndex
		then := p.then
		var color filter.Color
		if a.prev != nil {
			color = a.prev.color
		}
		a.closePrompt()
		if a.acceptPrompt(kind, act, back, idx, text, color) && then != nil {
			then()
		}
		return

	case tcell.KeyEscape, tcell.KeyCtrlG:
		a.closePrompt()
		return

	case tcell.KeyTab:
		switch p.kind {
		case pCommand:
			a.completeCommand(p)
		case pPattern:
			a.setPromptAction(p, nextAction(p.act))
		}
		return

	case tcell.KeyBacktab:
		if p.kind == pPattern {
			a.setPromptAction(p, prevAction(p.act))
		}
		return

	case tcell.KeyLeft:
		if p.cur > 0 {
			p.cur--
		}
		edited = false
	case tcell.KeyRight:
		if p.cur < len(p.buf) {
			p.cur++
		}
		edited = false
	case tcell.KeyHome, tcell.KeyCtrlA:
		p.cur = 0
		edited = false
	case tcell.KeyEnd, tcell.KeyCtrlE:
		p.cur = len(p.buf)
		edited = false

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

	default:
		edited = false
	}

	if edited {
		p.comps = nil
		a.updatePreview(string(p.buf))
	}
}

// setPromptAction changes what Enter will do, keeping the pattern already typed
// so a search can be promoted to a filter without retyping it.
func (a *App) setPromptAction(p *prompt, act promptAction) {
	p.act = act
	p.label = act.label(p.back)
	if a.prev != nil {
		a.prev.act = act
	}
	a.updatePreview(string(p.buf))
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
	if p.kind == pPattern {
		// A quiet reminder that Tab changes what Enter does.
		hint := "Tab: " + nextAction(p.act).String() + " "
		if x := w - len(hint); x > len([]rune(text))+len(a.prev.summary())+1 {
			a.putStr(x, row, hint, st.Dim(true))
		}
	}

	cx := len([]rune(p.label)) + p.cur
	if cx >= w {
		cx = w - 1
	}
	a.scr.ShowCursor(cx, row)
}

// acceptPrompt performs the action the prompt was opened for, reporting
// whether it succeeded so a follow-up action knows not to run.
func (a *App) acceptPrompt(kind promptKind, act promptAction, back bool, ruleIndex int, text string, color filter.Color) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	switch kind {
	case pPattern:
		if action, ok := act.rule(); ok {
			a.addRule(action, trimmed, color)
			return true
		}
		a.doSearch(trimmed, back)

	case pRuleEdit:
		a.replaceRule(ruleIndex, trimmed)

	case pSave:
		a.startSave(trimmed)

	case pSaveRules:
		return a.saveRules(expandPath(trimmed))

	case pLoadRules:
		a.loadRulesAt(expandPath(trimmed))

	case pGotoLine:
		n, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			a.errorf("not a line number: %s", trimmed)
			return false
		}
		a.gotoSourceLine(n)

	case pCommand:
		a.runCommandLine(trimmed)
	}
	return true
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
