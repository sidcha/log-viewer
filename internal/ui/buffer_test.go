package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sidcha/log-viewer/internal/color"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/ruleset"
	"github.com/sidcha/log-viewer/internal/source"
)

// addTestBuffer opens content as another file in a and indexes it fully.
// It returns the buffer's wake function.
func addTestBuffer(t *testing.T, a *App, name, content string) func() {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	idx := index.New(8, 0)
	ixr := index.NewIndexer(src, idx, index.Options{Block: 256})
	ixr.Sync(t.Context())
	return a.AddBuffer(src, idx, highlight.Fixed{})
}

func drainWake(a *App) bool {
	select {
	case <-a.wake:
		return true
	default:
		return false
	}
}

func TestEachBufferKeepsItsOwnPositionAndRules(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	keys(a, "40j")
	a.addRule(filter.Only, "ERROR", color.RGB{})

	addTestBuffer(t, a, "second.log", numbered(100))
	if a.bufferIndex() != 1 {
		t.Fatalf("AddBuffer left buffer %d on screen, want 1", a.bufferIndex())
	}
	if a.cursorLine() != 0 || a.rules.Len() != 0 {
		t.Fatalf("new buffer inherited state: cursor %d, %d rule(s)", a.cursorLine(), a.rules.Len())
	}
	keys(a, "7j")

	a.showBuffer(0)
	if a.rules.Len() != 1 || !a.filtered() {
		t.Errorf("first buffer lost its rule")
	}
	first := a.cursorLine()

	a.showBuffer(1)
	if a.cursorLine() != 7 {
		t.Errorf("second buffer's cursor is %d, want 7", a.cursorLine())
	}
	a.showBuffer(0)
	if a.cursorLine() != first {
		t.Errorf("first buffer's cursor moved from %d to %d", first, a.cursorLine())
	}
}

// TestHiddenBufferDoesNotAskForFrames: ten files indexing in the background
// must not repaint a screen that shows only one of them.
func TestHiddenBufferDoesNotAskForFrames(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	wakeSecond := addTestBuffer(t, a, "second.log", numbered(50))
	a.showBuffer(0)
	drainWake(a)

	wakeSecond()
	if drainWake(a) {
		t.Error("a hidden buffer's wake asked for a frame")
	}

	a.showBuffer(1)
	wakeSecond()
	if !drainWake(a) {
		t.Error("the shown buffer's wake did not ask for a frame")
	}
}

// TestSearchResultDoesNotMoveAnotherBuffer: a search finishing after a
// switch is a result for a buffer no longer on screen, so it is dropped
// rather than moving the cursor of whatever buffer replaced it.
func TestSearchResultDoesNotMoveAnotherBuffer(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	addTestBuffer(t, a, "second.log", numbered(200))
	keys(a, "2j") // buffer 1 on a non-match line
	a.showBuffer(0)

	// showBuffer is the silent switch, so it can happen mid-search here;
	// the key-driven switch refuses while busy.
	a.doSearch("ERROR", false)
	a.showBuffer(1)
	waitSearch(t, a)

	if a.cursorLine() != 2 {
		t.Errorf("a result for a buffer no longer on screen moved the cursor to %d, want 2", a.cursorLine())
	}
}

