package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// fileInfoText is the one-line answer to "where am I": the whole path, the
// cursor's place in the view and in the file, the file's size, the syntax
// profile of the region under the cursor, and the modes in force. A message
// has one row, so it is cut to the screen width, dropping the least useful
// parts first.
func (a *App) fileInfoText() string {
	v := a.activeView()
	path := a.src.Name()
	pos := "empty"
	profile := ""
	if v.Len() > 0 {
		ln, err := v.At(a.cursorLine(), nil)
		if err == nil {
			pos = a.positionText(ln.Num, ln.Off)
			if a.eng.Profiles != nil {
				profile = a.eng.Profiles.Describe(ln.Off)
			}
		}
	}
	size := humanBytes(a.idx.Size())
	flags := ""
	if f := a.modeFlags(); len(f) > 0 {
		flags = "[" + strings.Join(f, " ") + "]"
	}

	width, _ := a.scr.Size()
	join := func() string {
		var parts []string
		for _, p := range []string{path, pos, size, profile, flags} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, "  ")
	}
	for _, drop := range []*string{&flags, &size} {
		if utf8.RuneCountInString(join()) <= width {
			break
		}
		*drop = ""
	}
	if over := utf8.RuneCountInString(join()) - width; over > 0 {
		path = shortenPathLeft(path, utf8.RuneCountInString(path)-over)
	}
	return join()
}

// positionText places the cursor line, numbered num in the source and
// starting at byte off, within the active view.
func (a *App) positionText(num, off int64) string {
	v := a.activeView()
	total := commas(v.Len())
	if !v.Done() {
		total = "≥" + total
	}

	var details []string
	head := fmt.Sprintf("line %s of %s", commas(a.cursorLine()+1), total)
	if a.filtered() {
		head += " shown"
		details = append(details, "source line "+commas(num))
	}
	details = append(details, fmt.Sprintf("%d%%", a.bytePercent(off)))
	if p := a.progressText(v); p != "" {
		details = append(details, p)
	}
	return fmt.Sprintf("%s (%s)", head, strings.Join(details, ", "))
}

// shortenPathLeft cuts a path to limit runes from the left, where the
// directories are least telling, but never past its file name.
func shortenPathLeft(path string, limit int) string {
	n := utf8.RuneCountInString(path)
	limit = max(limit, 1+utf8.RuneCountInString(filepath.Base(path)))
	if n <= limit {
		return path
	}
	return "…" + string([]rune(path)[n-limit+1:])
}
