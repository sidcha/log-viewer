package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/view"
)

// statusCursor is what the status fields know about the cursor line, read
// once per frame however many fields ask.
type statusCursor struct {
	v  view.View
	ln view.Line
	ok bool
}

func (a *App) readStatusCursor() *statusCursor {
	c := &statusCursor{v: a.activeView()}
	if c.v.Len() > 0 {
		ln, err := c.v.At(a.cursorLine(), nil)
		c.ln, c.ok = ln, err == nil
	}
	return c
}

// statusFields draws each field config.StatusFields names. A field with
// nothing to report returns "".
var statusFields = map[string]func(a *App, c *statusCursor) string{
	"buffer": func(a *App, c *statusCursor) string { return a.bufferTag() },
	"file":   func(a *App, c *statusCursor) string { return filepath.Base(a.src.Name()) },
	"path":   func(a *App, c *statusCursor) string { return a.src.Name() },
	"line": func(a *App, c *statusCursor) string {
		if c.v.Len() == 0 {
			return "0"
		}
		return commas(a.cursorLine() + 1)
	},
	"lines": func(a *App, c *statusCursor) string {
		if !c.v.Done() {
			return "≥" + commas(c.v.Len())
		}
		return commas(c.v.Len())
	},
	"percent": func(a *App, c *statusCursor) string {
		if !c.ok {
			return ""
		}
		return fmt.Sprintf("%d%%", a.bytePercent(c.ln.Off))
	},
	"size": func(a *App, c *statusCursor) string { return humanBytes(a.idx.Size()) },
	"flags": func(a *App, c *statusCursor) string {
		if f := a.modeFlags(); len(f) > 0 {
			return "[" + strings.Join(f, " ") + "]"
		}
		return ""
	},
	"progress": func(a *App, c *statusCursor) string { return a.progressText(c.v) },
	"error": func(a *App, c *statusCursor) string {
		if err := a.idx.Err(); err != nil {
			return "read error: " + err.Error()
		}
		return ""
	},
	"keys": func(a *App, c *statusCursor) string { return a.pendingKeys() },
	"profile": func(a *App, c *statusCursor) string {
		if !c.ok || a.eng.Profiles == nil {
			return ""
		}
		return a.eng.Profiles.Describe(c.ln.Off)
	},
	"encoding": func(a *App, c *statusCursor) string { return a.screenBytes.encoding() },
}

// bytePercent is how far into the file off lies. It is exact even while the
// line count is still being gathered, which a line-based figure is not.
func (a *App) bytePercent(off int64) int64 {
	size := a.idx.Size()
	if size <= 0 {
		return 0
	}
	return clampPct(off * 100 / size)
}

// progressText reports a scan that has not caught up, or "".
func (a *App) progressText(v view.View) string {
	scanned, total := v.Progress()
	if v.Done() || total <= 0 {
		return ""
	}
	what := "indexing"
	if a.filtered() {
		what = "filtering"
	}
	return fmt.Sprintf("%s %d%%", what, clampPct(scanned*100/total))
}

// expandStatus fills a status template in. A field with nothing to report
// takes the spaces before it along, or the ones after it when only spaces
// precede it, so an idle field leaves no gap.
func (a *App) expandStatus(tmpl string, c *statusCursor) string {
	parts, err := config.ParseStatus(tmpl)
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	eatSpaces := false
	for _, p := range parts {
		if p.Field == "" {
			text := p.Text
			if eatSpaces {
				text = strings.TrimLeft(text, " ")
				eatSpaces = text == ""
			}
			b.WriteString(text)
			continue
		}
		if val := statusFields[p.Field](a, c); val != "" {
			eatSpaces = false
			b.WriteString(val)
			continue
		}
		kept := strings.TrimRight(b.String(), " ")
		if strings.TrimSpace(kept) == "" {
			eatSpaces = true
			continue
		}
		b.Reset()
		b.WriteString(kept)
	}
	return b.String()
}

// drawStatusSides draws left from the left edge and right against the right
// edge. When they collide the right keeps at most half the row, cut from its
// left so its edge survives, and the left is cut short of it.
func (a *App) drawStatusSides(row int, left, right string, st tcell.Style) {
	w := a.cols
	l, r := []rune(left), []rune(right)
	if len(l)+len(r) > w && len(r) > w/2 {
		r = r[len(r)-w/2:]
	}
	if room := max(w-len(r)-1, 0); len(l) > room {
		l = l[:room]
	}
	a.fillRow(row, string(l), st)
	a.drawText(w-len(r), row, string(r), st)
}