func TestBracketsMoveBetweenBuffersAndWrapAtTheEnds(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	addTestBuffer(t, a, "b.log", numbered(50))
	addTestBuffer(t, a, "c.log", numbered(50))
	a.showBuffer(0)

	keys(a, "]")
	if a.bufferIndex() != 1 {
		t.Fatalf("] went to %d, want 1", a.bufferIndex())
	}
	if !a.liveMsg() || !strings.Contains(a.msg, "[2/3]") || !strings.Contains(a.msg, "b.log") {
		t.Errorf("switching showed %q, want the tag and the file", a.msg)
	}
	keys(a, "]")
	if a.bufferIndex() != 2 {
		t.Errorf("] went to %d, want 2", a.bufferIndex())
	}
	keys(a, "]")
	if a.bufferIndex() != 0 || !strings.Contains(a.msg, "[1/3]") {
		t.Errorf("] past the end: buffer %d, message %q, want the first", a.bufferIndex(), a.msg)
	}
	keys(a, "[")
	if a.bufferIndex() != 2 || !strings.Contains(a.msg, "[3/3]") {
		t.Errorf("[ past the start: buffer %d, message %q, want the last", a.bufferIndex(), a.msg)
	}
	keys(a, "4]")
	if a.bufferIndex() != 0 {
		t.Errorf("4] from the last of three went to %d, want 0", a.bufferIndex())
	}
	keys(a, "5[")
	if a.bufferIndex() != 1 {
		t.Errorf("5[ from the first of three went to %d, want 1", a.bufferIndex())
	}
	a.exec(config.Command{Name: "buffer-go", Args: []string{"3"}}, 0)
	if a.bufferIndex() != 2 {
		t.Errorf(":b 3 went to %d", a.bufferIndex())
	}
}

// TestBufferNumberNeedsNoSpace: vim takes :b2 as well as :b 2.
func TestBufferNumberNeedsNoSpace(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	addTestBuffer(t, a, "b.log", numbered(50))
	addTestBuffer(t, a, "c.log", numbered(50))
	a.showBuffer(0)

	for _, c := range []struct {
		line string
		want int
	}{
		{"b2", 1},
		{"b 3", 2},
		{"buffer1", 0},
		{"b2 ", 1},
	} {
		a.runCommandLine(c.line)
		if a.bufferIndex() != c.want {
			t.Errorf(":%s went to %d, want %d (message %q)", c.line, a.bufferIndex(), c.want, a.msg)
		}
	}

	a.runCommandLine("b9")
	if !strings.Contains(a.msg, "no buffer 9") {
		t.Errorf(":b9 said %q, want no buffer 9", a.msg)
	}
	// Other commands that start with b stay what they were.
	a.runCommandLine("bn")
	if a.bufferIndex() != 2 {
		t.Errorf(":bn went to %d, want 2", a.bufferIndex())
	}
}

func TestOneBufferSaysSoAndHasNoTag(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	keys(a, "]")
	if a.bufferIndex() != 0 || !strings.Contains(a.msg, "only one buffer") {
		t.Errorf("] with one buffer: %d, %q", a.bufferIndex(), a.msg)
	}
	keys(a, "[")
	if !strings.Contains(a.msg, "only one buffer") {
		t.Errorf("[ with one buffer said %q", a.msg)
	}
	if a.bufferTag() != "" {
		t.Errorf("one buffer is tagged %q", a.bufferTag())
	}
}

func TestSwitchIsRefusedWhileBusy(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	addTestBuffer(t, a, "b.log", numbered(50))
	a.showBuffer(0)
	a.busy = &busyOp{what: "saving to x", cancel: func() {}}

	keys(a, "]")
	if a.bufferIndex() != 0 {
		t.Error("switched buffers with an operation still running")
	}
	a.busy = nil
}

// TestSwitchIsRefusedWhileFinderOpen: the finder's background scan re-derives
// state from whichever buffer is on screen, so a switch mid-scan would hand
// it a different file's lines. There is no key path to reach this while the
// window is open (its table has no binding for the command), so the guard is
// exercised directly.
func TestSwitchIsRefusedWhileFinderOpen(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	addTestBuffer(t, a, "b.log", numbered(50))
	a.showBuffer(0)
	a.openFinder(findJump, "")

	a.exec(config.Command{Name: "buffer-next"}, 0)
	if a.bufferIndex() != 0 {
		t.Error("switched buffers while the match window was open")
	}
	a.closeFinder()
}

func TestSwitchEndsTheSelection(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	addTestBuffer(t, a, "b.log", numbered(50))
	a.showBuffer(0)
	keys(a, "vj")
	a.exec(config.Command{Name: "buffer-next"}, 0)
	a.showBuffer(0)
	if a.visualActive() {
		t.Error("the selection survived a switch")
	}
}

