package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/filter"
)

// keys sends a literal string of key presses through the normal dispatch path,
// so counts and sequences are exercised exactly as a typist would produce them.
func keys(a *App, s string) {
	for _, r := range s {
		a.key(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

func ctrl(a *App, k tcell.Key) {
	a.key(tcell.NewEventKey(k, 0, tcell.ModCtrl))
}

// ---------------------------------------------------------------- counts

func TestCountRepeatsMotion(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	keys(a, "5j")
	if a.cursorLine() != 5 {
		t.Fatalf("5j moved to %d, want 5", a.cursorLine())
	}
	keys(a, "2k")
	if a.cursorLine() != 3 {
		t.Fatalf("2k moved to %d, want 3", a.cursorLine())
	}
	keys(a, "12j")
	if a.cursorLine() != 15 {
		t.Fatalf("12j moved to %d, want 15", a.cursorLine())
	}
}

func TestCountWithG(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	keys(a, "42G")
	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ln.Num != 42 {
		t.Fatalf("42G landed on line %d, want 42", ln.Num)
	}

	// G with no count goes to the end, as in vim.
	keys(a, "G")
	if a.cursorLine() != a.viewLen()-1 {
		t.Fatalf("G landed on %d, want the last line %d", a.cursorLine(), a.viewLen()-1)
	}
}

// TestZeroIsAMotionNotACount is the awkward case: 0 starts the line-start
// motion, but extends a count that is already being typed.
func TestZeroIsAMotionNotACount(t *testing.T) {
	a, _ := newTestApp(t, "    indented line here\n")

	keys(a, "$")
	if a.curCol == 0 {
		t.Fatal("$ did not move off column 0")
	}
	keys(a, "0")
	if a.curCol != 0 {
		t.Fatalf("0 left the column at %d", a.curCol)
	}

	b, _ := newTestApp(t, numbered(200))
	keys(b, "10j")
	if b.cursorLine() != 10 {
		t.Fatalf("10j moved to %d, want 10 (the 0 must extend the count)", b.cursorLine())
	}
}

func TestCountIsClearedAfterUse(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "5j")
	keys(a, "j")
	if a.cursorLine() != 6 {
		t.Fatalf("cursor = %d, want 6; the count leaked into the next motion", a.cursorLine())
	}
	if a.count != 0 {
		t.Errorf("count = %d after use", a.count)
	}
}

func TestEscapeClearsPendingCount(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "12")
	if a.count != 12 {
		t.Fatalf("count = %d while typing", a.count)
	}
	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.count != 0 {
		t.Errorf("Escape left count = %d", a.count)
	}
	keys(a, "j")
	if a.cursorLine() != 1 {
		t.Fatalf("cursor = %d, want 1", a.cursorLine())
	}
}

// ------------------------------------------------------------- sequences

func TestGgSequence(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "G")
	if a.cursorLine() == 0 {
		t.Fatal("G did not move")
	}

	// A lone g must do nothing and wait for the second.
	keys(a, "g")
	if len(a.pending) != 1 {
		t.Fatalf("a single g left %d pending keys, want 1", len(a.pending))
	}
	keys(a, "g")
	if a.cursorLine() != 0 {
		t.Fatalf("gg landed on %d, want 0", a.cursorLine())
	}
	if len(a.pending) != 0 {
		t.Error("pending keys were not cleared after the sequence completed")
	}
}

func TestUnknownSequenceIsDiscarded(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	keys(a, "gx")
	if len(a.pending) != 0 {
		t.Errorf("an unmatched sequence left %d pending keys", len(a.pending))
	}
	keys(a, "j")
	if a.cursorLine() != 1 {
		t.Fatalf("cursor = %d; the discarded sequence broke the next key", a.cursorLine())
	}
}

func TestAlignSequences(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "50G")
	cur := a.cursorLine()

	keys(a, "zt")
	if a.top != cur {
		t.Errorf("zt put top at %d, want %d", a.top, cur)
	}
	keys(a, "zz")
	if want := cur - int64(testH)/2; a.top != want {
		t.Errorf("zz put top at %d, want %d", a.top, want)
	}
	keys(a, "zb")
	if want := cur - int64(testH) + 1; a.top != want {
		t.Errorf("zb put top at %d, want %d", a.top, want)
	}
	// The cursor must not move while the window is repositioned around it.
	if a.cursorLine() != cur {
		t.Errorf("an align sequence moved the cursor to %d", a.cursorLine())
	}
}

func TestScreenPositionMotions(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "100G")
	keys(a, "zz")
	top := a.top
	off := int64(a.scrolloff())

	keys(a, "H")
	if a.cursorLine() != top+off {
		t.Errorf("H landed on %d, want %d", a.cursorLine(), top+off)
	}
	keys(a, "L")
	if want := top + int64(testH) - 1 - off; a.cursorLine() != want {
		t.Errorf("L landed on %d, want %d", a.cursorLine(), want)
	}
	keys(a, "M")
	if mid := top + int64(testH)/2; absDiff(a.cursorLine(), mid) > 1 {
		t.Errorf("M landed on %d, want about %d", a.cursorLine(), mid)
	}
}

