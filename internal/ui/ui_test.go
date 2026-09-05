package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/sidcha/log-viewer/internal/config"
	"github.com/sidcha/log-viewer/internal/filter"
	"github.com/sidcha/log-viewer/internal/highlight"
	"github.com/sidcha/log-viewer/internal/index"
	"github.com/sidcha/log-viewer/internal/source"
)

const (
	testW = 80
	testH = 10
)

func newTestApp(t *testing.T, content string) (*App, tcell.SimulationScreen) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })

	cfg, err := config.Load(config.LoadOptions{NoUser: true})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the status line out of the way unless a test asks for it.
	if err := cfg.Set("status-line", "off"); err != nil {
		t.Fatal(err)
	}

	idx := index.New(8, 0)
	ixr := index.NewIndexer(src, idx, index.Options{Block: 256})
	ixr.Sync(t.Context())

	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatal(err)
	}
	scr.SetSize(testW, testH)
	t.Cleanup(scr.Fini)

	prof := highlight.Select(cfg.Profiles(), "", path, []byte(content))
	app := New(scr, cfg, src, idx, prof)
	t.Cleanup(app.Close)
	return app, scr
}

// rowText reads one rendered row back as a string.
func rowText(scr tcell.SimulationScreen, row int) string {
	var b strings.Builder
	for x := 0; x < testW; x++ {
		r, _, _, _ := scr.GetContent(x, row)
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " ")
}

// numbered builds a corpus where every third line is an ERROR, so filters have
// something to bite on.
func numbered(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(numberedLine(i))
		b.WriteByte('\n')
	}
	return b.String()
}

func numberedLine(i int) string {
	kind := "INFO fine"
	if i%3 == 0 {
		kind = "ERROR bad"
	}
	return fmt.Sprintf("line %03d %s", i, kind)
}

func run(a *App, names ...string) {
	for _, n := range names {
		a.exec(config.Command{Name: n}, 0)
	}
	a.draw()
}