// TestQuitAsksAboutEachBufferInTurn: with work in two files, both are worth
// a question, and each is asked with its file on screen.
func TestQuitAsksAboutEachBufferInTurn(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	a.addRule(filter.Only, "ERROR", color.RGB{})
	addTestBuffer(t, a, "b.log", numbered(50))
	a.addRule(filter.Never, "INFO", color.RGB{})
	addTestBuffer(t, a, "c.log", numbered(50)) // clean

	run(a, "quit")
	if a.confirm == nil || a.bufferIndex() != 0 {
		t.Fatalf("first question: confirm %v, on buffer %d", a.confirm != nil, a.bufferIndex())
	}
	if !strings.Contains(a.confirm.text, "test.log") {
		t.Errorf("first question does not name the file: %q", a.confirm.text)
	}
	run(a, "confirm-no")
	if a.confirm == nil || a.bufferIndex() != 1 {
		t.Fatalf("second question: confirm %v, on buffer %d", a.confirm != nil, a.bufferIndex())
	}
	run(a, "confirm-cancel")
	if a.quit || a.bufferIndex() != 1 {
		t.Errorf("cancel: quit %v, left on buffer %d, want to stay on 1", a.quit, a.bufferIndex())
	}

	run(a, "quit", "confirm-no", "confirm-no")
	if !a.quit {
		t.Error("answering both questions did not quit")
	}
}

// TestOneBufferQuitQuestionIsUnchanged: the single-file session reads as
// it always has.
func TestOneBufferQuitQuestionIsUnchanged(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	a.addRule(filter.Only, "ERROR", color.RGB{})
	run(a, "quit")
	if a.confirm == nil || a.confirm.text != "Save rules before quitting?" {
		t.Errorf("single-buffer question changed: %+v", a.confirm)
	}
}

func writeRulesWithCase(t *testing.T, name, cs string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f := &ruleset.File{Case: cs, Rules: []ruleset.Rule{{Action: "only", Pattern: "ERROR"}}}
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func savedCase(t *testing.T, a *App) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.rules")
	if !a.saveRules(path) {
		t.Fatalf("save failed: %s", a.msg)
	}
	f, err := ruleset.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return f.Case
}

// TestEachBufferKeepsTheCaseItsRulesCameWith: loading a rules file sets the
// case for the session, but that must not make another file's rules look
// changed, or have them written back under a case they were not saved with.
func TestEachBufferKeepsTheCaseItsRulesCameWith(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	if err := a.LoadRules(writeRulesWithCase(t, "a.rules", "sensitive")); err != nil {
		t.Fatal(err)
	}
	addTestBuffer(t, a, "b.log", numbered(50))
	if err := a.LoadRules(writeRulesWithCase(t, "b.rules", "insensitive")); err != nil {
		t.Fatal(err)
	}
	a.showBuffer(0)

	if a.rulesDirty() {
		t.Error("the second file's rules left the first file's looking changed")
	}
	if got := savedCase(t, a); got != "sensitive" {
		t.Errorf("the first file's rules were saved as %q, want sensitive", got)
	}
}

// TestSettingCaseChangesTheRulesToSave: with one file, :set case is a
// change to the rules worth saving, as it always was.
func TestSettingCaseChangesTheRulesToSave(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	if err := a.LoadRules(writeRulesWithCase(t, "a.rules", "sensitive")); err != nil {
		t.Fatal(err)
	}
	a.setOption([]string{"case", "insensitive"})

	if !a.rulesDirty() {
		t.Error(":set case did not count as a change")
	}
	if got := savedCase(t, a); got != "insensitive" {
		t.Errorf("saved as %q, want insensitive", got)
	}
}

// TestSwitchToAFollowingBufferReportsItsEnd: a following buffer that grew
// while hidden is shown at its end, so that is the position to report.
func TestSwitchToAFollowingBufferReportsItsEnd(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	addTestBuffer(t, a, "b.log", numbered(50))
	run(a, "follow-toggle")
	a.draw()
	appendLines(t, a, 50, 10)
	a.showBuffer(0)

	keys(a, "]")
	if !strings.Contains(a.msg, "line 60 of") {
		t.Errorf("switching to a following buffer said %q, want line 60", a.msg)
	}
}