func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

// ----------------------------------------------------------- word motions

func TestWordMotions(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta_two  gamma.delta\n")

	keys(a, "w")
	if a.curCol != 6 {
		t.Errorf("w landed on %d, want 6 (beta_two)", a.curCol)
	}
	keys(a, "w")
	if a.curCol != 16 {
		t.Errorf("second w landed on %d, want 16 (gamma)", a.curCol)
	}
	keys(a, "w")
	if a.curCol != 21 {
		t.Errorf("third w landed on %d, want 21 (the dot)", a.curCol)
	}

	keys(a, "b")
	if a.curCol != 16 {
		t.Errorf("b landed on %d, want 16", a.curCol)
	}
	keys(a, "e")
	if a.curCol != 20 {
		t.Errorf("e landed on %d, want 20 (end of gamma)", a.curCol)
	}
	keys(a, "0")
	if a.curCol != 0 {
		t.Errorf("0 landed on %d", a.curCol)
	}
	keys(a, "$")
	if a.curCol != 26 {
		t.Errorf("$ landed on %d, want 26", a.curCol)
	}
}

func TestFirstNonBlank(t *testing.T) {
	a, _ := newTestApp(t, "\t   hello world\n")
	keys(a, "$")
	keys(a, "^")
	if a.curCol != 4 {
		t.Errorf("^ landed on %d, want 4", a.curCol)
	}
}

// TestRememberedColumn is vim's behaviour of restoring the column after
// passing through shorter lines.
func TestRememberedColumn(t *testing.T) {
	a, _ := newTestApp(t, "a long first line here\nshort\nanother long line here\n")

	keys(a, "$")
	want := a.curCol
	keys(a, "j")
	if a.curCol >= want {
		t.Fatalf("the short line did not clamp the column: %d", a.curCol)
	}
	keys(a, "j")
	if a.curCol != want {
		t.Errorf("column came back as %d, want the remembered %d", a.curCol, want)
	}
}

// --------------------------------------------------------- visual and yank

func TestVisualLineSelection(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	keys(a, "V")
	if a.sel.mode != visualLine {
		t.Fatal("V did not start a line-wise selection")
	}
	keys(a, "2j")

	sl, _, el, _ := a.span()
	if sl != 0 || el != 2 {
		t.Fatalf("selection spans %d..%d, want 0..2", sl, el)
	}
	// Every column of a selected row is marked.
	if _, _, ok := a.selectedRange(1, 10); !ok {
		t.Error("a line inside the selection is not marked")
	}
	if _, _, ok := a.selectedRange(3, 10); ok {
		t.Error("a line outside the selection is marked")
	}

	keys(a, "V")
	if a.visualActive() {
		t.Error("pressing V again did not leave visual mode")
	}
}

func TestVisualCharSelection(t *testing.T) {
	a, _ := newTestApp(t, "abcdefghij\nklmnopqrst\n")

	keys(a, "lll") // column 3
	keys(a, "v")
	keys(a, "ll") // through column 5

	start, end, ok := a.selectedRange(0, 10)
	if !ok {
		t.Fatal("no selection on the cursor line")
	}
	if start != 3 || end != 6 {
		t.Errorf("selection is %d..%d, want 3..6", start, end)
	}
}

func TestEscapeLeavesVisual(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	keys(a, "V")
	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.visualActive() {
		t.Error("Escape did not leave visual mode")
	}
}

func TestYankLinesToClipboardCommand(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))

	out := clipboardSink(t, a)
	keys(a, "Vjj")
	keys(a, "y")

	got := readSink(t, out)
	want := numberedLine(0) + "\n" + numberedLine(1) + "\n" + numberedLine(2) + "\n"
	if got != want {
		t.Errorf("yanked %q, want %q", got, want)
	}
	if a.visualActive() {
		t.Error("yank did not leave visual mode")
	}
}

func TestYankCurrentLineWithoutSelecting(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	out := clipboardSink(t, a)

	keys(a, "3j")
	keys(a, "yy")

	if got, want := readSink(t, out), numberedLine(3)+"\n"; got != want {
		t.Errorf("yy copied %q, want %q", got, want)
	}
}

func TestYankCountedLines(t *testing.T) {
	a, _ := newTestApp(t, numbered(40))
	out := clipboardSink(t, a)

	keys(a, "3yy")
	want := numberedLine(0) + "\n" + numberedLine(1) + "\n" + numberedLine(2) + "\n"
	if got := readSink(t, out); got != want {
		t.Errorf("3yy copied %q, want %q", got, want)
	}
}

func TestYankCharSelection(t *testing.T) {
	a, _ := newTestApp(t, "abcdefghij\n")
	out := clipboardSink(t, a)

	keys(a, "v")
	keys(a, "llll")
	keys(a, "y")

	if got := readSink(t, out); got != "abcde" {
		t.Errorf("yanked %q, want %q", got, "abcde")
	}
}