func TestRenderShowsFileFromTheTop(t *testing.T) {
	a, scr := newTestApp(t, numbered(100))
	a.draw()

	if got := rowText(scr, 0); got != numberedLine(0) {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowText(scr, 1); got != numberedLine(1) {
		t.Fatalf("row 1 = %q", got)
	}
	// Every row is log content: nothing is spent on chrome by default.
	if got := rowText(scr, testH-1); got != numberedLine(testH-1) {
		t.Fatalf("last row = %q, want log content", got)
	}
}

// TestCursorMotion checks vi's model: j and k move a cursor and the window
// only follows once the cursor would leave it.
func TestCursorMotion(t *testing.T) {
	a, scr := newTestApp(t, numbered(100))

	run(a, "down", "down")
	if a.cursorLine() != 2 {
		t.Fatalf("cursor = %d, want 2", a.cursorLine())
	}
	if got := rowText(scr, 0); got != numberedLine(0) {
		t.Fatalf("the window scrolled on j: row 0 = %q", got)
	}

	run(a, "up")
	if a.cursorLine() != 1 {
		t.Fatalf("after up cursor = %d, want 1", a.cursorLine())
	}

	// Moving past the bottom, less the scroll-off margin, does scroll.
	off := int64(a.scrolloff())
	for i := int64(0); i < int64(testH); i++ {
		run(a, "down")
	}
	if a.top == 0 {
		t.Fatal("the window never scrolled as the cursor went past the bottom")
	}
	if a.cursorLine() > a.top+int64(testH)-1-off {
		t.Fatalf("cursor %d is below the scroll-off margin (top %d)", a.cursorLine(), a.top)
	}

	run(a, "goto-end")
	if a.cursorLine() != 99 {
		t.Fatalf("G put the cursor at %d, want 99", a.cursorLine())
	}
	if got := rowText(scr, testH-1); got != numberedLine(99) {
		t.Fatalf("goto-end last row = %q", got)
	}

	run(a, "goto-start")
	if a.cursorLine() != 0 || a.top != 0 {
		t.Fatalf("gg left cursor %d top %d", a.cursorLine(), a.top)
	}
	if got := rowText(scr, 0); got != numberedLine(0) {
		t.Fatalf("goto-start row 0 = %q", got)
	}
}

func TestScrollViewKeepsCursorOnScreen(t *testing.T) {
	a, _ := newTestApp(t, numbered(100))

	// C-e scrolls the window; the cursor is dragged only when it must be.
	run(a, "scroll-down", "scroll-down", "scroll-down", "scroll-down", "scroll-down")
	if a.top != 5 {
		t.Fatalf("top = %d, want 5", a.top)
	}
	if a.cursorLine() < a.top || a.cursorLine() > a.top+int64(testH)-1 {
		t.Fatalf("cursor %d left the window (top %d)", a.cursorLine(), a.top)
	}
}

func TestPaging(t *testing.T) {
	a, _ := newTestApp(t, numbered(200))
	run(a, "page-down")
	if a.top == 0 {
		t.Fatal("page-down did not move the window")
	}
	if a.cursorLine() < a.top {
		t.Fatal("page-down left the cursor above the window")
	}
	before := a.top
	run(a, "page-up")
	if a.top >= before {
		t.Fatal("page-up did not move back")
	}
}

// TestHorizontalScrollFollowsCursor checks that the window tracks the cursor
// column, which is what makes $ and w usable on a line wider than the terminal.
func TestHorizontalScrollFollowsCursor(t *testing.T) {
	long := strings.Repeat("ab", 120) + "END\n"
	a, scr := newTestApp(t, long)

	if a.hoff != 0 {
		t.Fatalf("hoff = %d at rest", a.hoff)
	}
	run(a, "line-end")
	if a.hoff == 0 {
		t.Fatal("$ on a long line did not scroll sideways")
	}
	if got := rowText(scr, 0); !strings.HasSuffix(got, "END") {
		t.Fatalf("row 0 = %q, want the end of the line visible", got)
	}

	run(a, "line-start")
	if a.hoff != 0 {
		t.Fatalf("0 left hoff at %d", a.hoff)
	}
	if got := rowText(scr, 0); !strings.HasPrefix(got, "abab") {
		t.Fatalf("row 0 = %q, want the start of the line", got)
	}
}

func TestLineNumberGutter(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	run(a, "line-numbers-toggle")

	got := rowText(scr, 0)
	if !strings.HasPrefix(got, "  1") || !strings.Contains(got, "line 000") {
		t.Fatalf("gutter row = %q, want a line number then the text", got)
	}
}

func waitFiltered(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.filt != nil && !a.filt.Done() {
		if time.Now().After(deadline) {
			t.Fatal("filter scan did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	a.draw()
}

func TestFilterOutRemovesLines(t *testing.T) {
	a, scr := newTestApp(t, numbered(100))

	a.exec(config.Command{Name: "filter-never", Args: []string{"INFO"}}, 0)
	waitFiltered(t, a)

	for row := 0; row < testH; row++ {
		text := rowText(scr, row)
		if strings.Contains(text, "INFO") {
			t.Fatalf("row %d still shows a discarded line: %q", row, text)
		}
		if !strings.Contains(text, "ERROR") {
			t.Fatalf("row %d = %q, want an ERROR line", row, text)
		}
	}
	if got := rowText(scr, 0); got != numberedLine(0) {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowText(scr, 1); got != numberedLine(3) {
		t.Fatalf("row 1 = %q", got)
	}
}

func TestFilterKeepThenPopRestoresEverything(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	full := a.viewLen()

	a.exec(config.Command{Name: "filter-only", Args: []string{"ERROR"}}, 0)
	waitFiltered(t, a)
	kept := a.viewLen()
	if kept == 0 || kept >= full {
		t.Fatalf("kept %d of %d lines; filter had no effect", kept, full)
	}

	run(a, "filter-pop")
	if got := a.viewLen(); got != full {
		t.Fatalf("after filter-pop Len = %d, want %d", got, full)
	}
	if a.filt != nil {
		t.Error("popping the last filter should drop the filtered view entirely")
	}
}

func TestFilterBypassKeepsTheStack(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	full := a.viewLen()

	a.exec(config.Command{Name: "filter-never", Args: []string{"INFO"}}, 0)
	waitFiltered(t, a)
	filtered := a.viewLen()

	run(a, "filter-bypass")
	if a.viewLen() != full {
		t.Fatalf("bypass Len = %d, want the unfiltered %d", a.viewLen(), full)
	}
	if a.rules.Len() != 1 {
		t.Fatalf("bypass discarded the rules: Len = %d", a.rules.Len())
	}

	run(a, "filter-bypass")
	if a.viewLen() != filtered {
		t.Fatalf("un-bypass Len = %d, want %d", a.viewLen(), filtered)
	}
}

// TestFilterHoldsPosition checks the anchoring that keeps you where you were
// when a filter is added, rather than throwing you back to the top.
func TestFilterHoldsPosition(t *testing.T) {
	a, _ := newTestApp(t, numbered(600))

	a.exec(config.Command{Name: "goto-line", Args: []string{"300"}}, 0)
	a.draw()
	if a.cursorLine() != 299 {
		t.Fatalf("goto-line put the cursor at %d, want 299", a.cursorLine())
	}

	a.exec(config.Command{Name: "filter-only", Args: []string{"ERROR"}}, 0)
	waitFiltered(t, a)

	ln, err := a.activeView().At(a.cursorLine(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The anchor must land on the first kept line at or after source line 300.
	if ln.Num < 300 || ln.Num > 303 {
		t.Fatalf("after filtering, the cursor is on line %d; expected the first kept line at or after 300", ln.Num)
	}
}

func TestHighlightStylesMatchingText(t *testing.T) {
	a, scr := newTestApp(t, "plain text here\n")

	// Turn off the cursor-line tint so this compares the highlight alone.
	if err := a.cfg.Set("cursor-line", "off"); err != nil {
		t.Fatal(err)
	}
	a.draw()
	_, _, before, _ := scr.GetContent(0, 0)

	a.addRule(filter.Highlight, "text", filter.Color{})
	a.draw()

	// "text" starts at column 6.
	_, _, hit, _ := scr.GetContent(6, 0)
	_, _, miss, _ := scr.GetContent(0, 0)
	if hit == miss {
		t.Fatal("highlighted text has the same style as the rest of the line")
	}
	if miss != before {
		t.Error("adding a highlight changed unrelated cells")
	}
}

func TestSyntaxProfileStylesLevels(t *testing.T) {
	a, scr := newTestApp(t, "2024-01-02T03:04:05 ERROR disk on fire\n")
	if a.eng.Profile == nil {
		t.Fatal("no syntax profile was selected")
	}
	if err := a.cfg.Set("cursor-line", "off"); err != nil {
		t.Fatal(err)
	}
	a.draw()

	_, _, errStyle, _ := scr.GetContent(20, 0) // inside "ERROR"
	_, _, msgStyle, _ := scr.GetContent(30, 0) // inside "disk on fire"
	if errStyle == msgStyle {
		t.Error("the ERROR level should be styled differently from the message")
	}
	if errStyle != a.cfg.Style("syntax.level.error") {
		t.Error("ERROR is not using the syntax.level.error style")
	}
}

func TestKeyDispatch(t *testing.T) {
	a, scr := newTestApp(t, numbered(50))

	a.key(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	a.draw()
	if a.cursorLine() != 1 {
		t.Fatalf("j did not move the cursor: %d", a.cursorLine())
	}

	a.key(tcell.NewEventKey(tcell.KeyRune, 'G', tcell.ModNone))
	a.draw()
	if got := rowText(scr, testH-1); got != numberedLine(49) {
		t.Fatalf("G did not jump to the end: last row = %q", got)
	}

	a.key(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	if !a.quit {
		t.Error("q did not set quit")
	}
}

func TestPromptEditingAndSubmit(t *testing.T) {
	a, _ := newTestApp(t, numbered(60))
	full := a.viewLen()

	// `!` opens the discard prompt.
	a.key(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone))
	if a.prompt == nil {
		t.Fatal("! did not open a prompt")
	}
	for _, r := range "INFOX" {
		a.key(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.key(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	if got := string(a.prompt.buf); got != "INFO" {
		t.Fatalf("prompt buffer = %q, want INFO", got)
	}

	a.key(tcell.NewEventKey(tcell.KeyEnter, '\r', tcell.ModNone))
	if a.prompt != nil {
		t.Error("Enter did not close the prompt")
	}
	waitFiltered(t, a)
	if a.viewLen() >= full {
		t.Fatalf("the prompt's filter was not applied: Len = %d of %d", a.viewLen(), full)
	}
	if len(a.histPattern) != 1 || a.histPattern[0] != "INFO" {
		t.Errorf("pattern history = %v", a.histPattern)
	}
}

func TestPromptEscapeCancels(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	before := a.viewLen()

	a.key(tcell.NewEventKey(tcell.KeyRune, '!', tcell.ModNone))
	for _, r := range "INFO" {
		a.key(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if a.prompt != nil {
		t.Error("Escape did not close the prompt")
	}
	if a.viewLen() != before {
		t.Error("Escape applied the filter anyway")
	}
}

func TestStatusLineAppearsWhenPinned(t *testing.T) {
	a, scr := newTestApp(t, numbered(100))
	run(a, "status-toggle")

	status := rowText(scr, testH-1)
	if !strings.Contains(status, "test.log") || !strings.Contains(status, "/100") {
		t.Fatalf("status line = %q", status)
	}
	// The log now occupies one row fewer.
	if got := rowText(scr, testH-2); got != numberedLine(testH-2) {
		t.Fatalf("row above status = %q", got)
	}
}

func TestHelpOverlay(t *testing.T) {
	a, scr := newTestApp(t, numbered(20))
	run(a, "help")
	if a.ov == nil {
		t.Fatal("help did not open an overlay")
	}

	var all strings.Builder
	for row := 0; row < testH; row++ {
		all.WriteString(rowText(scr, row))
		all.WriteByte('\n')
	}
	if !strings.Contains(all.String(), "key bindings") {
		t.Fatalf("overlay does not show its title:\n%s", all.String())
	}

	a.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if a.ov != nil {
		t.Error("Escape did not close the overlay")
	}
}

func TestUnknownKeyIsIgnored(t *testing.T) {
	a, _ := newTestApp(t, numbered(10))
	top := a.top
	a.key(tcell.NewEventKey(tcell.KeyRune, 'Z', tcell.ModNone))
	if a.top != top || a.quit {
		t.Error("an unbound key changed state")
	}
}

func TestExpiredMessageStopsRepainting(t *testing.T) {
	a, _ := newTestApp(t, numbered(20))
	if err := a.cfg.Set("message-timeout", "1ms"); err != nil {
		t.Fatal(err)
	}

	a.infof("hello")
	a.draw()
	time.Sleep(10 * time.Millisecond) // let the message time out

	if !a.tickDirty() {
		t.Fatal("an expired message should ask for one repaint")
	}
	if a.msg != "" {
		t.Fatal("the expired message was not cleared")
	}
	// And then the UI must settle, so it can go idle and release memory.
	if a.tickDirty() {
		t.Fatal("the UI keeps reporting dirty after the message expired")
	}
}

func TestAutoStatusDoesNotFlickerWhileTailing(t *testing.T) {
	a, _ := newTestApp(t, numbered(50))
	if err := a.cfg.Set("status-line", "auto"); err != nil {
		t.Fatal(err)
	}
	a.statusOn = "auto"
	a.clearMsg()

	// Fully indexed and quiet: no chrome, every row is log content.
	if a.showStatus() {
		t.Error("auto status is showing on a fully indexed, idle file")
	}

	// A small append leaves the view briefly behind. That must not toggle the
	// status line, or the screen shifts by a row on every write to the log.
	f, err := os.OpenFile(a.src.Name(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(numberedLine(50) + "\n"); err != nil {
		t.Fatal(err)
	}
	// SetSize is what the indexer does on noticing growth, before it consumes
	// the new bytes; that gap is the window this threshold governs.
	noticeGrowth(t, a)
	if a.showStatus() {
		t.Error("auto status appeared for a one-line append")
	}

	// Falling a long way behind is worth reporting.
	if _, err := f.WriteString(strings.Repeat("x", statusLagBytes+1) + "\n"); err != nil {
		t.Fatal(err)
	}
	noticeGrowth(t, a)
	if !a.showStatus() {
		t.Error("auto status did not appear while a large backlog was pending")
	}
}

func noticeGrowth(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.src.Refresh(); err != nil {
		t.Fatal(err)
	}
	a.idx.SetSize(a.src.Size())
}
