package ui

import (
	"path/filepath"
	"strconv"

	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
)

// call is one invocation of a command.
type call struct {
	config.Command
	// count is the number typed before the key, zero when there was none.
	// Most commands read it as a repeat; a few, notably goto-end, read it as
	// a line number, the way vim does.
	count int64
	// n is the repeat: a numeric first argument, else the count, else 1.
	n int64
}

// handler carries out one command.
type handler func(a *App, c call)

// handlers implements every command in config.Specs, and nothing else; a
// test holds the two to each other, so a command can be neither declared
// without being implemented nor implemented without being declared.
var handlers map[string]handler

// exec runs one command. Every interactive action arrives here, whether from a
// key binding, a repeat count or the command line.
func (a *App) exec(cmd config.Command, count int64) {
	h, ok := handlers[cmd.Name]
	if !ok {
		a.errorf("unimplemented command %q", cmd.Name)
		return
	}
	n := max(count, 1)
	if len(cmd.Args) > 0 {
		if v, err := strconv.ParseInt(cmd.Args[0], 10, 64); err == nil && v > 0 {
			n = v
		}
	}
	h(a, call{Command: cmd, count: count, n: n})
}

// The table is filled in init rather than in its declaration because the
// handlers call back into exec, and Go does not allow a variable's
// initializer to refer to itself.
func init() {
	handlers = map[string]handler{
		"nop": func(a *App, c call) {},
		"quit": func(a *App, c call) {
			a.requestQuit()
		},
		"quit-force": func(a *App, c call) {
			a.quit = true
		},
		"rules-save": func(a *App, c call) {
			if len(c.Args) == 1 {
				a.saveRules(config.ExpandHome(c.Args[0]))
				return
			}
			if a.rulesPath != "" {
				a.saveRules(a.rulesPath)
				return
			}
			a.promptSaveRules(nil)
		},
		"rules-load": func(a *App, c call) {
			if len(c.Args) == 1 {
				a.loadRulesAt(config.ExpandHome(c.Args[0]))
				return
			}
			a.openPrompt(pLoadRules, "load rules from: ", &a.histPath)
		},
		"cancel": func(a *App, c call) {
			a.cancel()
		},
		"help": func(a *App, c call) {
			a.showHelp()
		},
		"command": func(a *App, c call) {
			a.openPrompt(pCommand, ":", &a.histCmd)
		},
		"redraw": func(a *App, c call) {
			a.scr.Sync()
		},

		// ---------------------------------------------------------------- motion
		"up": func(a *App, c call) {
			a.moveCursor(-c.n)
		},
		"down": func(a *App, c call) {
			a.moveCursor(c.n)
		},
		"left": func(a *App, c call) {
			a.moveColumn(-int(c.n))
		},
		"right": func(a *App, c call) {
			a.moveColumn(int(c.n))
		},
		"word-forward": func(a *App, c call) {
			for i := int64(0); i < c.n; i++ {
				a.wordForward()
			}
		},
		"word-back": func(a *App, c call) {
			for i := int64(0); i < c.n; i++ {
				a.wordBack()
			}
		},
		"word-end": func(a *App, c call) {
			for i := int64(0); i < c.n; i++ {
				a.wordEnd()
			}
		},
		"line-start": func(a *App, c call) {
			a.setColumn(0)
			a.hoff = 0
		},
		"line-first": func(a *App, c call) {
			a.columnFirstNonBlank()
		},
		"line-end": func(a *App, c call) {
			a.columnEnd()
		},
		"column-reset": func(a *App, c call) {
			a.hoff = 0
		},
		"scroll-right": func(a *App, c call) {
			a.scrollColumns(int(c.n))
		},
		"scroll-left": func(a *App, c call) {
			a.scrollColumns(-int(c.n))
		},
		"scroll-down": func(a *App, c call) {
			a.scrollView(c.n * int64(max(a.opts().ScrollStep, 1)))
		},
		"scroll-up": func(a *App, c call) {
			a.scrollView(-c.n * int64(max(a.opts().ScrollStep, 1)))
		},
		"page-up": func(a *App, c call) {
			a.pageCursor(-a.pageStep())
		},
		"page-down": func(a *App, c call) {
			a.pageCursor(a.pageStep())
		},
		"half-page-up": func(a *App, c call) {
			a.pageCursor(-int64(a.contentRows() / 2))
		},
		"half-page-down": func(a *App, c call) {
			a.pageCursor(int64(a.contentRows() / 2))
		},
		"goto-start": func(a *App, c call) {
			a.follow = false
			a.setCursor(0)
		},
		"goto-end": func(a *App, c call) {
			// vim: G alone goes to the end, {count}G to that line.
			if c.count > 0 {
				a.gotoSourceLine(c.count)
				return
			}
			a.follow = false
			a.pinToTail()
		},
		"goto-line": func(a *App, c call) {
			if len(c.Args) == 1 {
				v, err := strconv.ParseInt(c.Args[0], 10, 64)
				if err != nil {
					a.errorf("not a line number: %s", c.Args[0])
					return
				}
				a.gotoSourceLine(v)
				return
			}
			if c.count > 0 {
				a.gotoSourceLine(c.count)
				return
			}
			a.openPrompt(pGotoLine, "goto line: ", nil)
		},
		"screen-top": func(a *App, c call) {
			a.cursorToScreen(spotTop, c.n)
		},
		"screen-middle": func(a *App, c call) {
			a.cursorToScreen(spotMiddle, c.n)
		},
		"screen-bottom": func(a *App, c call) {
			a.cursorToScreen(spotBottom, c.n)
		},
		"align-center": func(a *App, c call) {
			a.alignView(spotMiddle)
		},
		"align-top": func(a *App, c call) {
			a.alignView(spotTop)
		},
		"align-bottom": func(a *App, c call) {
			a.alignView(spotBottom)
		},

		// ---------------------------------------------------------------- select
		"visual": func(a *App, c call) {
			a.startVisual(visualChar)
		},
		"visual-line": func(a *App, c call) {
			a.startVisual(visualLine)
		},
		"yank": func(a *App, c call) {
			a.yank(c.count)
		},
		"yank-line": func(a *App, c call) {
			if !a.visualActive() {
				a.sel = selection{mode: visualLine, line: a.cursorLine(), col: 0}
				if c.n > 1 {
					a.setCursor(a.cursorLine() + c.n - 1)
				}
			}
			a.yank(c.count)
		},

		// ----------------------------------------------------------------- mouse
		"mouse-cursor":      func(a *App, c call) { a.mouseCursor() },
		"mouse-select":      func(a *App, c call) { a.mouseSelect() },
		"mouse-select-word": func(a *App, c call) { a.mouseSelectWord() },
		"mouse-select-line": func(a *App, c call) { a.mouseSelectLine() },

		// ------------------------------------------------------------------ view
		"follow-toggle": func(a *App, c call) {
			a.follow = !a.follow
			if a.follow {
				a.everFollowed = true
				a.gotoEnd()
				a.infof("following %s", filepath.Base(a.src.Name()))
			} else {
				a.infof("follow off")
			}
		},
		"wrap-toggle": func(a *App, c call) {
			a.setBool("wrap", !a.opts().Wrap)
		},
		"line-numbers-toggle": func(a *App, c call) {
			a.setBool("line-numbers", !a.opts().LineNumbers)
		},
		"status-toggle": func(a *App, c call) {
			pin := "on"
			if a.opts().StatusLine == "on" {
				pin = "off"
			}
			_ = a.cfg.Set("status-line", pin)
		},
		"file-info": func(a *App, c call) {
			a.infof("%s", a.fileInfoText())
		},
		"set": func(a *App, c call) {
			a.setOption(c.Args)
		},
		"reload": func(a *App, c call) {
			a.reload()
		},
		"buffer-next": func(a *App, c call) { a.stepBuffer(c.n) },
		"buffer-prev": func(a *App, c call) { a.stepBuffer(-c.n) },
		"buffer-go": func(a *App, c call) {
			n, err := strconv.Atoi(c.Args[0])
			if err != nil || n < 1 || n > len(a.bufs) {
				a.errorf("no buffer %s", c.Args[0])
				return
			}
			a.switchBuffer(n - 1)
		},
		"buffer-list": func(a *App, c call) { a.showBufferList() },

		// ---------------------------------------------------------------- search
		"search-forward": func(a *App, c call) {
			if len(c.Args) == 1 {
				pattern, mode := splitModePrefix(c.Args[0], defaultMode(findSearch))
				a.doSearchMode(pattern, mode, false)
				return
			}
			a.openSearchFinder(false)
		},
		"search-backward": func(a *App, c call) {
			if len(c.Args) == 1 {
				pattern, mode := splitModePrefix(c.Args[0], defaultMode(findSearch))
				a.doSearchMode(pattern, mode, true)
				return
			}
			a.openSearchFinder(true)
		},
		"jump": func(a *App, c call) {
			pattern := ""
			if len(c.Args) == 1 {
				pattern = c.Args[0]
			}
			a.openFinder(findJump, pattern)
		},
		"search-next": func(a *App, c call) {
			a.searchStep(false)
		},
		"search-prev": func(a *App, c call) {
			a.searchStep(true)
		},
		"search-word": func(a *App, c call) {
			a.searchWordUnderCursor(false)
		},
		"search-word-back": func(a *App, c call) {
			a.searchWordUnderCursor(true)
		},
		"search-clear": func(a *App, c call) {
			// Also repaint, because C-l is bound here: in vi that key tidies the
			// screen, and clearing the search highlight is the half of that people
			// actually reach for.
			a.scr.Sync()
			if a.search.m == nil {
				return
			}
			a.search = searchState{}
			a.searchCur = -1
			a.infof("search highlight cleared")
		},

		// ----------------------------------------------------------------- rules
		"filter-only": func(a *App, c call) {
			a.ruleCommand(c.Command, findOnly)
		},
		"filter-never": func(a *App, c call) {
			a.ruleCommand(c.Command, findNever)
		},
		"highlight": func(a *App, c call) {
			a.ruleCommand(c.Command, findHighlight)
		},
		"filter-pop": func(a *App, c call) {
			if a.rules.Len() == 0 {
				a.infof("no rules")
				return
			}
			a.applyRules(a.rules.Pop(), true)
			a.infof("%d rule(s) remaining", a.rules.Len())
		},
		"filter-clear": func(a *App, c call) {
			if a.rules.Len() == 0 {
				a.infof("no rules")
				return
			}
			a.applyRules(a.rules.Clear(), true)
			a.infof("rules cleared")
		},
		"highlight-clear": func(a *App, c call) {
			// Only the rules. A search is transient and belongs to n and N, which
			// must keep working after the highlights you built up are removed.
			if a.rules.Count(filter.Highlight) == 0 {
				a.infof("no highlight rules")
				return
			}
			a.applyRules(a.rules.ClearAction(filter.Highlight), true)
			a.infof("highlight rules cleared")
		},
		"filter-bypass": func(a *App, c call) {
			if a.filt == nil {
				a.infof("nothing is being filtered")
				return
			}
			anchor := a.anchorSource()
			a.bypass = !a.bypass
			a.seekSource(anchor)
			a.infof("filters %s", onOff(!a.bypass))
		},
		"filter-list": func(a *App, c call) {
			a.showRules()
		},

		// ------------------------------------------------------------------ file
		"save-view": func(a *App, c call) {
			if len(c.Args) == 1 {
				a.startSave(c.Args[0])
				return
			}
			a.openPrompt(pSave, "save to: ", &a.histPath)
		},
	}
	for name, h := range modeHandlers() {
		handlers[name] = h
	}
}