// TestYankFromFilteredViewCopiesWhatIsShown checks that a yank follows the
// view rather than the file underneath it.
func TestYankFromFilteredViewCopiesWhatIsShown(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	out := clipboardSink(t, a)

	a.addRule(filter.Only, "ERROR", color.RGB{})
	waitFiltered(t, a)

	keys(a, "Vj")
	keys(a, "y")

	got := readSink(t, out)
	if strings.Contains(got, "INFO") {
		t.Errorf("yank copied hidden lines: %q", got)
	}
	want := numberedLine(0) + "\n" + numberedLine(3) + "\n"
	if got != want {
		t.Errorf("yanked %q, want %q", got, want)
	}
}

// ------------------------------------------------------------ command line

func TestCommandLineVimAliases(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))

	a.runCommandLine(":42")
	ln, _ := a.activeView().At(a.cursorLine(), nil)
	if ln.Num != 42 {
		t.Errorf(":42 landed on %d", ln.Num)
	}

	a.runCommandLine("$")
	if a.cursorLine() != a.viewLen()-1 {
		t.Errorf(":$ landed on %d", a.cursorLine())
	}

	a.runCommandLine("only ERROR")
	waitFiltered(t, a)
	if a.rules.Len() != 1 || a.rules.All()[0].Action != filter.Only {
		t.Errorf(":only did not add an only rule: %d rules", a.rules.Len())
	}

	a.runCommandLine("never INFO")
	if a.rules.Len() != 2 || a.rules.All()[1].Action != filter.Never {
		t.Errorf(":never did not add a never rule")
	}

	a.runCommandLine("hl bad")
	if a.rules.All()[2].Action != filter.Highlight {
		t.Errorf(":hl did not add a highlight")
	}

	// With rules in play :q asks first; :q! does not.
	a.runCommandLine("q")
	if a.quit {
		t.Error(":q quit without asking about the rules")
	}
	if a.confirm == nil {
		t.Fatal(":q did not ask about the rules")
	}
	keys(a, "n") // discard
	if !a.quit {
		t.Error("answering discard did not quit")
	}
}

func TestCommandLineSet(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	if a.opts().Wrap {
		t.Fatal("wrap starts on")
	}
	a.runCommandLine("set wrap")
	if !a.opts().Wrap {
		t.Error(":set wrap did not turn it on")
	}
	a.runCommandLine("set wrap")
	if a.opts().Wrap {
		t.Error(":set wrap did not toggle it back off")
	}

	a.runCommandLine("set tab-width 4")
	if a.cfg.Int("tab-width") != 4 {
		t.Errorf("tab-width = %d, want 4", a.cfg.Int("tab-width"))
	}

	a.runCommandLine("set nosuchthing on")
	if !strings.Contains(a.msg, "unknown setting") {
		t.Errorf("message = %q, want a complaint about the setting", a.msg)
	}
}

func TestCommandLineRejectsNonsense(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	a.runCommandLine("frobnicate")
	if !strings.Contains(a.msg, "unknown command") {
		t.Errorf("message = %q", a.msg)
	}
}

func TestCommandCompletion(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))

	a.key(tcell.NewEventKey(tcell.KeyRune, ':', tcell.ModNone))
	typeInto(a, "filt")
	press(a, tcell.KeyTab)

	got := string(a.prompt.buf)
	if !strings.HasPrefix(got, "filt") || got == "filt" {
		t.Fatalf("Tab completed to %q", got)
	}
	first := got

	// Repeated Tab cycles the candidates.
	press(a, tcell.KeyTab)
	if string(a.prompt.buf) == first {
		t.Error("a second Tab did not move to the next candidate")
	}
}

func TestSearchWordUnderCursor(t *testing.T) {
	a, _ := newTestApp(t, "alpha beta\ngamma delta\nbeta again\n")

	keys(a, "w") // onto "beta"
	if got := a.wordUnderCursor(); got != "beta" {
		t.Fatalf("word under cursor = %q", got)
	}

	keys(a, "*")
	if a.search.m == nil {
		t.Fatal("* did not set a search")
	}
	if !a.search.m.Match([]byte("beta again")) {
		t.Error("the search does not match the word it was made from")
	}
}

// ------------------------------------------------------------------ helpers

// clipboardSink points clipboard-command at a script that records what it is
// given, so yanking can be checked without a terminal.
func clipboardSink(t *testing.T, a *App) string {
	t.Helper()
	dir := t.TempDir()
	out := dir + "/clip.txt"
	script := dir + "/sink.sh"

	body := "#!/bin/sh\ncat > " + out + "\n"
	if err := writeExec(script, body); err != nil {
		t.Fatal(err)
	}
	if err := a.cfg.Set("clipboard-command", script); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeExec(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o755)
}

func readSink(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("clipboard sink was never written: %v", err)
	}
	return string(b)
}
